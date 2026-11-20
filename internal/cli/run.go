package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/nocaptain-oss/archie-link/internal/adapter"
	"github.com/nocaptain-oss/archie-link/internal/adapter/linux"
	"github.com/nocaptain-oss/archie-link/internal/adapter/macos"
	"github.com/nocaptain-oss/archie-link/internal/adapter/windows"
	"github.com/nocaptain-oss/archie-link/internal/app"
	"github.com/nocaptain-oss/archie-link/internal/coexist"
	"github.com/nocaptain-oss/archie-link/internal/devicebridge"
	"github.com/nocaptain-oss/archie-link/internal/doctor"
	"github.com/nocaptain-oss/archie-link/internal/entitlement"
	wireguardexport "github.com/nocaptain-oss/archie-link/internal/export"
	inspection "github.com/nocaptain-oss/archie-link/internal/inspect"
	"github.com/nocaptain-oss/archie-link/internal/model"
	appstate "github.com/nocaptain-oss/archie-link/internal/state"
)

var (
	Version     = "0.4.0-dev"
	BuildCommit = ""
	BuildID     = ""
)

type AdapterRegistry interface {
	For(string) (adapter.Adapter, error)
}

type Dependencies struct {
	Registry        AdapterRegistry
	GOOS            string
	Prober          doctor.Prober
	BaselineChecker coexist.HTTPChecker
	Lifecycle       *LifecycleDependencies
	LifecycleError  error
	DeviceBridge    DeviceDiscovery
}

type DeviceDiscovery interface {
	Discover(context.Context) (devicebridge.DiscoveryReport, error)
	DiscoverTool(context.Context, string) (devicebridge.DiscoveryReport, error)
	Install(context.Context, string, string) (devicebridge.InstallReport, error)
	Pair(context.Context, string) (devicebridge.PairReport, error)
	Launch(context.Context, string) (devicebridge.LaunchReport, error)
	LaunchTool(context.Context, string, string) (devicebridge.LaunchReport, error)
	Status(context.Context, string) (devicebridge.DeviceStatusReport, error)
	Capabilities(context.Context, string) (devicebridge.CapabilitiesReport, error)
	Prepare(context.Context, string, string, []byte) (devicebridge.ActionReport, error)
	Start(context.Context, string) (devicebridge.ActionReport, error)
	Stop(context.Context, string) (devicebridge.ActionReport, error)
	SetVPNAuthorization(context.Context, string, bool) (devicebridge.VPNAuthorizationReport, error)
	CloseSession(context.Context, string) (devicebridge.CloseReport, error)
	VPNProbe(context.Context, string) (devicebridge.VPNProbeReport, error)
}

type LifecycleDependencies struct {
	Automatic   app.Automatic
	Entitlement entitlement.Provider
	Store       appstate.Store
	InstallID   string
	ConfigPath  string
	StateDir    string
}

func Run(args []string, stdout, stderr io.Writer) int {
	runner := adapter.GuardRunner{Next: adapter.ExecRunner{}}
	lifecycle, lifecycleErr := defaultLifecycleDependencies()
	return RunWithDeps(args, stdout, stderr, Dependencies{
		Registry: adapter.NewRegistry(
			macos.New(runner),
			windows.New(runner),
			linux.New(runner),
		),
		GOOS:            runtime.GOOS,
		Prober:          doctor.DefaultProbeTargets(),
		BaselineChecker: coexist.DefaultHTTPChecker{},
		Lifecycle:       lifecycle,
		LifecycleError:  lifecycleErr,
		DeviceBridge:    devicebridge.NewHost(devicebridge.ExecRunner{}),
	})
}

