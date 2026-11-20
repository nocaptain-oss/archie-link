package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/nocaptain-oss/archie-link/internal/coexist"
	"github.com/nocaptain-oss/archie-link/internal/entitlement"
	"github.com/nocaptain-oss/archie-link/internal/lock"
	"github.com/nocaptain-oss/archie-link/internal/model"
	"github.com/nocaptain-oss/archie-link/internal/state"
)

const (
	ExitOK               = 0
	ExitArgument         = 2
	ExitUnsupported      = 3
	ExitEntitlement      = 4
	ExitPreflight        = 5
	ExitStartup          = 6
	ExitHealth           = 7
	ExitRollback         = 8
	ExitLocked           = 9
	NetworkApplyFeature  = "network_apply"
	DefaultInstallIDName = "install-id"
)

type Automatic interface {
	Detect(context.Context) (model.PlatformReport, model.CapabilityReport, error)
	Capture(context.Context) (model.NetworkSnapshot, error)
	Coexistence(context.Context) (coexist.Profile, error)
	Probe(context.Context, model.ProbePlan) (model.ProbeResult, error)
	Apply(context.Context, model.ApplyPlan) (model.Ownership, error)
	Verify(context.Context, model.Ownership) (model.HealthReport, error)
	Rollback(context.Context, model.NetworkSnapshot) error
	Resume(model.Ownership)
}

type Options struct {
	Yes                bool
	InstallID          string
	ProbePlan          model.ProbePlan
	LocalDNSDomains    []string
	ProtectionDomains  []string
	AlwaysDirectCIDRs  []string
	LocalPorts         []int
	ConfigPath         string
	ProtectedEndpoints []coexist.ProtectedEndpoint
}

type Result struct {
	SchemaVersion string              `json:"schema_version"`
	Phase         state.Phase         `json:"phase"`
	Connected     bool                `json:"connected"`
	Healthy       bool                `json:"healthy"`
	Ownership     *model.Ownership    `json:"ownership,omitempty"`
	Health        *model.HealthReport `json:"health,omitempty"`
	ExitCode      int                 `json:"exit_code"`
	Error         string              `json:"error,omitempty"`
}

type ServiceError struct {
	Code     string
	ExitCode int
	Err      error
}

func (e *ServiceError) Error() string {
	if e == nil {
		return ""
	}
	return e.Code + ": " + e.Err.Error()
}

