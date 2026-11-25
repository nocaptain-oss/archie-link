package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/nocaptain-oss/archie-link/internal/app"
	"github.com/nocaptain-oss/archie-link/internal/leaseclient"
	"github.com/nocaptain-oss/archie-link/internal/model"
	appstate "github.com/nocaptain-oss/archie-link/internal/state"
)

func runLifecycle(command string, args []string, stdout, stderr io.Writer, deps Dependencies) int {
	jsonOutput := false
	yes := false
	for _, arg := range args {
		switch arg {
		case "--json":
			jsonOutput = true
		case "--yes":
			yes = true
		default:
			fmt.Fprintf(stderr, "unknown %s option: %s\n", command, arg)
			return 2
		}
	}
	if command == "up" && !yes {
		return writeLifecycleArgumentError(stdout, stderr, jsonOutput,
			errors.New("archielink up requires --yes"))
	}
	if command != "up" && yes {
		fmt.Fprintf(stderr, "%s does not accept --yes\n", command)
		return 2
	}
	if deps.LifecycleError != nil {
		return writeLifecycleError(stdout, stderr, jsonOutput, 5, "preflight_failed", deps.LifecycleError)
	}
	if deps.Lifecycle == nil || deps.Lifecycle.Automatic == nil {
		return runGuidedLifecycle(command, stdout, stderr, jsonOutput, deps)
	}

	lifecycle := deps.Lifecycle
	// up 前尽力在线刷新撤销 floor：在线客户端在 chargeback/退款后即时被拒，
	// 离线或控制面不可达时静默容忍，不阻塞也不报错。floor 只升不降。
	if command == "up" {
		bestEffortEpochRefresh(context.Background(), lifecycle)
	}
	service := app.New(
		lifecycle.Automatic,
		lifecycle.Entitlement,
		appstate.NewMachine(),
		lifecycle.Store,
		stderr,
	)
	ctx := context.Background()
	var result app.Result
	var err error
	switch command {
	case "up":
		result, err = service.Up(ctx, app.Options{
			Yes:        yes,
			InstallID:  lifecycle.InstallID,
			ConfigPath: lifecycle.ConfigPath,
		})
	case "down":
		result, err = service.Down(ctx)
	case "status":
		result, err = service.Status(ctx)
	case "rollback":
		result, err = service.Rollback(ctx)
	}
	if jsonOutput {
		if encodeErr := writeJSON(stdout, result); encodeErr != nil {
			fmt.Fprintf(stderr, "encode lifecycle result: %v\n", encodeErr)
			return 1
		}
	} else {
		fmt.Fprintf(stdout, "phase=%s connected=%t healthy=%t exit=%d\n",
			result.Phase, result.Connected, result.Healthy, result.ExitCode)
		if result.Error != "" {
			fmt.Fprintf(stdout, "error=%s\n", result.Error)
		}
	}
	if err != nil {
		var serviceErr *app.ServiceError
		if errors.As(err, &serviceErr) {
			fmt.Fprintf(stderr, "%s: %s\n", serviceErr.Code, serviceErr.Err)
			return serviceErr.ExitCode
		}
		fmt.Fprintf(stderr, "lifecycle failed: %v\n", err)
		return 1
	}
	return result.ExitCode
}

// epochRefreshServer 在测试中可替换控制面地址；生产固定为 leaseclient.DefaultServer。
var epochRefreshServer = leaseclient.DefaultServer