func RunWithDeps(args []string, stdout, stderr io.Writer, deps Dependencies) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "usage: archielink <version|detect|plan|doctor|coexist|lease|device|remote|up|down|status|rollback|export>")
		return 2
	}

	if args[0] == "coexist" {
		return runCoexist(args[1:], stdout, stderr, deps)
	}
	if args[0] == "lease" {
		return runLease(args[1:], stdout, stderr, deps)
	}
	if args[0] == "device" {
		return runDevice(args[1:], stdout, stderr, deps)
	}
	if args[0] == "remote" {
		return runRemote(args[1:], stdout, stderr, deps)
	}
	switch args[0] {
	case "up", "down", "status", "rollback":
		return runLifecycle(args[0], args[1:], stdout, stderr, deps)
	case "export":
		return runExport(args[1:], stdout, stderr, deps)
	}

	jsonOutput, outputPath, ok := parseSimpleFlags(args[1:])
	if !ok {
		fmt.Fprintln(stderr, "usage: archielink <version|detect|plan|doctor|coexist> [--json] [--output <path> (detect/doctor only)]")
		return 2
	}

	switch args[0] {
	case "version":
		if outputPath != "" {
			fmt.Fprintln(stderr, "version does not support --output")
			return 2
		}
		return writeVersion(stdout, stderr, jsonOutput)
	case "detect":
		return runDetect(stdout, stderr, deps, jsonOutput, outputPath)
	case "plan":
		if outputPath != "" {
			fmt.Fprintln(stderr, "plan does not support --output")
			return 2
		}
		return runPlan(stdout, stderr, deps, jsonOutput)
	case "doctor":
		return runDoctor(stdout, stderr, deps, jsonOutput, outputPath)
	default:
		fmt.Fprintf(stderr, "unknown command: %s\n", args[0])
		return 2
	}
}

func runDevice(args []string, stdout, stderr io.Writer, deps Dependencies) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "usage: archielink device <discover|install|pair|launch|status|capabilities|prepare|authorize-vpn|revoke-vpn|start|stop|close|vpn-probe> [options]")
		return 2
	}
	switch args[0] {
	case "discover":
		return runDeviceDiscover(args[1:], stdout, stderr, deps)
	case "install":
		return runDeviceInstall(args[1:], stdout, stderr, deps)
	case "pair":
		return runDevicePair(args[1:], stdout, stderr, deps)
	case "launch":
		return runDeviceLaunch(args[1:], stdout, stderr, deps)
	case "status":
		return runDeviceStatus(args[1:], stdout, stderr, deps)
	case "capabilities":
		return runDeviceCapabilities(args[1:], stdout, stderr, deps)
	case "prepare":
		return runDevicePrepare(args[1:], stdout, stderr, deps)
	case "authorize-vpn":
		return runDeviceVPNAuthorization("authorize-vpn", true, args[1:], stdout, stderr, deps)
	case "revoke-vpn":
		return runDeviceVPNAuthorization("revoke-vpn", false, args[1:], stdout, stderr, deps)
	case "start":
		return runDeviceAction("start", args[1:], stdout, stderr, deps)
	case "stop":
		return runDeviceAction("stop", args[1:], stdout, stderr, deps)
	case "close":
		return runDeviceClose(args[1:], stdout, stderr, deps)
	case "vpn-probe":
		return runDeviceVPNProbe(args[1:], stdout, stderr, deps)
	default:
		fmt.Fprintf(stderr, "unknown device command: %s\n", args[0])
		return 2
	}
}

func runDeviceDiscover(args []string, stdout, stderr io.Writer, deps Dependencies) int {
	tool := "adb"
	jsonOutput := false
	for index := 0; index < len(args); index++ {
		switch args[index] {
		case "--json":
			jsonOutput = true
		case "--tool":
			index++
			if index >= len(args) {
				fmt.Fprintln(stderr, "usage: archielink device discover [--tool adb|hdc|auto] [--json]")
				return 2
			}
			tool = strings.ToLower(strings.TrimSpace(args[index]))
		default:
			fmt.Fprintf(stderr, "unknown device discover option: %s\n", args[index])
			return 2
		}
	}
	if tool != "adb" && tool != "hdc" && tool != "auto" {
		return writeLifecycleError(stdout, stderr, jsonOutput, 2, "unsupported_device_tool",
			fmt.Errorf("unsupported device discovery tool: %s", tool))
	}
	if deps.DeviceBridge == nil {
		return writeLifecycleError(stdout, stderr, jsonOutput, 5, "device_bridge_unavailable",
			errors.New("device discovery dependency is not configured"))
	}
	report, err := deps.DeviceBridge.DiscoverTool(context.Background(), tool)
	if err != nil {
		if errors.Is(err, devicebridge.ErrADBUnavailable) ||
			errors.Is(err, devicebridge.ErrHDCUnavailable) ||
			errors.Is(err, devicebridge.ErrUnsupportedDeviceTool) {
			return writeLifecycleError(stdout, stderr, jsonOutput, 3, "device_bridge_unavailable", err)
		}
		return writeLifecycleError(stdout, stderr, jsonOutput, 5, "device_discovery_failed", err)
	}
	if jsonOutput {
		return encodeResult(stdout, stderr, report)
	}
	fmt.Fprintf(stdout, "platform=%s tool=%s devices=%d\n", report.Platform, report.Tool, len(report.Devices))
	for _, device := range report.Devices {
		fmt.Fprintf(stdout, "- %s %s %s\n", device.Serial, device.Platform, device.State)
	}
	return 0
}