func (e *ServiceError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

type Service struct {
	automatic   Automatic
	entitlement entitlement.Provider
	machine     *state.Machine
	store       state.Store
	log         io.Writer
	coexistence coexist.Provider
	baseline    coexist.HTTPChecker
	endpoints   []coexist.ProtectedEndpoint
	probePlan   model.ProbePlan
}

func New(
	automatic Automatic,
	provider entitlement.Provider,
	machine *state.Machine,
	store state.Store,
	log io.Writer,
) *Service {
	service := &Service{
		automatic:   automatic,
		entitlement: provider,
		machine:     machine,
		store:       store,
		log:         log,
		baseline:    coexist.DefaultHTTPChecker{},
		endpoints:   defaultProtectedEndpoints(),
		probePlan:   defaultProbePlan(),
	}
	if provider, ok := automatic.(coexist.Provider); ok {
		service.coexistence = provider
	}
	return service
}

func NewWithCoexistence(
	automatic Automatic,
	provider entitlement.Provider,
	machine *state.Machine,
	store state.Store,
	log io.Writer,
	coexistence coexist.Provider,
) *Service {
	service := New(automatic, provider, machine, store, log)
	service.coexistence = coexistence
	return service
}

func (s *Service) Up(ctx context.Context, options Options) (Result, error) {
	result := Result{SchemaVersion: model.SchemaVersion, Phase: state.PhaseDetected}
	if !options.Yes {
		return s.fail(result, "confirmation_required", ExitArgument,
			errors.New("network changes require explicit --yes confirmation"))
	}
	if s.automatic == nil {
		return s.fail(result, "preflight_failed", ExitPreflight, errors.New("automatic adapter is not configured"))
	}
	if s.entitlement == nil {
		return s.fail(result, "entitlement_failed", ExitEntitlement, entitlement.ErrProviderNotConfigured)
	}
	release, err := s.acquire()
	if err != nil {
		return s.fail(result, "lock_conflict", ExitLocked, err)
	}
	defer release()

	if runtime, err := s.store.LoadRuntime(); err == nil && runtime.Phase == state.PhaseActive && runtime.Ownership != nil {
		s.automatic.Resume(*runtime.Ownership)
		health, verifyErr := s.automatic.Verify(ctx, *runtime.Ownership)
		result.Phase = runtime.Phase
		result.Connected = true
		result.Healthy = health.OK && verifyErr == nil
		result.Ownership = runtime.Ownership
		result.Health = &health
		return result, verifyErr
	}

	platform, capabilities, err := s.automatic.Detect(ctx)
	if err != nil {
		return s.fail(result, "preflight_failed", ExitPreflight, err)
	}
	if (platform.OS != "darwin" && platform.OS != "linux") || !capabilities.Shell || !capabilities.NetworkMutation {
		return s.fail(result, "unsupported_platform", ExitUnsupported,
			fmt.Errorf("automatic mode is supported only on macOS 12+ or Ubuntu/Debian with WireGuard tools and shell access"))
	}
	if !capabilities.Elevation {
		return s.fail(result, "elevation_required", ExitPreflight,
			errors.New("automatic mode requires an elevated ArchieLink process"))
	}
	if err := s.advance(state.PhasePlanned); err != nil {
		return s.fail(result, "invalid_state", ExitPreflight, err)
	}
	result.Phase = s.machine.Current()

	installID := strings.TrimSpace(options.InstallID)
	if installID == "" {
		return s.fail(result, "install_id_required", ExitPreflight, errors.New("install ID is required"))
	}
	_, err = s.entitlement.Verify(ctx, entitlement.Request{
		SkillID: "archie-link", InstallID: installID, Feature: NetworkApplyFeature,
	})
	if err != nil {
		return s.fail(result, "entitlement_failed", ExitEntitlement, err)
	}
	if err := s.advance(state.PhaseLicensed); err != nil {
		return s.fail(result, "invalid_state", ExitPreflight, err)
	}

	snapshot, err := s.automatic.Capture(ctx)
	if err != nil {
		return s.fail(result, "snapshot_failed", ExitPreflight, err)
	}
	if err := s.store.SaveSnapshot(snapshot); err != nil {
		return s.fail(result, "snapshot_failed", ExitPreflight, err)
	}

	profile, err := s.buildCoexistenceProfile(ctx, options.ProtectedEndpoints)
	if err != nil {
		return s.fail(result, "preflight_failed", ExitPreflight, err)
	}
	decision := coexist.Evaluate(profile)
	if err := coexist.RequireAuto(decision); err != nil {
		return s.fail(result, "coexistence_blocked", ExitPreflight, err)
	}

	if err := s.advance(state.PhaseProbing); err != nil {
		return s.fail(result, "invalid_state", ExitPreflight, err)
	}
	probePlan := options.ProbePlan
	if len(probePlan.Candidates) == 0 {
		probePlan = s.probePlan
	}
	probe, err := s.automatic.Probe(ctx, probePlan)
	if err != nil {
		return s.fail(result, "probe_failed", ExitPreflight, err)
	}

	if err := s.advance(state.PhaseApplying); err != nil {
		return s.fail(result, "invalid_state", ExitPreflight, err)
	}
	resolverDNS := snapshot.ResolverDNS
	if len(resolverDNS) == 0 {
		resolverDNS = snapshot.DNS
	}
	applyPlan := model.ApplyPlan{
		Endpoint:          probe.Endpoint,
		LocalDNSDomains:   defaultStrings(options.LocalDNSDomains, []string{"api.deepseek.com"}),
		ProtectionDomains: defaultStrings(options.ProtectionDomains, []string{"api.skillhub.cn"}),
		AlwaysDirectCIDRs: defaultStrings(options.AlwaysDirectCIDRs, []string{
			"10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16",
		}),
		LocalPorts:       defaultInts(options.LocalPorts, []int{8688}),
		ConfigPath:       options.ConfigPath,
		OriginalDNS:      append([]string(nil), resolverDNS...),
		Gateway:          snapshot.Gateway,
		DefaultInterface: snapshot.DefaultInterface,
		NetworkService:   snapshot.NetworkService,
	}
	ownership, err := s.automatic.Apply(ctx, applyPlan)
	if err != nil {
		return s.rollbackFailure(ctx, result, snapshot, "apply_failed", err, ExitStartup)
	}
	if err := s.advance(state.PhaseVerifying); err != nil {
		return s.rollbackFailure(ctx, result, snapshot, "invalid_state", err, ExitStartup)
	}
	health, err := s.automatic.Verify(ctx, ownership)
	result.Health = &health
	if err != nil || !health.OK {
		if err == nil {
			err = fmt.Errorf("health check failed: %s", health.Failure)
		}
		return s.rollbackFailure(ctx, result, snapshot, "health_failed", err, ExitHealth)
	}
	if err := s.advance(state.PhaseActive); err != nil {
		return s.rollbackFailure(ctx, result, snapshot, "invalid_state", err, ExitStartup)
	}
	runtime := state.Runtime{
		SchemaVersion: model.SchemaVersion,
		Phase:         state.PhaseActive,
		Endpoint:      ownership.Endpoint,
		Ownership:     &ownership,
	}
	if err := s.store.SaveRuntime(runtime); err != nil {
		return s.rollbackFailure(ctx, result, snapshot, "state_write_failed", err, ExitStartup)
	}
	result.Phase = state.PhaseActive
	result.Connected = true
	result.Healthy = true
	result.Ownership = &ownership
	s.logf("active interface=%s endpoint=%s:%d", ownership.Interface, ownership.Endpoint.Address, ownership.Endpoint.Port)
	return result, nil
}

func (s *Service) Status(ctx context.Context) (Result, error) {
	result := Result{SchemaVersion: model.SchemaVersion, Phase: state.PhaseDetected, Healthy: false}
	runtime, err := s.store.LoadRuntime()
	if errors.Is(err, io.EOF) {
		return result, nil
	}
	if errors.Is(err, os.ErrNotExist) {
		return result, nil
	}
	if err != nil {
		result.Phase = state.PhaseRollbackFailed
		return s.fail(result, "state_unreadable", ExitPreflight, err)
	}
	result.Phase = runtime.Phase
	result.Connected = runtime.Phase == state.PhaseActive
	result.Ownership = runtime.Ownership
	if !result.Connected || runtime.Ownership == nil {
		return result, nil
	}
	s.automatic.Resume(*runtime.Ownership)
	health, err := s.automatic.Verify(ctx, *runtime.Ownership)
	result.Health = &health
	result.Healthy = health.OK && err == nil
	if err != nil {
		return s.fail(result, "health_failed", ExitHealth, err)
	}
	if !health.OK {
		return s.fail(result, "health_failed", ExitHealth, fmt.Errorf("health check failed: %s", health.Failure))
	}
	return result, nil
}

func (s *Service) Down(ctx context.Context) (Result, error) {
	return s.Rollback(ctx)
}

func (s *Service) Rollback(ctx context.Context) (Result, error) {
	result := Result{SchemaVersion: model.SchemaVersion, Phase: state.PhaseDetected}
	release, err := s.acquire()
	if err != nil {
		return s.fail(result, "lock_conflict", ExitLocked, err)
	}
	defer release()

	runtime, err := s.store.LoadRuntime()
	if errors.Is(err, os.ErrNotExist) {
		return result, nil
	}
	if err != nil {
		return s.fail(result, "state_unreadable", ExitPreflight, err)
	}
	snapshot, err := s.store.LoadSnapshot()
	if err != nil {
		return s.fail(result, "snapshot_missing", ExitPreflight, err)
	}
	if runtime.Ownership != nil {
		s.automatic.Resume(*runtime.Ownership)
	}
	if err := s.automatic.Rollback(ctx, snapshot); err != nil {
		result.Phase = state.PhaseRollbackFailed
		return s.fail(result, "rollback_incomplete", ExitRollback, err)
	}
	if err := s.store.ClearRuntime(); err != nil {
		return s.fail(result, "state_write_failed", ExitRollback, err)
	}
	result.Phase = state.PhaseRestored
	result.Connected = false
	result.Healthy = false
	return result, nil
}

func (s *Service) buildCoexistenceProfile(
	ctx context.Context,
	configured []coexist.ProtectedEndpoint,
) (coexist.Profile, error) {
	if s.coexistence == nil {
		return coexist.Profile{}, errors.New("coexistence provider is not configured")
	}
	profile, err := s.coexistence.Coexistence(ctx)
	if err != nil {
		return coexist.Profile{}, err
	}
	if profile.BaselineComplete {
		return profile, nil
	}
	endpoints := configured
	if len(endpoints) == 0 {
		endpoints = s.endpoints
	}
	endpoints = addDetectedLocalEndpoints(endpoints, profile.Listeners)
	profile.Protected = append([]coexist.ProtectedEndpoint(nil), endpoints...)
	results := coexist.RunBaseline(ctx, endpoints, s.baseline)
	profile.BaselineComplete = len(results) > 0
	for _, result := range results {
		if !result.OK {
			profile.BaselineComplete = false
			break
		}
	}
	return profile, nil
}

func (s *Service) rollbackFailure(
	ctx context.Context,
	result Result,
	snapshot model.NetworkSnapshot,
	code string,
	cause error,
	exitCode int,
) (Result, error) {
	_ = s.advance(state.PhaseRollingBack)
	rollbackErr := s.automatic.Rollback(context.WithoutCancel(ctx), snapshot)
	_ = s.store.ClearRuntime()
	if rollbackErr != nil {
		result.Phase = state.PhaseRollbackFailed
		return s.fail(result, "rollback_incomplete", ExitRollback, errors.Join(cause, rollbackErr))
	}
	result.Phase = state.PhaseRestored
	return s.fail(result, code, exitCode, cause)
}

func (s *Service) advance(phase state.Phase) error {
	if s.machine == nil {
		s.machine = state.NewMachine()
	}
	return s.machine.To(phase)
}

func (s *Service) acquire() (func(), error) {
	handle, err := lock.Acquire(filepath.Join(s.store.Dir, "archielink.lock"))
	if err != nil {
		return nil, err
	}
	return func() { _ = handle.Close() }, nil
}

func (s *Service) fail(result Result, code string, exitCode int, err error) (Result, error) {
	result.ExitCode = exitCode
	if err != nil {
		result.Error = err.Error()
	}
	return result, &ServiceError{Code: code, ExitCode: exitCode, Err: err}
}

func (s *Service) logf(format string, args ...any) {
	if s.log != nil {
		fmt.Fprintf(s.log, format+"\n", args...)
	}
}

func defaultStrings(value, fallback []string) []string {
	if len(value) > 0 {
		return value
	}
	return fallback
}

func defaultInts(value, fallback []int) []int {
	if len(value) > 0 {
		return value
	}
	return fallback
}

func defaultProtectedEndpoints() []coexist.ProtectedEndpoint {
	return []coexist.ProtectedEndpoint{
		{
			ID: "deepseek-api", Kind: "local_proxy_upstream",
			UpstreamURL: "https://api.deepseek.com",
			Expected:    "not_403", RequiredRoute: "local_dns",
			BaselineRequired: true,
		},
	}
}

func addDetectedLocalEndpoints(
	endpoints []coexist.ProtectedEndpoint,
	listeners []coexist.ListenerRecord,
) []coexist.ProtectedEndpoint {
	for _, listener := range listeners {
		if listener.Port != 8688 {
			continue
		}
		for _, endpoint := range endpoints {
			if endpoint.ID == "local-cache" {
				return endpoints
			}
		}
		return append([]coexist.ProtectedEndpoint{{
			ID: "local-cache", Kind: "loopback_service",
			DirectURL: "http://127.0.0.1:8688/responses",
			Expected:  "no_error", RequiredRoute: "local_direct",
			BaselineRequired: true,
		}}, endpoints...)
	}
	return endpoints
}

func defaultProbePlan() model.ProbePlan {
	return model.ProbePlan{
		Candidates: []model.Endpoint{
			{Address: "162.159.192.1", Port: 2408},
			{Address: "162.159.193.1", Port: 2408},
			{Address: "188.114.96.1", Port: 500},
			{Address: "188.114.97.1", Port: 500},
			{Address: "188.114.98.1", Port: 500},
		},
		TimeoutSec: 10,
	}
}