// bestEffortEpochRefresh 在 up 前尽力向控制面拉取订阅的撤销纪元并 ratchet 本地 floor。
// 任何一步失败（无租约、离线、超时、旧服务端 401）都静默返回，不影响命令执行。
func bestEffortEpochRefresh(ctx context.Context, lifecycle *LifecycleDependencies) {
	if lifecycle == nil || lifecycle.Store.Dir == "" {
		return
	}
	data, err := os.ReadFile(leaseFilePath(lifecycle, ""))
	if err != nil {
		return
	}
	lease, err := decodeLease(data)
	if err != nil || strings.TrimSpace(lease.LicenseID) == "" {
		return
	}
	client := leaseclient.Client{BaseURL: epochRefreshServer}
	fetchCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	epoch, err := client.Epoch(fetchCtx, lease.LicenseID)
	if err != nil || epoch <= 0 {
		return
	}
	state, err := lifecycle.Store.LoadLeaseState()
	if err != nil {
		return
	}
	if epoch > state.RevocationFloor {
		state.RevocationFloor = epoch
		_ = lifecycle.Store.SaveLeaseState(state)
	}
}

func runGuidedLifecycle(command string, stdout, stderr io.Writer, jsonOutput bool, deps Dependencies) int {
	report := buildGuidedReport(context.Background(), command, deps)
	if jsonOutput {
		if err := writeJSON(stdout, report); err != nil {
			fmt.Fprintf(stderr, "encode guided report: %v\n", err)
			return 1
		}
	} else {
		writeGuidedText(stdout, report)
	}
	return 3
}

func buildGuidedReport(ctx context.Context, command string, deps Dependencies) model.GuidedReport {
	report := model.GuidedReport{
		SchemaVersion: model.SchemaVersion,
		Mode:          model.ModeGuided,
		Platform:      model.PlatformReport{OS: deps.GOOS},
		Network:       model.NetworkReport{DNS: []string{}, ExistingTunnels: []string{}},
		Steps:         guidedSteps(command),
		Result:        model.ResultUnsupported,
	}
	if report.Platform.OS == "" {
		report.Platform.OS = runtime.GOOS
	}
	if deps.Registry != nil {
		if state, err := inspect(ctx, deps); err == nil {
			report.Platform = state.Platform
			report.Capabilities = state.Capabilities
			report.Network = state.Network
		}
	}
	return report
}

func guidedSteps(command string) []model.GuidedStep {
	if command != "up" {
		return []model.GuidedStep{{
			Code: "manage_in_official_app",
			Message: "archielink does not manage tunnels automatically on this platform; " +
				"enable, disable, and inspect the tunnel in the official WireGuard app.",
		}}
	}
	return []model.GuidedStep{
		{
			Code:    "install_official_app",
			Message: "Install the official WireGuard app for this platform (https://www.wireguard.com/install/).",
		},
		{
			Code: "prepare_config",
			Message: "Prepare a WireGuard config file (.conf). On a host with archielink automatic mode " +
				"(macOS) run: archielink export --format wireguard --include-private-key --output <path> " +
				"(0600; never paste full configs into chat, logs, or cloud storage).",
		},
		{
			Code:    "import_tunnel",
			Message: "In the WireGuard app: Add Tunnel -> Import tunnel(s) from file -> select the .conf file.",
		},
		{
			Code:    "activate_and_verify",
			Message: "Activate the tunnel in the WireGuard app, then verify connectivity in a browser.",
		},
	}
}

func writeGuidedText(w io.Writer, report model.GuidedReport) {
	fmt.Fprintf(w, "mode=%s result=%s platform=%s\n", report.Mode, report.Result, report.Platform.OS)
	for i, step := range report.Steps {
		fmt.Fprintf(w, "%d. [%s] %s\n", i+1, step.Code, step.Message)
	}
}

func writeLifecycleArgumentError(stdout, stderr io.Writer, jsonOutput bool, err error) int {
	return writeLifecycleError(stdout, stderr, jsonOutput, 2, "confirmation_required", err)
}

func writeLifecycleError(stdout, stderr io.Writer, jsonOutput bool, exitCode int, code string, err error) int {
	if jsonOutput {
		_ = writeJSON(stdout, map[string]any{
			"schema_version": "1",
			"error": map[string]string{
				"code":    code,
				"message": err.Error(),
			},
			"exit_code": exitCode,
		})
	}
	fmt.Fprintf(stderr, "%s: %v\n", code, err)
	return exitCode
}