func runDeviceInstall(args []string, stdout, stderr io.Writer, deps Dependencies) int {
	var serial, apkPath string
	var jsonOutput, confirmed bool
	for index := 0; index < len(args); index++ {
		switch args[index] {
		case "--json":
			jsonOutput = true
		case "--yes":
			confirmed = true
		case "--serial", "--package", "--apk":
			flag := args[index]
			index++
			if index >= len(args) {
				fmt.Fprintln(stderr, "usage: archielink device install --serial <serial> --package <path> --yes [--json]")
				return 2
			}
			if flag == "--serial" {
				serial = args[index]
			} else {
				apkPath = args[index]
			}
		default:
			fmt.Fprintf(stderr, "unknown device install option: %s\n", args[index])
			return 2
		}
	}
	if strings.TrimSpace(serial) == "" || strings.TrimSpace(apkPath) == "" {
		fmt.Fprintln(stderr, "usage: archielink device install --serial <serial> --package <path> --yes [--json]")
		return 2
	}
	if !confirmed {
		return writeLifecycleError(stdout, stderr, jsonOutput, 2, "confirmation_required",
			errors.New("device install requires --yes"))
	}
	if deps.DeviceBridge == nil {
		return writeLifecycleError(stdout, stderr, jsonOutput, 5, "device_bridge_unavailable",
			errors.New("device bridge dependency is not configured"))
	}
	report, err := deps.DeviceBridge.Install(context.Background(), serial, apkPath)
	if err != nil {
		switch {
		case errors.Is(err, devicebridge.ErrADBUnavailable):
			return writeLifecycleError(stdout, stderr, jsonOutput, 3, "device_bridge_unavailable", err)
		case errors.Is(err, devicebridge.ErrHDCUnavailable):
			return writeLifecycleError(stdout, stderr, jsonOutput, 3, "device_bridge_unavailable", err)
		case errors.Is(err, devicebridge.ErrInvalidAPK), errors.Is(err, devicebridge.ErrInvalidHAP):
			return writeLifecycleError(stdout, stderr, jsonOutput, 2, "invalid_package", err)
		default:
			return writeLifecycleError(stdout, stderr, jsonOutput, 5, "device_install_failed", err)
		}
	}
	if jsonOutput {
		return encodeResult(stdout, stderr, report)
	}
	fmt.Fprintf(stdout, "installed=true serial=%s tool=%s\n", report.Serial, report.Tool)
	return 0
}

func runDevicePair(args []string, stdout, stderr io.Writer, deps Dependencies) int {
	serial, jsonOutput, confirmed, ok := parseDeviceSessionArgs("pair", args, true, stderr)
	if !ok {
		return 2
	}
	if !confirmed {
		return writeLifecycleError(stdout, stderr, jsonOutput, 2, "confirmation_required",
			errors.New("device pair requires --yes"))
	}
	if deps.DeviceBridge == nil {
		return writeLifecycleError(stdout, stderr, jsonOutput, 5, "device_bridge_unavailable",
			errors.New("device bridge dependency is not configured"))
	}
	report, err := deps.DeviceBridge.Pair(context.Background(), serial)
	if err != nil {
		return writeDeviceError(stdout, stderr, jsonOutput, "device_pair_failed", err)
	}
	if jsonOutput {
		return encodeResult(stdout, stderr, report)
	}
	fmt.Fprintf(stdout, "paired=true serial=%s platform=%s code=%s expiry=%s fingerprint=%s\n",
		report.Serial, report.Platform, report.Code, report.ExpiresAt.Format(time.RFC3339), report.Fingerprint)
	return 0
}

