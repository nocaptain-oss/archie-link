package devicebridge

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

var (
	ErrADBUnavailable        = errors.New("adb is unavailable")
	ErrHDCUnavailable        = errors.New("hdc is unavailable")
	ErrDiscoveryFailed       = errors.New("device discovery failed")
	ErrInvalidAPK            = errors.New("APK path is invalid")
	ErrInvalidHAP            = errors.New("HAP path is invalid")
	ErrInstallFailed         = errors.New("Android companion installation failed")
	ErrHDCInstallFailed      = errors.New("HarmonyOS companion installation failed")
	ErrHDCLaunchFailed       = errors.New("HarmonyOS companion launch failed")
	ErrUnsupportedDeviceTool = errors.New("device discovery tool is not supported")
)

type Runner interface {
	Run(context.Context, string, ...string) ([]byte, error)
}

type ExecRunner struct{}

func (ExecRunner) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, args...).CombinedOutput()
}

type Host struct {
	Runner    Runner
	ADB       string
	HDC       string
	Connector RPCConnector
	Pairing   *PairingManager
	Sessions  SessionStore
}

func NewHost(runner Runner) *Host {
	host := &Host{
		Runner:   runner,
		ADB:      "adb",
		HDC:      defaultHDC(),
		Pairing:  NewPairingManager(nil, 0),
		Sessions: SessionStore{Path: defaultDeviceSessionPath()},
	}
	host.Connector = &ADBRPCConnector{Runner: runner, ADB: host.ADB}
	return host
}

type DiscoveryReport struct {
	SchemaVersion int      `json:"schema_version"`
	Platform      Platform `json:"platform"`
	Tool          string   `json:"tool"`
	Devices       []Device `json:"devices"`
}

type Device struct {
	Serial    string   `json:"serial"`
	State     string   `json:"state"`
	Platform  Platform `json:"platform"`
	Product   string   `json:"product,omitempty"`
	Model     string   `json:"model,omitempty"`
	Device    string   `json:"device,omitempty"`
	Transport string   `json:"transport,omitempty"`
}

type InstallReport struct {
	SchemaVersion int    `json:"schema_version"`
	Tool          string `json:"tool"`
	Serial        string `json:"serial"`
	Installed     bool   `json:"installed"`
}

func (h *Host) Discover(ctx context.Context) (DiscoveryReport, error) {
	return h.DiscoverTool(ctx, "adb")
}

func (h *Host) DiscoverTool(ctx context.Context, tool string) (DiscoveryReport, error) {
	switch strings.ToLower(strings.TrimSpace(tool)) {
	case "", "adb":
		return h.discoverADB(ctx)
	case "hdc":
		return h.discoverHDC(ctx)
	case "auto":
		report, adbErr := h.discoverADB(ctx)
		if adbErr == nil && len(report.Devices) > 0 {
			return report, nil
		}
		hdcReport, hdcErr := h.discoverHDC(ctx)
		if hdcErr == nil && len(hdcReport.Devices) > 0 {
			return hdcReport, nil
		}
		if hdcErr == nil {
			return hdcReport, nil
		}
		if adbErr == nil {
			return report, nil
		}
		return DiscoveryReport{}, fmt.Errorf(
			"%w: adb: %v; hdc: %v", ErrDiscoveryFailed, adbErr, hdcErr,
		)
	default:
		return DiscoveryReport{}, fmt.Errorf("%w: %s", ErrUnsupportedDeviceTool, tool)
	}
}

func (h *Host) discoverADB(ctx context.Context) (DiscoveryReport, error) {
	if h == nil || h.Runner == nil {
		return DiscoveryReport{}, ErrDiscoveryFailed
	}
	adb := strings.TrimSpace(h.ADB)
	if adb == "" {
		adb = "adb"
	}
	output, err := h.Runner.Run(ctx, adb, "devices", "-l")
	if err != nil {
		var execErr *exec.Error
		if errors.As(err, &execErr) {
			return DiscoveryReport{}, fmt.Errorf("%w: %v", ErrADBUnavailable, err)
		}
		return DiscoveryReport{}, fmt.Errorf("%w: %v", ErrDiscoveryFailed, err)
	}
	devices, err := ParseADBDevices(string(output))
	if err != nil {
		return DiscoveryReport{}, err
	}
	return DiscoveryReport{
		SchemaVersion: SchemaVersion,
		Platform:      PlatformAndroid,
		Tool:          "adb",
		Devices:       devices,
	}, nil
}

func (h *Host) discoverHDC(ctx context.Context) (DiscoveryReport, error) {
	if h == nil || h.Runner == nil {
		return DiscoveryReport{}, ErrDiscoveryFailed
	}
	hdc := strings.TrimSpace(h.HDC)
	if hdc == "" {
		hdc = "hdc"
	}
	output, err := h.Runner.Run(ctx, hdc, "list", "targets")
	if err != nil {
		var execErr *exec.Error
		if errors.As(err, &execErr) || strings.Contains(strings.ToLower(string(output)), "connect server failed") {
			return DiscoveryReport{}, fmt.Errorf("%w: %v", ErrHDCUnavailable, err)
		}
		return DiscoveryReport{}, fmt.Errorf("%w: %v", ErrDiscoveryFailed, err)
	}
	if strings.Contains(strings.ToLower(string(output)), "connect server failed") {
		return DiscoveryReport{}, fmt.Errorf("%w: %s", ErrHDCUnavailable, strings.TrimSpace(string(output)))
	}
	devices := ParseHDCDevices(string(output))
	return DiscoveryReport{
		SchemaVersion: SchemaVersion,
		Platform:      PlatformHarmonyOS,
		Tool:          "hdc",
		Devices:       devices,
	}, nil
}

