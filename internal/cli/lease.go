package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"time"

	"github.com/nocaptain-oss/archie-link/internal/deviceid"
	"github.com/nocaptain-oss/archie-link/internal/entitlement"
	"github.com/nocaptain-oss/archie-link/internal/leaseclient"
	appstate "github.com/nocaptain-oss/archie-link/internal/state"
)

type leaseOptions struct {
	order      string
	server     string
	file       string
	plan       string
	requestID  string
	jsonOutput bool
}

// runLease 让客户端直接与控制面完成挑战、激活、续租，并把租约写到本地。
func runLease(args []string, stdout, stderr io.Writer, deps Dependencies) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "usage: archielink lease <identity|pay-intent|activate|renew|status|renew-loop> [--order id] [--server url] [--file path] [--request-id id] [--plan activation-task] [--json]")
		return 2
	}
	command := args[0]
	switch command {
	case "identity", "pay-intent", "activate", "renew", "status", "renew-loop":
	default:
		fmt.Fprintf(stderr, "unknown lease command: %s\n", command)
		return 2
	}
	options, ok := parseLeaseOptions(args[1:], stderr)
	if !ok {
		return 2
	}
	if (command == "activate" || command == "renew" || command == "renew-loop") && options.order == "" {
		fmt.Fprintf(stderr, "lease %s requires --order\n", command)
		return 2
	}
	if command == "pay-intent" && options.requestID == "" {
		fmt.Fprintln(stderr, "lease pay-intent requires --request-id")
		return 2
	}
	if options.plan == "" {
		options.plan = "activation-task"
	}
	if deps.LifecycleError != nil {
		return writeLifecycleError(stdout, stderr, options.jsonOutput, 5, "preflight_failed", deps.LifecycleError)
	}
	if deps.Lifecycle == nil {
		return writeLifecycleError(stdout, stderr, options.jsonOutput, 3, "unsupported_platform",
			errors.New("lease management is not available on this platform"))
	}
	if deps.Lifecycle.Automatic == nil {
		return writeLifecycleError(stdout, stderr, options.jsonOutput, 3, "unsupported_platform",
			errors.New("lease management is not available on this platform"))
	}

	lifecycle := deps.Lifecycle
	ctx := context.Background()
	device := deviceid.NewWithEnclave(deviceid.KeychainStore{}, lifecycle.InstallID, deviceid.SecureEnclaveStore{})
	path := leaseFilePath(lifecycle, options.file)

	if command == "identity" {
		return runLeaseIdentity(ctx, device, stdout, stderr, options.jsonOutput)
	}
	if command == "status" {
		return runLeaseStatus(ctx, path, device, lifecycle.Store, stdout, stderr, options.jsonOutput)
	}

	client := leaseclient.Client{
		BaseURL: options.server, Device: device, InstallID: lifecycle.InstallID,
		APIToken: os.Getenv("ARCHIE_LINK_CP_API_TOKEN"),
	}
	if command == "pay-intent" {
		identity, err := device.LoadOrCreate(ctx)
		if err != nil {
			return writeLifecycleError(stdout, stderr, options.jsonOutput, 5, "device_identity_unavailable", err)
		}
		result, err := client.BeginActivationIntent(ctx, options.requestID, options.plan)
		if err != nil {
			return writeLifecycleError(stdout, stderr, options.jsonOutput, 4, "payment_intent_failed", err)
		}
		if options.jsonOutput {
			payload := map[string]any{
				"schema_version": "1",
				"request_id":     options.requestID,
				"plan":           options.plan,
				"payment_needed": result.PaymentRequired,
				"request_body": map[string]string{
					"request_id": options.requestID, "plan": options.plan,
					"install_id": lifecycle.InstallID, "device_public_key": identity.PublicKey,
				},
			}
			if result.PaymentRequired {
				payload["payment_needed_header"] = result.PaymentNeeded
				payload["resource_url"] = clientURL(client, "/v1/pay/activation-intent")
			} else {
				payload["grant"] = result.Grant
			}
			if err := writeJSON(stdout, payload); err != nil {
				fmt.Fprintf(stderr, "encode payment intent: %v\n", err)
				return 1
			}
			return 0
		}
		if result.PaymentRequired {
			fmt.Fprintf(stdout, "payment required for %s; hand the 402 request to the Alipay payment skill\n", options.plan)
			return 0
		}
		fmt.Fprintf(stdout, "activation paid: order=%s plan=%s\n", result.Grant.OrderID, result.Grant.Plan)
		return 0
	}
	if command == "renew-loop" {
		return runLeaseRenewLoop(ctx, client, path, lifecycle, options, stdout, stderr)
	}
	if command == "activate" {
		return storeActivatedLease(ctx, client, options.order, path, lifecycle, stdout, stderr, options.jsonOutput)
	}
	data, err := client.Renew(ctx, options.order)
	if err != nil {
		return writeLifecycleError(stdout, stderr, options.jsonOutput, 4, "lease_failed", err)
	}
	lease, err := decodeLease(data)
	if err != nil {
		return writeLifecycleError(stdout, stderr, options.jsonOutput, 4, "lease_malformed", err)
	}
	if err := recordLeaseState(lifecycle.Store, options.order, lease, time.Now()); err != nil {
		return writeLifecycleError(stdout, stderr, options.jsonOutput, 4, "lease_state_write_failed", err)
	}
	if err := writeLeaseFile(path, data); err != nil {
		return writeLifecycleError(stdout, stderr, options.jsonOutput, 4, "lease_write_failed", err)
	}
	if options.jsonOutput {
		if err := writeJSON(stdout, map[string]any{
			"schema_version":      "1",
			"lease_file":          path,
			"license_id":          lease.LicenseID,
			"plan":                lease.Plan,
			"features":            lease.Features,
			"issued_at":           lease.IssuedAt,
			"expires_at":          lease.ExpiresAt,
			"offline_grace_until": lease.OfflineGraceUntil,
			"revocation_epoch":    lease.RevocationEpoch,
		}); err != nil {
			fmt.Fprintf(stderr, "encode lease result: %v\n", err)
			return 1
		}
		return 0
	}
	fmt.Fprintf(stdout, "lease written to %s (license %s, expires %s)\n",
		path, lease.LicenseID, lease.ExpiresAt.Format(time.RFC3339))
	return 0
}