func runDeviceLaunch(args []string, stdout, stderr io.Writer, deps Dependencies) int {
	var serial, tool string
	var jsonOutput bool
	tool = "adb"
	for index := 0; index < len(args); index++ {
		switch args[index] {
		case "--json":
			jsonOutput = true
		case "--serial", "--tool":
			flag := args[index]
			index++
			if index >= len(args) {
				fmt.Fprintln(stderr, "usage: archielink device launch --serial <serial> [--tool adb|hdc] [--json]")
				return 2
			}
			if flag == "--serial" {
				serial = args[index]
			} else {
				tool = strings.ToLower(strings.TrimSpace(args[index]))
			}
		default:
			fmt.Fprintf(stderr, "unknown device launch option: %s\n", args[index])
			return 2
		}
	}
	if strings.TrimSpace(serial) == "" || (tool != "adb" && tool != "hdc") {
		fmt.Fprintln(stderr, "usage: archielink device launch --serial <serial> [--tool adb|hdc] [--json]")
		return 2
	}
	if deps.DeviceBridge == nil {
		return writeLifecycleError(stdout, stderr, jsonOutput, 5, "device_bridge_unavailable",
			errors.New("device bridge dependency is not configured"))
	}
	report, err := deps.DeviceBridge.LaunchTool(context.Background(), serial, tool)
	if err != nil {
		return writeDeviceError(stdout, stderr, jsonOutput, "device_launch_failed", err)
	}
	if jsonOutput {
		return encodeResult(stdout, stderr, report)
	}
	fmt.Fprintf(stdout, "launched=%t serial=%s\n", report.Launched, report.Serial)
	return 0
}

func runDeviceStatus(args []string, stdout, stderr io.Writer, deps Dependencies) int {
	serial, jsonOutput, _, ok := parseDeviceSessionArgs("status", args, false, stderr)
	if !ok {
		return 2
	}
	if deps.DeviceBridge == nil {
		return writeLifecycleError(stdout, stderr, jsonOutput, 5, "device_bridge_unavailable",
			errors.New("device bridge dependency is not configured"))
	}
	report, err := deps.DeviceBridge.Status(context.Background(), serial)
	if err != nil {
		return writeDeviceError(stdout, stderr, jsonOutput, "device_status_failed", err)
	}
	if jsonOutput {
		return encodeResult(stdout, stderr, report)
	}
	fmt.Fprintf(stdout, "serial=%s state=%s platform=%s vpn_authorized=%t connection=%s health=%s routed_apps=%d\n",
		report.Serial, report.State, report.Status.Platform,
		report.Status.VPNAuthorized, report.Status.Connection, report.Status.Health,
		report.Status.RoutedApps)
	return 0
}

func runDeviceCapabilities(args []string, stdout, stderr io.Writer, deps Dependencies) int {
	serial, jsonOutput, _, ok := parseDeviceSessionArgs("capabilities", args, false, stderr)
	if !ok {
		return 2
	}
	if deps.DeviceBridge == nil {
		return writeLifecycleError(stdout, stderr, jsonOutput, 5, "device_bridge_unavailable",
			errors.New("device bridge dependency is not configured"))
	}
	report, err := deps.DeviceBridge.Capabilities(context.Background(), serial)
	if err != nil {
		return writeDeviceError(stdout, stderr, jsonOutput, "device_capabilities_failed", err)
	}
	if jsonOutput {
		return encodeResult(stdout, stderr, report)
	}
	fmt.Fprintf(stdout, "serial=%s state=%s capabilities=%s\n",
		report.Serial, report.State, string(report.Capabilities))
	return 0
}