func ParseHDCDevices(output string) []Device {
	devices := []Device{}
	for _, raw := range strings.Split(output, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.EqualFold(line, "[Empty]") ||
			strings.Contains(strings.ToLower(line), "connect server failed") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) == 0 || strings.HasPrefix(fields[0], "[") {
			continue
		}
		state := "device"
		lower := strings.ToLower(line)
		if strings.Contains(lower, "unauthorized") {
			state = "unauthorized"
		} else if strings.Contains(lower, "offline") {
			state = "offline"
		}
		devices = append(devices, Device{
			Serial:    fields[0],
			State:     state,
			Platform:  PlatformHarmonyOS,
			Transport: "hdc",
		})
	}
	return devices
}

func ParseADBDevices(output string) ([]Device, error) {
	devices := []Device{}
	for _, raw := range strings.Split(output, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "List of devices") || strings.HasPrefix(line, "* daemon") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		device := Device{
			Serial:   fields[0],
			State:    fields[1],
			Platform: PlatformAndroid,
		}
		for _, field := range fields[2:] {
			key, value, ok := strings.Cut(field, ":")
			if !ok {
				continue
			}
			switch key {
			case "product":
				device.Product = value
			case "model":
				device.Model = value
			case "device":
				device.Device = value
			case "transport_id":
				device.Transport = "adb"
			}
		}
		if strings.Contains(strings.ToLower(device.Product+" "+device.Model+" "+device.Device), "tv") {
			device.Platform = PlatformAndroidTV
		}
		devices = append(devices, device)
	}
	return devices, nil
}

func (h *Host) Install(ctx context.Context, serial, apkPath string) (InstallReport, error) {
	if h == nil || h.Runner == nil {
		return InstallReport{}, ErrInstallFailed
	}
	serial = strings.TrimSpace(serial)
	if serial == "" || strings.ContainsAny(serial, " \t\r\n") || strings.HasPrefix(serial, "-") {
		return InstallReport{}, fmt.Errorf("%w: invalid device serial", ErrInstallFailed)
	}
	apkPath = strings.TrimSpace(apkPath)
	extension := strings.ToLower(filepath.Ext(apkPath))
	invalidPackage := ErrInvalidAPK
	if extension == ".hap" {
		invalidPackage = ErrInvalidHAP
	} else if extension != ".apk" {
		return InstallReport{}, ErrInvalidAPK
	}
	info, err := os.Stat(apkPath)
	if err != nil || !info.Mode().IsRegular() || info.Size() == 0 {
		return InstallReport{}, invalidPackage
	}
	if extension == ".hap" {
		return h.installHAP(ctx, serial, apkPath)
	}
	adb := strings.TrimSpace(h.ADB)
	if adb == "" {
		adb = "adb"
	}
	output, err := h.Runner.Run(ctx, adb, "-s", serial, "install", "-r", apkPath)
	if err != nil {
		var execErr *exec.Error
		if errors.As(err, &execErr) {
			return InstallReport{}, fmt.Errorf("%w: %v", ErrADBUnavailable, err)
		}
		return InstallReport{}, fmt.Errorf("%w: %v: %s", ErrInstallFailed, err, strings.TrimSpace(string(output)))
	}
	if !strings.Contains(string(output), "Success") {
		return InstallReport{}, fmt.Errorf("%w: %s", ErrInstallFailed, strings.TrimSpace(string(output)))
	}
	return InstallReport{SchemaVersion: SchemaVersion, Tool: "adb", Serial: serial, Installed: true}, nil
}

func (h *Host) installHAP(ctx context.Context, serial, hapPath string) (InstallReport, error) {
	hdc := strings.TrimSpace(h.HDC)
	if hdc == "" {
		hdc = "hdc"
	}
	output, err := h.Runner.Run(ctx, hdc, "-t", serial, "install", "-r", hapPath)
	if err != nil {
		var execErr *exec.Error
		if errors.As(err, &execErr) {
			return InstallReport{}, fmt.Errorf("%w: %v", ErrHDCUnavailable, err)
		}
		return InstallReport{}, fmt.Errorf(
			"%w: %v: %s", ErrHDCInstallFailed, err, strings.TrimSpace(string(output)),
		)
	}
	lower := strings.ToLower(string(output))
	if !strings.Contains(lower, "success") && !strings.Contains(lower, "install bundle successfully") {
		return InstallReport{}, fmt.Errorf("%w: %s", ErrHDCInstallFailed, strings.TrimSpace(string(output)))
	}
	return InstallReport{SchemaVersion: SchemaVersion, Tool: "hdc", Serial: serial, Installed: true}, nil
}

func defaultHDC() string {
	if value := strings.TrimSpace(os.Getenv("ARCHIE_LINK_HDC")); value != "" {
		return value
	}
	if path, err := exec.LookPath("hdc"); err == nil {
		return path
	}
	if runtime.GOOS == "darwin" {
		path := "/Applications/DevEco-Studio.app/Contents/sdk/default/openharmony/toolchains/hdc"
		if info, err := os.Stat(path); err == nil && info.Mode().IsRegular() {
			return path
		}
	}
	return "hdc"
}
