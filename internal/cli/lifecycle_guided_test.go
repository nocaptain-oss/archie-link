package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/nocaptain-oss/archie-link/internal/model"
)

func guidedDeps(registry AdapterRegistry, goos string, lifecycle *LifecycleDependencies) Dependencies {
	return Dependencies{Registry: registry, GOOS: goos, Lifecycle: lifecycle}
}

func TestLifecycleGuidedWhenLifecycleNil(t *testing.T) {
	var stdout, stderr bytes.Buffer
	deps := guidedDeps(fakeRegistry{adapter: fakeAdapter{}}, "windows", nil)
	code := RunWithDeps([]string{"up", "--yes", "--json"}, &stdout, &stderr, deps)
	if code != 3 {
		t.Fatalf("exit = %d, want 3 (guided)", code)
	}
	var report model.GuidedReport
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatalf("decode guided report: %v\nstdout: %s", err, stdout.String())
	}
	if report.SchemaVersion != model.SchemaVersion {
		t.Errorf("schema_version = %q, want %q", report.SchemaVersion, model.SchemaVersion)
	}
	if report.Mode != model.ModeGuided {
		t.Errorf("mode = %q, want guided", report.Mode)
	}
	if report.Result != model.ResultUnsupported {
		t.Errorf("result = %q, want unsupported", report.Result)
	}
	if report.Platform.OS != "darwin" || report.Platform.Version != "12.7.6" {
		t.Errorf("platform = %+v, want inspect() detected values", report.Platform)
	}
	if !report.Capabilities.Shell {
		t.Errorf("capabilities.shell = false, want true from inspect()")
	}
	if report.Network.DefaultInterface != "en0" {
		t.Errorf("network.default_interface = %q, want en0", report.Network.DefaultInterface)
	}
	wantSteps := []string{"install_official_app", "prepare_config", "import_tunnel", "activate_and_verify"}
	if len(report.Steps) != len(wantSteps) {
		t.Fatalf("steps = %d, want %d", len(report.Steps), len(wantSteps))
	}
	for i, code := range wantSteps {
		if report.Steps[i].Code != code {
			t.Errorf("steps[%d].code = %q, want %q", i, report.Steps[i].Code, code)
		}
		if report.Steps[i].Message == "" {
			t.Errorf("steps[%d].message empty", i)
		}
	}
}

func TestLifecycleGuidedFallsBackToGOOSWhenInspectFails(t *testing.T) {
	var stdout, stderr bytes.Buffer
	deps := guidedDeps(fakeRegistry{err: errors.New("registry boom")}, "windows", nil)
	code := RunWithDeps([]string{"up", "--yes", "--json"}, &stdout, &stderr, deps)
	if code != 3 {
		t.Fatalf("exit = %d, want 3 (guided)", code)
	}
	var report model.GuidedReport
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatalf("decode guided report: %v", err)
	}
	if report.Platform.OS != "windows" {
		t.Errorf("platform.os = %q, want windows fallback", report.Platform.OS)
	}
	if report.Network.DNS == nil || report.Network.ExistingTunnels == nil {
		t.Error("network slices must be non-nil for stable JSON")
	}
	if len(report.Steps) != 4 {
		t.Errorf("steps = %d, want 4", len(report.Steps))
	}
}

func TestLifecycleGuidedWhenAutomaticNil(t *testing.T) {
	var stdout, stderr bytes.Buffer
	deps := guidedDeps(fakeRegistry{adapter: fakeAdapter{}}, "windows", &LifecycleDependencies{ConfigPath: "C:/warp.conf"})
	if code := RunWithDeps([]string{"up", "--yes", "--json"}, &stdout, &stderr, deps); code != 3 {
		t.Fatalf("exit = %d, want 3 (guided)", code)
	}
	var report model.GuidedReport
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatalf("decode guided report: %v", err)
	}
	if report.Mode != model.ModeGuided || len(report.Steps) != 4 {
		t.Errorf("mode = %q, steps = %d; want guided with 4 steps", report.Mode, len(report.Steps))
	}
}

func TestLifecycleGuidedDownSingleStep(t *testing.T) {
	var stdout, stderr bytes.Buffer
	deps := guidedDeps(fakeRegistry{adapter: fakeAdapter{}}, "windows", nil)
	code := RunWithDeps([]string{"down", "--json"}, &stdout, &stderr, deps)
	if code != 3 {
		t.Fatalf("exit = %d, want 3 (guided)", code)
	}
	var report model.GuidedReport
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatalf("decode guided report: %v", err)
	}
	if len(report.Steps) != 1 || report.Steps[0].Code != "manage_in_official_app" {
		t.Fatalf("steps = %+v, want single manage_in_official_app", report.Steps)
	}
}

func TestLifecycleGuidedTextOutput(t *testing.T) {
	var stdout, stderr bytes.Buffer
	deps := guidedDeps(fakeRegistry{err: errors.New("boom")}, "windows", nil)
	code := RunWithDeps([]string{"up", "--yes"}, &stdout, &stderr, deps)
	if code != 3 {
		t.Fatalf("exit = %d, want 3 (guided)", code)
	}
	out := stdout.String()
	if !strings.Contains(out, "mode=guided") || !strings.Contains(out, "result=unsupported") {
		t.Errorf("text output missing mode/result line: %q", out)
	}
	if !strings.Contains(out, "install_official_app") || !strings.Contains(out, "activate_and_verify") {
		t.Errorf("text output missing guided steps: %q", out)
	}
}

func TestLeaseGuardedWhenAutomaticNil(t *testing.T) {
	var stdout, stderr bytes.Buffer
	deps := guidedDeps(fakeRegistry{adapter: fakeAdapter{}}, "windows", &LifecycleDependencies{})
	code := RunWithDeps([]string{"lease", "identity"}, &stdout, &stderr, deps)
	if code != 3 {
		t.Fatalf("exit = %d, want 3 (lease unsupported)", code)
	}
	if !strings.Contains(stderr.String(), "unsupported_platform") {
		t.Errorf("stderr missing unsupported_platform: %q", stderr.String())
	}
}