// storeActivatedLease 用已支付订单在本机完成激活并落盘租约（lease activate 与 buy 共用）。
func storeActivatedLease(ctx context.Context, client leaseclient.Client, orderID, path string, lifecycle *LifecycleDependencies, stdout, stderr io.Writer, jsonOutput bool) int {
	data, err := client.Activate(ctx, orderID)
	if err != nil {
		return writeLifecycleError(stdout, stderr, jsonOutput, 4, "lease_failed", err)
	}
	lease, err := decodeLease(data)
	if err != nil {
		return writeLifecycleError(stdout, stderr, jsonOutput, 4, "lease_malformed", err)
	}
	if err := recordLeaseState(lifecycle.Store, orderID, lease, time.Now()); err != nil {
		return writeLifecycleError(stdout, stderr, jsonOutput, 4, "lease_state_write_failed", err)
	}
	if err := writeLeaseFile(path, data); err != nil {
		return writeLifecycleError(stdout, stderr, jsonOutput, 4, "lease_write_failed", err)
	}
	if jsonOutput {
		if err := writeJSON(stdout, map[string]any{
			"schema_version":      "1",
			"lease_file":          path,
			"license_id":          lease.LicenseID,
			"plan":                lease.Plan,
			"features":            lease.Features,
			"issued_at":           lease.IssuedAt,
			"expires_at":          lease.ExpiresAt,
			"offline_grace_until": lease.OfflineGraceUntil,
			"revocation_epoch":    lease.RevocationEpoch,
		}); err != nil {
			fmt.Fprintf(stderr, "encode lease result: %v\n", err)
			return 1
		}
		return 0
	}
	fmt.Fprintf(stdout, "lease written to %s (license %s, expires %s)\n",
		path, lease.LicenseID, lease.ExpiresAt.Format(time.RFC3339))
	return 0
}

func runLeaseIdentity(ctx context.Context, device *deviceid.Manager, stdout, stderr io.Writer, jsonOutput bool) int {
	identity, err := device.LoadOrCreate(ctx)
	if err != nil {
		return writeLifecycleError(stdout, stderr, jsonOutput, 5, "device_identity_unavailable", err)
	}
	if jsonOutput {
		if err := writeJSON(stdout, map[string]any{
			"schema_version": "1",
			"install_id":     identity.InstallID, "device_public_key": identity.PublicKey,
			"key_id": identity.KeyID, "key_format": identity.KeyFormat,
			"secure_enclave": identity.SecureEnclave,
		}); err != nil {
			fmt.Fprintf(stderr, "encode device identity: %v\n", err)
			return 1
		}
		return 0
	}
	fmt.Fprintf(stdout, "install=%s key=%s secure_enclave=%t\n",
		identity.InstallID, identity.KeyID, identity.SecureEnclave)
	return 0
}