func runDevicePrepare(args []string, stdout, stderr io.Writer, deps Dependencies) int {
	var serial, configPath, profile string
	var jsonOutput, confirmed bool
	for index := 0; index < len(args); index++ {
		switch args[index] {
		case "--json":
			jsonOutput = true
		case "--yes":
			confirmed = true
		case "--serial", "--config", "--profile":
			flag := args[index]
			index++
			if index >= len(args) {
				fmt.Fprintln(stderr, "usage: archielink device prepare --serial <serial> --config <path> --yes [--profile <name>] [--json]")
				return 2
			}
			switch flag {
			case "--serial":
				serial = args[index]
			case "--config":
				configPath = args[index]
			case "--profile":
				profile = args[index]
			}
		default:
			fmt.Fprintf(stderr, "unknown device prepare option: %s\n", args[index])
			return 2
		}
	}
	if strings.TrimSpace(serial) == "" || strings.TrimSpace(configPath) == "" {
		fmt.Fprintln(stderr, "usage: archielink device prepare --serial <serial> --config <path> --yes [--profile <name>] [--json]")
		return 2
	}
	if !confirmed {
		return writeLifecycleError(stdout, stderr, jsonOutput, 2, "confirmation_required",
			errors.New("device prepare requires --yes"))
	}
	if deps.DeviceBridge == nil {
		return writeLifecycleError(stdout, stderr, jsonOutput, 5, "device_bridge_unavailable",
			errors.New("device bridge dependency is not configured"))
	}
	if !strings.EqualFold(filepath.Ext(configPath), ".conf") {
		return writeLifecycleError(stdout, stderr, jsonOutput, 2, "invalid_device_config",
			errors.New("device config must be a WireGuard .conf file"))
	}
	info, err := os.Stat(configPath)
	if err != nil || !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > 64*1024 {
		return writeLifecycleError(stdout, stderr, jsonOutput, 2, "invalid_device_config",
			errors.New("device config must be a non-empty regular file up to 64 KiB"))
	}
	config, err := os.ReadFile(configPath)
	if err != nil {
		return writeLifecycleError(stdout, stderr, jsonOutput, 5, "device_config_unreadable", err)
	}
	if _, err := wireguardexport.WireGuard(config, true); err != nil {
		return writeLifecycleError(stdout, stderr, jsonOutput, 2, "invalid_device_config", err)
	}
	report, err := deps.DeviceBridge.Prepare(context.Background(), serial, profile, config)
	if err != nil {
		return writeDeviceError(stdout, stderr, jsonOutput, "device_prepare_failed", err)
	}
	if jsonOutput {
		return encodeResult(stdout, stderr, report)
	}
	fmt.Fprintf(stdout, "prepared=true serial=%s state=%s\n", report.Serial, report.State)
	return 0
}

func runDeviceAction(command string, args []string, stdout, stderr io.Writer, deps Dependencies) int {
	serial, jsonOutput, _, ok := parseDeviceSessionArgs(command, args, false, stderr)
	if !ok {
		return 2
	}
	if deps.DeviceBridge == nil {
		return writeLifecycleError(stdout, stderr, jsonOutput, 5, "device_bridge_unavailable",
			errors.New("device bridge dependency is not configured"))
	}
	var report devicebridge.ActionReport
	var err error
	if command == "start" {
		report, err = deps.DeviceBridge.Start(context.Background(), serial)
	} else {
		report, err = deps.DeviceBridge.Stop(context.Background(), serial)
	}
	if err != nil {
		return writeDeviceError(stdout, stderr, jsonOutput, "device_"+command+"_failed", err)
	}
	if jsonOutput {
		return encodeResult(stdout, stderr, report)
	}
	fmt.Fprintf(stdout, "serial=%s state=%s vpn_supported=%t vpn_authorized=%t prepared=%t connection=%s\n",
		report.Serial, report.State, report.Status.VPNSupported, report.Status.VPNAuthorized,
		report.Status.Prepared, report.Status.Connection)
	return 0
}

