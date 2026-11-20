package app

import (
	"bytes"
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/nocaptain-oss/archie-link/internal/coexist"
	"github.com/nocaptain-oss/archie-link/internal/entitlement"
	"github.com/nocaptain-oss/archie-link/internal/model"
	"github.com/nocaptain-oss/archie-link/internal/state"
)

func TestUpDoesNotApplyWhenCoexistenceGateBlocks(t *testing.T) {
	adapter := newFakeAutomatic()
	adapter.profile = coexist.Profile{
		SchemaVersion: "1",
		OS:            "darwin",
		Routes: []coexist.RouteRecord{{
			Destination: "default",
			Interface:   "utun9",
			Owner:       coexist.OwnerUnknown,
		}},
		DNSResolvers: []coexist.DNSResolver{{
			Address: "192.168.1.1", Owner: coexist.OwnerSystem,
		}},
		BaselineComplete: true,
	}
	service := New(adapter, entitlement.NewTestProvider(nil), state.NewMachine(), storeForTest(t), &bytes.Buffer{})
	result, err := service.Up(context.Background(), Options{Yes: true, InstallID: "test-install"})
	if !errors.Is(err, coexist.ErrCoexistenceBlocked) {
		t.Fatalf("err=%v", err)
	}
	if adapter.ApplyCalls != 0 {
		t.Fatalf("Apply called %d times", adapter.ApplyCalls)
	}
	if result.ExitCode != 5 {
		t.Fatalf("exit=%d", result.ExitCode)
	}
}

func TestUpRollsBackOnHealthFailure(t *testing.T) {
	adapter := newFakeAutomatic()
	adapter.verifyReport = model.HealthReport{
		OK:      false,
		Failure: "deepseek_forbidden",
		Checks:  []model.HealthCheck{},
	}
	service := New(adapter, entitlement.NewTestProvider(nil), state.NewMachine(), storeForTest(t), &bytes.Buffer{})
	result, err := service.Up(context.Background(), Options{Yes: true, InstallID: "test-install"})
	if err == nil {
		t.Fatal("expected failure")
	}
	if !adapter.RollbackCalled {
		t.Fatal("rollback was not called")
	}
	if result.ExitCode != 7 {
		t.Fatalf("exit=%d", result.ExitCode)
	}
}

func TestUpRequiresExplicitYes(t *testing.T) {
	adapter := newFakeAutomatic()
	service := New(adapter, entitlement.NewTestProvider(nil), state.NewMachine(), storeForTest(t), &bytes.Buffer{})
	result, err := service.Up(context.Background(), Options{InstallID: "test-install"})
	if err == nil || result.ExitCode != 2 {
		t.Fatalf("result=%#v err=%v", result, err)
	}
	if adapter.ApplyCalls != 0 {
		t.Fatal("Apply ran without --yes")
	}
}

func TestUpRejectsExpiredEntitlementBeforeSnapshot(t *testing.T) {
	adapter := newFakeAutomatic()
	provider := entitlement.NewTestProvider(func(entitlement *entitlement.Entitlement) {
		entitlement.ExpiresAt = time.Now().Add(-time.Hour)
	})
	service := New(adapter, provider, state.NewMachine(), storeForTest(t), &bytes.Buffer{})
	result, err := service.Up(context.Background(), Options{Yes: true, InstallID: "test-install"})
	if !errors.Is(err, entitlement.ErrExpired) {
		t.Fatalf("err=%v", err)
	}
	if result.ExitCode != 4 {
		t.Fatalf("exit=%d", result.ExitCode)
	}
	if adapter.CaptureCalls != 0 || adapter.ApplyCalls != 0 {
		t.Fatalf("snapshot=%d apply=%d", adapter.CaptureCalls, adapter.ApplyCalls)
	}
}

func TestUpPersistsActiveOwnership(t *testing.T) {
	adapter := newFakeAutomatic()
	store := storeForTest(t)
	service := New(adapter, entitlement.NewTestProvider(nil), state.NewMachine(), store, &bytes.Buffer{})
	result, err := service.Up(context.Background(), Options{Yes: true, InstallID: "test-install"})
	if err != nil {
		t.Fatal(err)
	}
	if result.ExitCode != 0 || !result.Connected || !result.Healthy {
		t.Fatalf("result=%#v", result)
	}
	runtime, err := store.LoadRuntime()
	if err != nil {
		t.Fatal(err)
	}
	if runtime.Phase != state.PhaseActive || runtime.Ownership == nil {
		t.Fatalf("runtime=%#v", runtime)
	}
}

func TestUpUsesResolverChainWhenServiceDNSIsEmpty(t *testing.T) {
	adapter := newFakeAutomatic()
	adapter.snapshot.DNS = []string{}
	adapter.snapshot.ResolverDNS = []string{"192.168.1.1"}
	service := New(adapter, entitlement.NewTestProvider(nil), state.NewMachine(), storeForTest(t), &bytes.Buffer{})
	if _, err := service.Up(context.Background(), Options{Yes: true, InstallID: "test-install"}); err != nil {
		t.Fatal(err)
	}
	if len(adapter.lastApplyPlan.OriginalDNS) != 1 ||
		adapter.lastApplyPlan.OriginalDNS[0] != "192.168.1.1" {
		t.Fatalf("apply plan=%#v", adapter.lastApplyPlan)
	}
}