func runLeaseRenewLoop(
	ctx context.Context,
	client leaseclient.Client,
	path string,
	lifecycle *LifecycleDependencies,
	options leaseOptions,
	stdout, stderr io.Writer,
) int {
	renewer := leaseclient.Renewer{
		Client:  &client,
		OrderID: options.order,
		LoadLease: func(ctx context.Context) ([]byte, error) {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			return os.ReadFile(path)
		},
		SaveLease: func(ctx context.Context, data []byte) error {
			if err := ctx.Err(); err != nil {
				return err
			}
			lease, err := decodeLease(data)
			if err != nil {
				return err
			}
			if err := recordLeaseState(lifecycle.Store, options.order, lease, time.Now()); err != nil {
				return err
			}
			return writeLeaseFile(path, data)
		},
		SaveEpoch: func(ctx context.Context, epoch int) error {
			state, err := lifecycle.Store.LoadLeaseState()
			if err != nil {
				return err
			}
			if epoch > state.RevocationFloor {
				state.RevocationFloor = epoch
			}
			state = state.Observe(time.Now())
			return lifecycle.Store.SaveLeaseState(state)
		},
	}
	runCtx, stop := signal.NotifyContext(ctx, os.Interrupt)
	defer stop()
	if options.jsonOutput {
		if err := writeJSON(stdout, map[string]any{
			"schema_version": "1",
			"status":         "renew_loop_started",
			"lease_file":     path,
		}); err != nil {
			fmt.Fprintf(stderr, "encode renew loop: %v\n", err)
			return 1
		}
	} else {
		fmt.Fprintf(stdout, "lease renew loop started for %s\n", path)
	}
	if err := renewer.Run(runCtx); err != nil && !errors.Is(err, context.Canceled) {
		return writeLifecycleError(stdout, stderr, options.jsonOutput, 4, "renew_loop_failed", err)
	}
	return 0
}

func runLeaseStatus(ctx context.Context, path string, device *deviceid.Manager, store appstate.Store, stdout, stderr io.Writer, jsonOutput bool) int {
	data, err := os.ReadFile(path)
	if err != nil {
		return writeLifecycleError(stdout, stderr, jsonOutput, 4, "lease_missing", err)
	}
	root, err := entitlement.EmbeddedTrustRoot()
	if err != nil {
		return writeLifecycleError(stdout, stderr, jsonOutput, 5, "trust_root_unavailable", err)
	}
	identity, err := device.LoadOrCreate(ctx)
	if err != nil {
		return writeLifecycleError(stdout, stderr, jsonOutput, 5, "device_identity_unavailable", err)
	}
	leaseState, stateErr := store.LoadLeaseState()
	if stateErr != nil {
		return writeLifecycleError(stdout, stderr, jsonOutput, 5, "lease_state_unavailable", stateErr)
	}
	now := time.Now()
	clockReview := leaseState.ClockReviewRequired || entitlement.ClockRollback(leaseState.LastSeenAt, now)
	leaseState = leaseState.Observe(now)
	leaseState.ClockReviewRequired = clockReview
	if stateErr := store.SaveLeaseState(leaseState); stateErr != nil {
		return writeLifecycleError(stdout, stderr, jsonOutput, 5, "lease_state_write_failed", stateErr)
	}
	if clockReview {
		return writeLifecycleError(stdout, stderr, jsonOutput, 5, "clock_rollback_detected",
			entitlement.ErrClockRollback)
	}
	lease, state, verifyErr := verifyLeaseData(data, root, identity, now, leaseState.RevocationFloor)
	if verifyErr == nil {
		leaseState.LicenseID = lease.LicenseID
		if lease.RevocationEpoch > leaseState.RevocationFloor {
			leaseState.RevocationFloor = lease.RevocationEpoch
		}
		if stateErr := store.SaveLeaseState(leaseState); stateErr != nil {
			return writeLifecycleError(stdout, stderr, jsonOutput, 5, "lease_state_write_failed", stateErr)
		}
	}

	if jsonOutput {
		payload := map[string]any{
			"schema_version": "1",
			"lease_file":     path,
			"valid":          verifyErr == nil,
		}
		if verifyErr == nil {
			payload["state"] = string(state)
			payload["license_id"] = lease.LicenseID
			payload["plan"] = lease.Plan
			payload["features"] = lease.Features
			payload["issued_at"] = lease.IssuedAt
			payload["expires_at"] = lease.ExpiresAt
			payload["offline_grace_until"] = lease.OfflineGraceUntil
			payload["revocation_epoch"] = lease.RevocationEpoch
		} else {
			payload["error"] = verifyErr.Error()
		}
		if err := writeJSON(stdout, payload); err != nil {
			fmt.Fprintf(stderr, "encode lease status: %v\n", err)
			return 1
		}
		if verifyErr != nil {
			return 4
		}
		return 0
	}
	if verifyErr != nil {
		return writeLifecycleError(stdout, stderr, false, 4, "lease_invalid", verifyErr)
	}
	fmt.Fprintf(stdout, "lease valid (%s): license %s, plan %s, expires %s\n",
		state, lease.LicenseID, lease.Plan, lease.ExpiresAt.Format(time.RFC3339))
	return 0
}