func runDeviceVPNAuthorization(
	command string, allow bool, args []string, stdout, stderr io.Writer, deps Dependencies,
) int {
	serial, jsonOutput, confirmed, ok := parseDeviceSessionArgs(command, args, true, stderr)
	if !ok {
		return 2
	}
	if !confirmed {
		return writeLifecycleError(stdout, stderr, jsonOutput, 2, "confirmation_required",
			fmt.Errorf("device %s requires --yes", command))
	}
	if deps.DeviceBridge == nil {
		return writeLifecycleError(stdout, stderr, jsonOutput, 5, "device_bridge_unavailable",
			errors.New("device bridge dependency is not configured"))
	}
	report, err := deps.DeviceBridge.SetVPNAuthorization(context.Background(), serial, allow)
	if err != nil {
		return writeDeviceError(stdout, stderr, jsonOutput, "device_vpn_authorization_failed", err)
	}
	if jsonOutput {
		return encodeResult(stdout, stderr, report)
	}
	fmt.Fprintf(stdout, "serial=%s mode=%s authorized=%t\n",
		report.Serial, report.Mode, report.Authorized)
	return 0
}

func runDeviceClose(args []string, stdout, stderr io.Writer, deps Dependencies) int {
	serial, jsonOutput, _, ok := parseDeviceSessionArgs("close", args, false, stderr)
	if !ok {
		return 2
	}
	if deps.DeviceBridge == nil {
		return writeLifecycleError(stdout, stderr, jsonOutput, 5, "device_bridge_unavailable",
			errors.New("device bridge dependency is not configured"))
	}
	report, err := deps.DeviceBridge.CloseSession(context.Background(), serial)
	if err != nil {
		return writeDeviceError(stdout, stderr, jsonOutput, "device_close_failed", err)
	}
	if jsonOutput {
		return encodeResult(stdout, stderr, report)
	}
	fmt.Fprintf(stdout, "closed=%t serial=%s\n", report.Closed, report.Serial)
	return 0
}

// runDeviceVPNProbe implements the HRM-001 host-side "当前是否已有其他 VPN"
// detection field for HarmonyOS devices: fixed-parameter read-only probes only.
func runDeviceVPNProbe(args []string, stdout, stderr io.Writer, deps Dependencies) int {
	serial, jsonOutput, _, ok := parseDeviceSessionArgs("vpn-probe", args, false, stderr)
	if !ok {
		return 2
	}
	if deps.DeviceBridge == nil {
		return writeLifecycleError(stdout, stderr, jsonOutput, 5, "device_bridge_unavailable",
			errors.New("device bridge dependency is not configured"))
	}
	report, err := deps.DeviceBridge.VPNProbe(context.Background(), serial)
	if err != nil {
		return writeDeviceError(stdout, stderr, jsonOutput, "vpn_probe_failed", err)
	}
	if jsonOutput {
		return encodeResult(stdout, stderr, report)
	}
	fmt.Fprintf(stdout, "serial=%s vpn_active=%t attribution=%s\n",
		report.Serial, report.VPNActive, report.Attribution)
	if len(report.Interfaces) > 0 {
		fmt.Fprintf(stdout, "interfaces=%s\n", strings.Join(report.Interfaces, ","))
	}
	return 0
}

func parseDeviceSessionArgs(command string, args []string, allowYes bool, stderr io.Writer) (
	serial string, jsonOutput, confirmed, ok bool,
) {
	usageSuffix := ""
	if allowYes {
		usageSuffix = " --yes"
	}
	for index := 0; index < len(args); index++ {
		switch args[index] {
		case "--json":
			jsonOutput = true
		case "--yes":
			if !allowYes {
				fmt.Fprintf(stderr, "%s does not accept --yes\n", command)
				return "", false, false, false
			}
			confirmed = true
		case "--serial":
			index++
			if index >= len(args) {
				fmt.Fprintf(stderr, "usage: archielink device %s --serial <serial>%s [--json]\n",
					command, usageSuffix)
				return "", false, false, false
			}
			serial = args[index]
		default:
			fmt.Fprintf(stderr, "unknown device %s option: %s\n", command, args[index])
			return "", false, false, false
		}
	}
	if strings.TrimSpace(serial) == "" {
		fmt.Fprintf(stderr, "usage: archielink device %s --serial <serial>%s [--json]\n",
			command, usageSuffix)
		return "", false, false, false
	}
	return serial, jsonOutput, confirmed, true
}