func TestUpAcceptsLinuxPlatform(t *testing.T) {
	adapter := newFakeAutomatic()
	adapter.profile.OS = "linux"
	adapter.platform = model.PlatformReport{OS: "linux", Version: "22.04", Arch: "amd64"}
	service := New(adapter, entitlement.NewTestProvider(nil), state.NewMachine(), storeForTest(t), &bytes.Buffer{})
	result, err := service.Up(context.Background(), Options{Yes: true, InstallID: "test-install"})
	if err != nil {
		t.Fatalf("linux automatic mode must pass the platform gate: %v", err)
	}
	if result.ExitCode != 0 || !result.Connected {
		t.Fatalf("result=%#v", result)
	}
	if adapter.ApplyCalls != 1 {
		t.Fatalf("Apply called %d times", adapter.ApplyCalls)
	}
}

func TestUpRejectsUnsupportedPlatform(t *testing.T) {
	adapter := newFakeAutomatic()
	adapter.platform = model.PlatformReport{OS: "plan9", Version: "1", Arch: "amd64"}
	service := New(adapter, entitlement.NewTestProvider(nil), state.NewMachine(), storeForTest(t), &bytes.Buffer{})
	result, err := service.Up(context.Background(), Options{Yes: true, InstallID: "test-install"})
	if err == nil || result.ExitCode != 3 {
		t.Fatalf("result=%#v err=%v", result, err)
	}
	if adapter.ApplyCalls != 0 {
		t.Fatal("Apply must not run on unsupported platforms")
	}
}

func storeForTest(t *testing.T) state.Store {
	t.Helper()
	return state.Store{Dir: filepath.Join(t.TempDir(), "state")}
}

type fakeAutomatic struct {
	platform        model.PlatformReport
	profile         coexist.Profile
	snapshot       model.NetworkSnapshot
	probe          model.ProbeResult
	ownership      model.Ownership
	verifyReport   model.HealthReport
	ApplyCalls     int
	CaptureCalls   int
	RollbackCalls  int
	RollbackCalled bool
	lastApplyPlan  model.ApplyPlan
}

func newFakeAutomatic() *fakeAutomatic {
	return &fakeAutomatic{
		platform: model.PlatformReport{OS: "darwin", Version: "12.7.6", Arch: "amd64"},
		profile: coexist.Profile{
			SchemaVersion: "1",
			OS:            "darwin",
			Routes: []coexist.RouteRecord{{
				Destination: "default",
				Interface:   "en0",
				Owner:       coexist.OwnerSystem,
			}},
			DNSResolvers: []coexist.DNSResolver{{
				Address: "192.168.1.1", Owner: coexist.OwnerSystem,
			}},
			BaselineComplete: true,
		},
		snapshot: model.NetworkSnapshot{
			SchemaVersion:    model.SchemaVersion,
			DefaultInterface: "en0",
			Gateway:          "192.168.1.1",
			DNS:              []string{"192.168.1.1"},
		},
		probe: model.ProbeResult{
			Endpoint:  model.Endpoint{Address: "188.114.97.1", Port: 500},
			Handshake: true,
			Trace:     "warp=on",
		},
		ownership: model.Ownership{
			Interface: "utun12",
			Endpoint:  model.Endpoint{Address: "188.114.97.1", Port: 500},
		},
		verifyReport: model.HealthReport{OK: true, Checks: []model.HealthCheck{}},
	}
}

func (f *fakeAutomatic) Detect(context.Context) (model.PlatformReport, model.CapabilityReport, error) {
	return f.platform, model.CapabilityReport{Shell: true, Elevation: true, NetworkMutation: true}, nil
}

func (f *fakeAutomatic) Capture(context.Context) (model.NetworkSnapshot, error) {
	f.CaptureCalls++
	return f.snapshot, nil
}

func (f *fakeAutomatic) Coexistence(context.Context) (coexist.Profile, error) {
	return f.profile, nil
}

func (f *fakeAutomatic) Probe(context.Context, model.ProbePlan) (model.ProbeResult, error) {
	return f.probe, nil
}

func (f *fakeAutomatic) Apply(_ context.Context, plan model.ApplyPlan) (model.Ownership, error) {
	f.ApplyCalls++
	f.lastApplyPlan = plan
	return f.ownership, nil
}

func (f *fakeAutomatic) Verify(context.Context, model.Ownership) (model.HealthReport, error) {
	return f.verifyReport, nil
}

func (f *fakeAutomatic) Rollback(context.Context, model.NetworkSnapshot) error {
	f.RollbackCalls++
	f.RollbackCalled = true
	return nil
}

func (f *fakeAutomatic) Resume(model.Ownership) {}
