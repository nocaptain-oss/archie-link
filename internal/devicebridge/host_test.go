package devicebridge

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

type fakeRunner struct {
	output []byte
	err    error
	name   string
	args   []string
}

type vpnAppOpRunner struct {
	mode  string
	calls [][]string
}

type toolRunner struct {
	outputs map[string][]byte
}

func (r *toolRunner) Run(_ context.Context, name string, _ ...string) ([]byte, error) {
	return r.outputs[name], nil
}

func (r *vpnAppOpRunner) Run(_ context.Context, _ string, args ...string) ([]byte, error) {
	r.calls = append(r.calls, append([]string(nil), args...))
	if len(args) >= 8 && args[4] == "set" {
		r.mode = args[7]
		return nil, nil
	}
	if r.mode == "allow" {
		return []byte("ACTIVATE_VPN: allow;"), nil
	}
	return []byte("ACTIVATE_VPN: default;"), nil
}

func (f *fakeRunner) Run(_ context.Context, name string, args ...string) ([]byte, error) {
	f.name = name
	f.args = append([]string(nil), args...)
	return f.output, f.err
}

func TestDiscoverADBDevices(t *testing.T) {
	runner := &fakeRunner{output: []byte(
		"List of devices attached\n" +
			"emulator-5554 device product:sdk_gphone64 model:Pixel_8 device:emu64 transport_id:1\n" +
			"tvbox-1 device product:bravia model:BRAVIA_4K device:tv transport_id:2\n" +
			"phone-2 unauthorized usb:1-1 transport_id:3\n",
	)}
	report, err := NewHost(runner).Discover(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if runner.name != "adb" || len(runner.args) != 2 || runner.args[0] != "devices" || runner.args[1] != "-l" {
		t.Fatalf("unexpected command: %s %#v", runner.name, runner.args)
	}
	if len(report.Devices) != 3 || report.Devices[0].Platform != PlatformAndroid ||
		report.Devices[1].Platform != PlatformAndroidTV ||
		report.Devices[2].State != "unauthorized" {
		t.Fatalf("devices=%#v", report.Devices)
	}
}

func TestDiscoverReportsMissingADB(t *testing.T) {
	runner := &fakeRunner{err: &exec.Error{Name: "adb", Err: exec.ErrNotFound}}
	_, err := NewHost(runner).Discover(context.Background())
	if !errors.Is(err, ErrADBUnavailable) {
		t.Fatalf("missing adb=%v", err)
	}
}

func TestDiscoverHDCDevices(t *testing.T) {
	runner := &fakeRunner{output: []byte("harmony-1\n127.0.0.1:5555\n")}
	host := NewHost(runner)
	host.HDC = "hdc"
	report, err := host.DiscoverTool(context.Background(), "hdc")
	if err != nil {
		t.Fatal(err)
	}
	if runner.name != "hdc" || len(runner.args) != 2 ||
		runner.args[0] != "list" || runner.args[1] != "targets" {
		t.Fatalf("unexpected command: %s %#v", runner.name, runner.args)
	}
	if report.Platform != PlatformHarmonyOS || report.Tool != "hdc" ||
		len(report.Devices) != 2 || report.Devices[0].Transport != "hdc" {
		t.Fatalf("report=%#v", report)
	}
}

func TestDiscoverAutoFallsBackFromADBToHDC(t *testing.T) {
	runner := &toolRunner{outputs: map[string][]byte{
		"adb": []byte(""),
		"hdc": []byte("harmony-1\n"),
	}}
	host := NewHost(runner)
	host.HDC = "hdc"
	report, err := host.DiscoverTool(context.Background(), "auto")
	if err != nil {
		t.Fatal(err)
	}
	if report.Tool != "hdc" || len(report.Devices) != 1 {
		t.Fatalf("report=%#v", report)
	}
}

func TestInstallAPKRequiresLocalFileAndReportsSuccess(t *testing.T) {
	apk := filepath.Join(t.TempDir(), "companion.apk")
	if err := os.WriteFile(apk, []byte("debug-apk"), 0o600); err != nil {
		t.Fatal(err)
	}
	runner := &fakeRunner{output: []byte("Performing Streamed Install\nSuccess\n")}
	report, err := NewHost(runner).Install(context.Background(), "tvbox-1", apk)
	if err != nil {
		t.Fatal(err)
	}
	if !report.Installed || report.Serial != "tvbox-1" {
		t.Fatalf("report=%#v", report)
	}
	if runner.name != "adb" || len(runner.args) != 5 ||
		runner.args[0] != "-s" || runner.args[1] != "tvbox-1" ||
		runner.args[2] != "install" || runner.args[3] != "-r" || runner.args[4] != apk {
		t.Fatalf("unexpected command: %s %#v", runner.name, runner.args)
	}
}

func TestInstallRejectsNonAPK(t *testing.T) {
	if _, err := NewHost(&fakeRunner{}).Install(context.Background(), "serial-1", "/tmp/probe.html"); !errors.Is(err, ErrInvalidAPK) {
		t.Fatalf("non-apk=%v", err)
	}
}

func TestInstallHAPUsesFixedHDCInstallCommand(t *testing.T) {
	hap := filepath.Join(t.TempDir(), "companion.hap")
	if err := os.WriteFile(hap, []byte("debug-hap"), 0o600); err != nil {
		t.Fatal(err)
	}
	runner := &fakeRunner{output: []byte("install bundle successfully.\n")}
	host := NewHost(runner)
	host.HDC = "hdc"
	report, err := host.Install(context.Background(), "harmony-1", hap)
	if err != nil {
		t.Fatal(err)
	}
	if report.Tool != "hdc" || !report.Installed {
		t.Fatalf("report=%#v", report)
	}
	if runner.name != "hdc" || len(runner.args) != 5 ||
		runner.args[0] != "-t" || runner.args[1] != "harmony-1" ||
		runner.args[2] != "install" || runner.args[3] != "-r" || runner.args[4] != hap {
		t.Fatalf("unexpected command: %s %#v", runner.name, runner.args)
	}
}

func TestLaunchHAPUsesFixedHDCCommand(t *testing.T) {
	runner := &fakeRunner{output: []byte("start ability successfully.\n")}
	host := NewHost(runner)
	host.HDC = "hdc"
	report, err := host.LaunchTool(context.Background(), "harmony-1", "hdc")
	if err != nil {
		t.Fatal(err)
	}
	if !report.Launched {
		t.Fatalf("report=%#v", report)
	}
	if runner.name != "hdc" || len(runner.args) != 10 ||
		runner.args[0] != "-t" || runner.args[1] != "harmony-1" ||
		runner.args[2] != "shell" || runner.args[3] != "aa" ||
		runner.args[4] != "start" || runner.args[5] != "-a" ||
		runner.args[6] != "EntryAbility" || runner.args[7] != "-b" {
		t.Fatalf("unexpected command: %s %#v", runner.name, runner.args)
	}
	if runner.args[8] != companionPackage || runner.args[9] != "-D" {
		t.Fatalf("package arg: %#v", runner.args)
	}
}

func TestSetVPNAuthorizationUsesOnlyPackageScopedAppOp(t *testing.T) {
	runner := &vpnAppOpRunner{}
	report, err := NewHost(runner).SetVPNAuthorization(context.Background(), "tvbox-1", true)
	if err != nil {
		t.Fatal(err)
	}
	if !report.Authorized || report.Mode != "allow" || runner.mode != "allow" {
		t.Fatalf("report=%#v mode=%s", report, runner.mode)
	}
	if len(runner.calls) != 2 {
		t.Fatalf("calls=%#v", runner.calls)
	}
	first := runner.calls[0]
	if len(first) != 8 || first[0] != "-s" || first[1] != "tvbox-1" ||
		first[2] != "shell" || first[3] != "appops" || first[4] != "set" ||
		first[5] != companionPackage || first[6] != "ACTIVATE_VPN" || first[7] != "allow" {
		t.Fatalf("set args=%#v", first)
	}
}