func writeDeviceError(stdout, stderr io.Writer, jsonOutput bool, fallback string, err error) int {
	exitCode := 5
	code := fallback
	switch {
	case errors.Is(err, devicebridge.ErrADBUnavailable), errors.Is(err, devicebridge.ErrRPCUnavailable):
		exitCode, code = 3, "device_bridge_unavailable"
	case errors.Is(err, devicebridge.ErrDeviceNotFound):
		exitCode, code = 4, "device_not_found"
	case errors.Is(err, devicebridge.ErrDeviceUnavailable):
		exitCode, code = 4, "device_unavailable"
	case errors.Is(err, devicebridge.ErrVPNUnsupported):
		exitCode, code = 4, "device_vpn_unsupported"
	case errors.Is(err, devicebridge.ErrNotPrepared):
		exitCode, code = 4, "device_not_prepared"
	case errors.Is(err, devicebridge.ErrTunnelUnavailable):
		exitCode, code = 4, "device_tunnel_unavailable"
	case errors.Is(err, devicebridge.ErrTunnelStart):
		exitCode, code = 4, "device_tunnel_start_failed"
	case errors.Is(err, devicebridge.ErrSessionMissing):
		exitCode, code = 4, "device_session_missing"
	case errors.Is(err, devicebridge.ErrSessionExpired):
		exitCode, code = 4, "device_session_expired"
	case errors.Is(err, devicebridge.ErrPairingCode), errors.Is(err, devicebridge.ErrPairingExpired),
		errors.Is(err, devicebridge.ErrPairingAttempts), errors.Is(err, devicebridge.ErrPairingFingerprint),
		errors.Is(err, devicebridge.ErrPairingProof):
		exitCode, code = 4, "device_pairing_failed"
	case errors.Is(err, devicebridge.ErrInvalidRequest):
		exitCode, code = 2, "invalid_device_request"
	case errors.Is(err, devicebridge.ErrVPNProbeUnavailable):
		exitCode, code = 4, "vpn_probe_unavailable"
	}
	return writeLifecycleError(stdout, stderr, jsonOutput, exitCode, code, err)
}

func parseSimpleFlags(args []string) (bool, string, bool) {
	jsonOutput := false
	outputPath := ""
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--json":
			jsonOutput = true
		case "--output":
			i++
			if i >= len(args) {
				return false, "", false
			}
			outputPath = args[i]
		default:
			return false, "", false
		}
	}
	return jsonOutput, outputPath, true
}

// writeReportFile writes a JSON report to outputPath (0600, refuses to overwrite).
// The file bytes are identical to what --json would print, so the file schema
// never drifts from stdout.
func writeReportFile(outputPath string, value any, stderr io.Writer) error {
	var buf bytes.Buffer
	encoder := json.NewEncoder(&buf)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		fmt.Fprintf(stderr, "encode report: %v\n", err)
		return err
	}
	file, err := os.OpenFile(outputPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			fmt.Fprintf(stderr, "refusing to overwrite existing file: %s\n", outputPath)
			return err
		}
		fmt.Fprintf(stderr, "open report output: %v\n", err)
		return err
	}
	defer file.Close()
	if _, err := file.Write(buf.Bytes()); err != nil {
		fmt.Fprintf(stderr, "write report output: %v\n", err)
		return err
	}
	return nil
}

func writeVersion(stdout, stderr io.Writer, jsonOutput bool) int {
	if jsonOutput {
		if err := writeJSON(stdout, map[string]string{
			"name":         "archielink",
			"version":      Version,
			"build_commit": BuildCommit,
			"build_id":     BuildID,
		}); err != nil {
			fmt.Fprintf(stderr, "encode version: %v\n", err)
			return 1
		}
		return 0
	}
	fmt.Fprintf(stdout, "archielink %s", Version)
	if BuildID != "" {
		fmt.Fprintf(stdout, " (%s)", BuildID)
	}
	fmt.Fprintln(stdout)
	return 0
}