func verifyLeaseData(data []byte, root entitlement.TrustRoot, identity deviceid.Identity, now time.Time, revocationFloor int) (entitlement.Lease, entitlement.LeaseState, error) {
	lease, err := decodeLease(data)
	if err != nil {
		return entitlement.Lease{}, "", err
	}
	feature := ""
	if len(lease.Features) > 0 {
		feature = lease.Features[0]
	}
	return entitlement.VerifyLease(data, root, entitlement.LeaseRequest{
		SkillID:         entitlement.SkillID,
		InstallID:       identity.InstallID,
		DevicePublicKey: identity.PublicKey,
		Feature:         feature,
		RevocationFloor: revocationFloor,
	}, now)
}

func recordLeaseState(store appstate.Store, orderID string, lease entitlement.Lease, now time.Time) error {
	state, err := store.LoadLeaseState()
	if err != nil {
		return err
	}
	state.LicenseID = lease.LicenseID
	state.OrderID = orderID
	if lease.IssuedAt.After(now) {
		now = lease.IssuedAt
	}
	state = state.Observe(now)
	if now.After(state.LastRenewedAt) {
		state.LastRenewedAt = now
	}
	state.ClockReviewRequired = false
	if lease.RevocationEpoch > state.RevocationFloor {
		state.RevocationFloor = lease.RevocationEpoch
	}
	return store.SaveLeaseState(state)
}

func decodeLease(data []byte) (entitlement.Lease, error) {
	var envelope struct {
		Payload json.RawMessage `json:"payload"`
	}
	if err := json.Unmarshal(data, &envelope); err != nil {
		return entitlement.Lease{}, entitlement.ErrMalformed
	}
	var lease entitlement.Lease
	if err := json.Unmarshal(envelope.Payload, &lease); err != nil {
		return entitlement.Lease{}, entitlement.ErrMalformed
	}
	return lease, nil
}

func leaseFilePath(lifecycle *LifecycleDependencies, explicit string) string {
	if strings.TrimSpace(explicit) != "" {
		return explicit
	}
	if env := strings.TrimSpace(os.Getenv("ARCHIE_LINK_LICENSE_FILE")); env != "" {
		return env
	}
	return filepath.Join(lifecycle.StateDir, "lease.json")
}

func parseLeaseOptions(args []string, stderr io.Writer) (leaseOptions, bool) {
	var options leaseOptions
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--json":
			options.jsonOutput = true
		case "--order", "--server", "--file", "--plan", "--request-id":
			option := args[i]
			if i+1 >= len(args) {
				fmt.Fprintf(stderr, "%s requires a value\n", option)
				return options, false
			}
			i++
			switch option {
			case "--order":
				options.order = args[i]
			case "--server":
				options.server = args[i]
			case "--file":
				options.file = args[i]
			case "--plan":
				options.plan = args[i]
			case "--request-id":
				options.requestID = args[i]
			}
		default:
			fmt.Fprintf(stderr, "unknown lease option: %s\n", args[i])
			return options, false
		}
	}
	return options, true
}

func clientURL(client leaseclient.Client, path string) string {
	base := strings.TrimSpace(client.BaseURL)
	if base == "" {
		base = leaseclient.DefaultServer
	}
	return strings.TrimRight(base, "/") + path
}

func writeLeaseFile(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".lease-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}
