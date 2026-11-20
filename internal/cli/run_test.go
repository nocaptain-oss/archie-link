package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/nocaptain-oss/archie-link/internal/adapter"
	"github.com/nocaptain-oss/archie-link/internal/devicebridge"
	"github.com/nocaptain-oss/archie-link/internal/model"
)

func TestVersionJSON(t *testing.T) {
	oldCommit, oldBuildID := BuildCommit, BuildID
	BuildCommit, BuildID = "abc123", "0.4.0-dev+abc123"
	t.Cleanup(func() {
		BuildCommit, BuildID = oldCommit, oldBuildID
	})
	var stdout, stderr bytes.Buffer
	if code := Run([]string{"version", "--json"}, &stdout, &stderr); code != 0 {
		t.Fatalf("exit=%d stderr=%s", code, stderr.String())
	}

	var got map[string]string
	if err := json.Unmarshal(stdout.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got["name"] != "archielink" || got["version"] != "0.4.0-dev" ||
		got["build_commit"] != "abc123" || got["build_id"] != "0.4.0-dev+abc123" {
		t.Fatalf("unexpected version payload: %#v", got)
	}
}

func TestUpRequiresYesBeforeLifecycleStarts(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := RunWithDeps([]string{"up", "--json"}, &stdout, &stderr, Dependencies{})
	if code != 2 {
		t.Fatalf("exit=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	var got map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got["exit_code"] != float64(2) {
		t.Fatalf("payload=%s", stdout.String())
	}
}

func TestDoctorJSON(t *testing.T) {
	deps := Dependencies{
		Registry: fakeRegistry{adapter: fakeAdapter{}},
		GOOS:     "darwin",
	}
	var stdout, stderr bytes.Buffer
	code := RunWithDeps([]string{"doctor", "--json"}, &stdout, &stderr, deps)
	if code != 0 {
		t.Fatalf("exit=%d stderr=%s", code, stderr.String())
	}

	var got map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got["schema_version"] != "1" || got["mode"] != "auto" {
		t.Fatalf("payload=%s", stdout.String())
	}
}

func TestUnknownPlatformReturnsExit3(t *testing.T) {
	deps := Dependencies{
		Registry: fakeRegistry{err: adapter.ErrUnsupportedPlatform},
		GOOS:     "plan9",
	}
	var stdout, stderr bytes.Buffer
	if code := RunWithDeps([]string{"detect", "--json"}, &stdout, &stderr, deps); code != 3 {
		t.Fatalf("exit=%d stderr=%s", code, stderr.String())
	}
}

func TestDeviceDiscoverJSON(t *testing.T) {
	deps := Dependencies{DeviceBridge: &fakeDeviceDiscovery{report: devicebridge.DiscoveryReport{
		SchemaVersion: devicebridge.SchemaVersion,
		Platform:      devicebridge.PlatformAndroid,
		Tool:          "adb",
		Devices: []devicebridge.Device{{
			Serial: "tvbox-1", State: "device", Platform: devicebridge.PlatformAndroidTV,
			Model: "BRAVIA_4K",
		}},
	}}}
	var stdout, stderr bytes.Buffer
	code := RunWithDeps([]string{"device", "discover", "--json"}, &stdout, &stderr, deps)
	if code != 0 {
		t.Fatalf("exit=%d stderr=%s", code, stderr.String())
	}
	var got devicebridge.DiscoveryReport
	if err := json.Unmarshal(stdout.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Devices) != 1 || got.Devices[0].Serial != "tvbox-1" ||
		got.Devices[0].Platform != devicebridge.PlatformAndroidTV {
		t.Fatalf("payload=%s", stdout.String())
	}
}

func TestDeviceDiscoverMissingADBReturnsExit3(t *testing.T) {
	deps := Dependencies{DeviceBridge: &fakeDeviceDiscovery{err: devicebridge.ErrADBUnavailable}}
	var stdout, stderr bytes.Buffer
	if code := RunWithDeps([]string{"device", "discover", "--json"}, &stdout, &stderr, deps); code != 3 {
		t.Fatalf("exit=%d stderr=%s", code, stderr.String())
	}
}

func TestDeviceDiscoverHDCUsesRequestedTool(t *testing.T) {
	discovery := fakeDeviceDiscovery{report: devicebridge.DiscoveryReport{
		SchemaVersion: devicebridge.SchemaVersion,
		Platform:      devicebridge.PlatformHarmonyOS,
		Tool:          "hdc",
		Devices: []devicebridge.Device{{
			Serial: "harmony-1", State: "device", Platform: devicebridge.PlatformHarmonyOS,
		}},
	}}
	var stdout, stderr bytes.Buffer
	code := RunWithDeps([]string{
		"device", "discover", "--tool", "hdc", "--json",
	}, &stdout, &stderr, Dependencies{DeviceBridge: &discovery})
	if code != 0 {
		t.Fatalf("exit=%d stderr=%s", code, stderr.String())
	}
	if discovery.discoveredTool != "hdc" {
		t.Fatalf("tool=%q", discovery.discoveredTool)
	}
	var got devicebridge.DiscoveryReport
	if err := json.Unmarshal(stdout.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Tool != "hdc" || got.Devices[0].Platform != devicebridge.PlatformHarmonyOS {
		t.Fatalf("payload=%s", stdout.String())
	}
}

func TestDeviceLaunchHDCUsesRequestedTool(t *testing.T) {
	bridge := fakeDeviceDiscovery{launchReport: devicebridge.LaunchReport{
		SchemaVersion: devicebridge.SchemaVersion,
		Serial:        "harmony-1",
		Launched:      true,
	}}
	var stdout, stderr bytes.Buffer
	code := RunWithDeps([]string{
		"device", "launch", "--serial", "harmony-1", "--tool", "hdc", "--json",
	}, &stdout, &stderr, Dependencies{DeviceBridge: &bridge})
	if code != 0 {
		t.Fatalf("exit=%d stderr=%s", code, stderr.String())
	}
	if bridge.launchedTool != "hdc" {
		t.Fatalf("tool=%q", bridge.launchedTool)
	}
}

func TestDeviceVPNProbeReportsOtherVPN(t *testing.T) {
	bridge := fakeDeviceDiscovery{vpnProbeReport: devicebridge.VPNProbeReport{
		SchemaVersion: devicebridge.SchemaVersion,
		Serial:        "harmony-1",
		Tool:          "hdc",
		VPNActive:     true,
		Interfaces:    []string{"vpn-tun"},
		Attribution:   "other",
		Probes:        []devicebridge.VPNProbeDetail{{Command: "hdc -t harmony-1 shell ifconfig", OK: true}},
	}}
	var stdout, stderr bytes.Buffer
	code := RunWithDeps([]string{
		"device", "vpn-probe", "--serial", "harmony-1", "--json",
	}, &stdout, &stderr, Dependencies{DeviceBridge: &bridge})
	if code != 0 {
		t.Fatalf("exit=%d stderr=%s", code, stderr.String())
	}
	var got devicebridge.VPNProbeReport
	if err := json.Unmarshal(stdout.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if !got.VPNActive || got.Attribution != "other" || len(got.Interfaces) != 1 {
		t.Fatalf("payload=%s", stdout.String())
	}
}

func TestDeviceVPNProbeRequiresSerial(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := RunWithDeps([]string{
		"device", "vpn-probe", "--json",
	}, &stdout, &stderr, Dependencies{DeviceBridge: &fakeDeviceDiscovery{}})
	if code != 2 {
		t.Fatalf("exit=%d stderr=%s", code, stderr.String())
	}
}

func TestDeviceVPNProbeUnavailableReturnsExit4(t *testing.T) {
	bridge := fakeDeviceDiscovery{vpnProbeErr: devicebridge.ErrVPNProbeUnavailable}
	var stdout, stderr bytes.Buffer
	code := RunWithDeps([]string{
		"device", "vpn-probe", "--serial", "harmony-1", "--json",
	}, &stdout, &stderr, Dependencies{DeviceBridge: &bridge})
	if code != 4 {
		t.Fatalf("exit=%d stderr=%s", code, stderr.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	wrapper, _ := payload["error"].(map[string]any)
	if wrapper["code"] != "vpn_probe_unavailable" {
		t.Fatalf("payload=%s", stdout.String())
	}
}

func TestDeviceInstallRequiresYesAndInstallsAPK(t *testing.T) {
	discovery := fakeDeviceDiscovery{installReport: devicebridge.InstallReport{
		SchemaVersion: devicebridge.SchemaVersion, Tool: "adb", Serial: "serial-1", Installed: true,
	}}
	var stdout, stderr bytes.Buffer
	code := RunWithDeps([]string{
		"device", "install", "--serial", "serial-1", "--apk", "/tmp/companion.apk", "--json",
	}, &stdout, &stderr, Dependencies{DeviceBridge: &discovery})
	if code != 2 {
		t.Fatalf("without yes exit=%d", code)
	}
	stdout.Reset()
	stderr.Reset()
	code = RunWithDeps([]string{
		"device", "install", "--serial", "serial-1", "--apk", "/tmp/companion.apk", "--yes", "--json",
	}, &stdout, &stderr, Dependencies{DeviceBridge: &discovery})
	if code != 0 {
		t.Fatalf("exit=%d stderr=%s", code, stderr.String())
	}
	var got devicebridge.InstallReport
	if err := json.Unmarshal(stdout.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if !got.Installed || got.Serial != "serial-1" {
		t.Fatalf("payload=%s", stdout.String())
	}
}

func TestDevicePairRequiresYesAndStatusRoundTrip(t *testing.T) {
	bridge := fakeDeviceDiscovery{
		pairReport: devicebridge.PairReport{
			SchemaVersion: devicebridge.SchemaVersion,
			Serial:        "serial-1",
			Platform:      devicebridge.PlatformAndroidTV,
			Code:          "123456",
		},
		statusReport: devicebridge.DeviceStatusReport{
			SchemaVersion: devicebridge.SchemaVersion,
			Serial:        "serial-1",
			State:         "ready",
			Status: devicebridge.StatusPayload{
				Platform:   devicebridge.PlatformAndroidTV,
				Connection: "disconnected",
				Health:     "unknown",
			},
		},
	}
	var stdout, stderr bytes.Buffer
	if code := RunWithDeps([]string{"device", "pair", "--serial", "serial-1", "--json"},
		&stdout, &stderr, Dependencies{DeviceBridge: &bridge}); code != 2 {
		t.Fatalf("without yes exit=%d", code)
	}
	stdout.Reset()
	stderr.Reset()
	if code := RunWithDeps([]string{"device", "pair", "--serial", "serial-1", "--yes", "--json"},
		&stdout, &stderr, Dependencies{DeviceBridge: &bridge}); code != 0 {
		t.Fatalf("pair exit=%d stderr=%s", code, stderr.String())
	}
	var pair devicebridge.PairReport
	if err := json.Unmarshal(stdout.Bytes(), &pair); err != nil {
		t.Fatal(err)
	}
	if pair.Code != "123456" || !bridge.paired {
		t.Fatalf("pair=%#v called=%t", pair, bridge.paired)
	}
	stdout.Reset()
	stderr.Reset()
	if code := RunWithDeps([]string{"device", "status", "--serial", "serial-1", "--json"},
		&stdout, &stderr, Dependencies{DeviceBridge: &bridge}); code != 0 {
		t.Fatalf("status exit=%d stderr=%s", code, stderr.String())
	}
	var status devicebridge.DeviceStatusReport
	if err := json.Unmarshal(stdout.Bytes(), &status); err != nil {
		t.Fatal(err)
	}
	if status.State != "ready" || !bridge.statusCalled {
		t.Fatalf("status=%#v called=%t", status, bridge.statusCalled)
	}
}

func TestDevicePrepareRequiresYesAndStartsAuthorization(t *testing.T) {
	config := filepath.Join(t.TempDir(), "warp.conf")
	if err := os.WriteFile(config, []byte(
		"[Interface]\nPrivateKey = test\n\n[Peer]\nPublicKey = test\n",
	), 0o600); err != nil {
		t.Fatal(err)
	}
	bridge := fakeDeviceDiscovery{
		prepareReport: devicebridge.ActionReport{
			SchemaVersion: devicebridge.SchemaVersion, Serial: "serial-1", State: "prepared",
		},
		startReport: devicebridge.ActionReport{
			SchemaVersion: devicebridge.SchemaVersion, Serial: "serial-1",
			State: "authorization_required",
		},
	}
	var stdout, stderr bytes.Buffer
	if code := RunWithDeps([]string{
		"device", "prepare", "--serial", "serial-1", "--config", config, "--json",
	}, &stdout, &stderr, Dependencies{DeviceBridge: &bridge}); code != 2 {
		t.Fatalf("without yes exit=%d", code)
	}
	stdout.Reset()
	stderr.Reset()
	if code := RunWithDeps([]string{
		"device", "prepare", "--serial", "serial-1", "--config", config, "--yes", "--json",
	}, &stdout, &stderr, Dependencies{DeviceBridge: &bridge}); code != 0 {
		t.Fatalf("prepare exit=%d stderr=%s", code, stderr.String())
	}
	stdout.Reset()
	stderr.Reset()
	if code := RunWithDeps([]string{
		"device", "start", "--serial", "serial-1", "--json",
	}, &stdout, &stderr, Dependencies{DeviceBridge: &bridge}); code != 0 {
		t.Fatalf("start exit=%d stderr=%s", code, stderr.String())
	}
	var started devicebridge.ActionReport
	if err := json.Unmarshal(stdout.Bytes(), &started); err != nil {
		t.Fatal(err)
	}
	if started.State != "authorization_required" {
		t.Fatalf("start=%#v", started)
	}
}

func TestDeviceVPNUnsupportedReturnsExit4(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := writeDeviceError(&stdout, &stderr, true, "device_start_failed",
		devicebridge.ErrVPNUnsupported)
	if code != 4 {
		t.Fatalf("exit=%d", code)
	}
	var got map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	errorValue, _ := got["error"].(map[string]any)
	if errorValue["code"] != "device_vpn_unsupported" {
		t.Fatalf("payload=%s", stdout.String())
	}
}

func TestDeviceAuthorizeVPNRequiresYes(t *testing.T) {
	bridge := fakeDeviceDiscovery{vpnAuthReport: devicebridge.VPNAuthorizationReport{
		SchemaVersion: devicebridge.SchemaVersion,
		Serial:        "serial-1",
		Mode:          "allow",
		Authorized:    true,
	}}
	var stdout, stderr bytes.Buffer
	if code := RunWithDeps([]string{
		"device", "authorize-vpn", "--serial", "serial-1", "--json",
	}, &stdout, &stderr, Dependencies{DeviceBridge: &bridge}); code != 2 {
		t.Fatalf("without yes exit=%d", code)
	}
	stdout.Reset()
	stderr.Reset()
	if code := RunWithDeps([]string{
		"device", "authorize-vpn", "--serial", "serial-1", "--yes", "--json",
	}, &stdout, &stderr, Dependencies{DeviceBridge: &bridge}); code != 0 {
		t.Fatalf("authorize exit=%d stderr=%s", code, stderr.String())
	}
	var report devicebridge.VPNAuthorizationReport
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if !report.Authorized || report.Mode != "allow" {
		t.Fatalf("report=%#v", report)
	}
}

type fakeDeviceDiscovery struct {
	report          devicebridge.DiscoveryReport
	err             error
	installReport   devicebridge.InstallReport
	installErr      error
	pairReport      devicebridge.PairReport
	pairErr         error
	launchReport    devicebridge.LaunchReport
	launchErr       error
	statusReport    devicebridge.DeviceStatusReport
	statusErr       error
	capabilities    devicebridge.CapabilitiesReport
	capabilitiesErr error
	prepareReport   devicebridge.ActionReport
	prepareErr      error
	startReport     devicebridge.ActionReport
	startErr        error
	stopReport      devicebridge.ActionReport
	stopErr         error
	vpnAuthReport   devicebridge.VPNAuthorizationReport
	vpnAuthErr      error
	closeReport     devicebridge.CloseReport
	closeErr        error
	vpnProbeReport  devicebridge.VPNProbeReport
	vpnProbeErr     error
	paired          bool
	statusCalled    bool
	closed          bool
	discoveredTool  string
	launchedTool    string
}

func (f *fakeDeviceDiscovery) Discover(context.Context) (devicebridge.DiscoveryReport, error) {
	return f.report, f.err
}

func (f *fakeDeviceDiscovery) DiscoverTool(
	_ context.Context, tool string,
) (devicebridge.DiscoveryReport, error) {
	f.discoveredTool = tool
	return f.report, f.err
}

func (f *fakeDeviceDiscovery) Install(context.Context, string, string) (devicebridge.InstallReport, error) {
	return f.installReport, f.installErr
}

func (f *fakeDeviceDiscovery) Pair(context.Context, string) (devicebridge.PairReport, error) {
	f.paired = true
	return f.pairReport, f.pairErr
}

func (f *fakeDeviceDiscovery) Launch(context.Context, string) (devicebridge.LaunchReport, error) {
	return f.launchReport, f.launchErr
}

func (f *fakeDeviceDiscovery) LaunchTool(
	_ context.Context, _ string, tool string,
) (devicebridge.LaunchReport, error) {
	f.launchedTool = tool
	return f.launchReport, f.launchErr
}

func (f *fakeDeviceDiscovery) Status(context.Context, string) (devicebridge.DeviceStatusReport, error) {
	f.statusCalled = true
	return f.statusReport, f.statusErr
}

func (f *fakeDeviceDiscovery) Capabilities(context.Context, string) (devicebridge.CapabilitiesReport, error) {
	return f.capabilities, f.capabilitiesErr
}

func (f *fakeDeviceDiscovery) Prepare(context.Context, string, string, []byte) (devicebridge.ActionReport, error) {
	return f.prepareReport, f.prepareErr
}

func (f *fakeDeviceDiscovery) Start(context.Context, string) (devicebridge.ActionReport, error) {
	return f.startReport, f.startErr
}

func (f *fakeDeviceDiscovery) Stop(context.Context, string) (devicebridge.ActionReport, error) {
	return f.stopReport, f.stopErr
}

func (f *fakeDeviceDiscovery) SetVPNAuthorization(
	context.Context, string, bool,
) (devicebridge.VPNAuthorizationReport, error) {
	return f.vpnAuthReport, f.vpnAuthErr
}

func (f *fakeDeviceDiscovery) CloseSession(context.Context, string) (devicebridge.CloseReport, error) {
	f.closed = true
	return f.closeReport, f.closeErr
}

func (f *fakeDeviceDiscovery) VPNProbe(context.Context, string) (devicebridge.VPNProbeReport, error) {
	return f.vpnProbeReport, f.vpnProbeErr
}

type fakeRegistry struct {
	adapter adapter.Adapter
	err     error
}

func (f fakeRegistry) For(string) (adapter.Adapter, error) {
	return f.adapter, f.err
}

type fakeAdapter struct {
	detectErr error
}

func (fakeAdapter) Name() string { return "darwin" }

func (f fakeAdapter) Detect(context.Context) (model.PlatformReport, model.CapabilityReport, error) {
	if f.detectErr != nil {
		return model.PlatformReport{}, model.CapabilityReport{}, f.detectErr
	}
	return model.PlatformReport{
		OS: "darwin", Version: "12.7.6", Arch: "amd64",
	}, model.CapabilityReport{
		Shell: true, Elevation: true, NetworkMutation: true,
	}, nil
}

func (fakeAdapter) Snapshot(context.Context) (model.NetworkReport, error) {
	return model.NetworkReport{
		DefaultInterface: "en0",
		Gateway:          "192.168.1.1",
		DNS:              []string{"192.168.1.1"},
		ExistingTunnels:  []string{},
	}, nil
}