func runDetect(stdout, stderr io.Writer, deps Dependencies, jsonOutput bool, outputPath string) int {
	state, err := inspection.Inspect(context.Background(), deps.Registry, deps.GOOS)
	if err != nil {
		return writeInspectionError(stdout, stderr, jsonOutput, err)
	}
	report := state.DetectReport()
	if outputPath != "" {
		if err := writeReportFile(outputPath, report, stderr); err != nil {
			return 2
		}
	}
	if jsonOutput {
		return encodeResult(stdout, stderr, report)
	}
	fmt.Fprintf(stdout, "mode=%s os=%s version=%s arch=%s gateway=%s dns=%s\n",
		report.Mode,
		report.Platform.OS,
		report.Platform.Version,
		report.Platform.Arch,
		emptyAsUnknown(report.Network.Gateway),
		joinOrNone(report.Network.DNS),
	)
	return 0
}

func runPlan(stdout, stderr io.Writer, deps Dependencies, jsonOutput bool) int {
	state, err := inspection.Inspect(context.Background(), deps.Registry, deps.GOOS)
	if err != nil {
		return writeInspectionError(stdout, stderr, jsonOutput, err)
	}
	plan := state.PlanReport()
	if jsonOutput {
		return encodeResult(stdout, stderr, plan)
	}
	fmt.Fprintf(stdout, "mode=%s result=%s permissions=%s\n",
		plan.Mode,
		plan.Result,
		joinOrNone(plan.Permissions),
	)
	for _, action := range plan.Actions {
		fmt.Fprintf(stdout, "- %s: %s\n", action.Code, action.Message)
	}
	return 0
}

func runDoctor(stdout, stderr io.Writer, deps Dependencies, jsonOutput bool, outputPath string) int {
	state, err := inspection.Inspect(context.Background(), deps.Registry, deps.GOOS)
	if err != nil {
		return writeInspectionError(stdout, stderr, jsonOutput, err)
	}
	report := state.DoctorReport(deps.Prober)
	if outputPath != "" {
		if err := writeReportFile(outputPath, report, stderr); err != nil {
			return 2
		}
	}

	if jsonOutput {
		return encodeResult(stdout, stderr, report)
	}
	fmt.Fprintf(stdout, "mode=%s os=%s findings=%d\n", report.Mode, report.Platform.OS, len(report.Findings))
	for _, finding := range report.Findings {
		fmt.Fprintf(stdout, "- %s [%s]: %s\n", finding.Code, finding.Level, finding.Message)
	}
	return 0
}

type inspectedState = inspection.Inspected

func inspect(ctx context.Context, deps Dependencies) (inspectedState, error) {
	return inspection.Inspect(ctx, deps.Registry, deps.GOOS)
}

func writeInspectionError(stdout, stderr io.Writer, jsonOutput bool, err error) int {
	exitCode := 5
	code := "preflight_failed"
	if errors.Is(err, adapter.ErrUnsupportedPlatform) {
		exitCode = 3
		code = "unsupported_platform"
	}
	if jsonOutput {
		_ = writeJSON(stdout, map[string]any{
			"schema_version": model.SchemaVersion,
			"error": map[string]string{
				"code":    code,
				"message": err.Error(),
			},
		})
	}
	fmt.Fprintf(stderr, "%s: %v\n", code, err)
	return exitCode
}

func encodeResult(stdout, stderr io.Writer, value any) int {
	if err := writeJSON(stdout, value); err != nil {
		fmt.Fprintf(stderr, "encode result: %v\n", err)
		return 1
	}
	return 0
}

func writeJSON(writer io.Writer, value any) error {
	encoder := json.NewEncoder(writer)
	encoder.SetEscapeHTML(false)
	return encoder.Encode(value)
}

func emptyAsUnknown(value string) string {
	if strings.TrimSpace(value) == "" {
		return "unknown"
	}
	return value
}

func joinOrNone(values []string) string {
	if len(values) == 0 {
		return "none"
	}
	return strings.Join(values, ",")
}
