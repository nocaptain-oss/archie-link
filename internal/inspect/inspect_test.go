package inspect

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/nocaptain-oss/archie-link/internal/adapter"
	"github.com/nocaptain-oss/archie-link/internal/model"
)

type fakeRegistry struct {
	platform adapter.Adapter
	err      error
}

func (f fakeRegistry) For(string) (adapter.Adapter, error) {
	return f.platform, f.err
}

type fakeAdapter struct{}

func (fakeAdapter) Name() string { return "linux" }

func (fakeAdapter) Detect(context.Context) (model.PlatformReport, model.CapabilityReport, error) {
	return model.PlatformReport{
		OS: "linux", Version: "5.15.0", Arch: "amd64",
	}, model.CapabilityReport{
		Shell: true, Elevation: true, NetworkMutation: true,
	}, nil
}

func (fakeAdapter) Snapshot(context.Context) (model.NetworkReport, error) {
	return model.NetworkReport{
		DefaultInterface: "eth0",
		Gateway:          "172.17.0.1",
		DNS:              []string{"192.168.1.1"},
		ExistingTunnels:  []string{},
	}, nil
}

func TestInspectSelectsMode(t *testing.T) {
	state, err := Inspect(context.Background(), fakeRegistry{platform: fakeAdapter{}}, "linux")
	if err != nil {
		t.Fatal(err)
	}
	if state.Mode != model.ModeAuto {
		t.Fatalf("auto mode expected: %s", state.Mode)
	}
	if state.Platform.OS != "linux" {
		t.Fatalf("linux expected: %s", state.Platform.OS)
	}
}

func TestInspectRequiresRegistry(t *testing.T) {
	if _, err := Inspect(context.Background(), nil, "linux"); err == nil {
		t.Fatal("registry is required")
	}
}

func TestInspectPropagatesRegistryError(t *testing.T) {
	_, err := Inspect(context.Background(), fakeRegistry{err: adapter.ErrUnsupportedPlatform}, "plan9")
	if !errors.Is(err, adapter.ErrUnsupportedPlatform) {
		t.Fatalf("unsupported platform expected: %v", err)
	}
}

func TestDetectReportMatchesLocalSchema(t *testing.T) {
	state, _ := Inspect(context.Background(), fakeRegistry{platform: fakeAdapter{}}, "linux")
	raw, err := json.Marshal(state.DetectReport())
	if err != nil {
		t.Fatal(err)
	}
	var report model.DetectReport
	if err := json.Unmarshal(raw, &report); err != nil {
		t.Fatal(err)
	}
	if report.SchemaVersion != model.SchemaVersion {
		t.Fatalf("schema_version expected: %s", report.SchemaVersion)
	}
	if report.Mode != model.ModeAuto || report.Platform.OS != "linux" {
		t.Fatalf("report content mismatch: %+v", report)
	}
}

func TestPlanReportContainsActionsAndResult(t *testing.T) {
	state, _ := Inspect(context.Background(), fakeRegistry{platform: fakeAdapter{}}, "linux")
	plan := state.PlanReport()
	if plan.SchemaVersion != model.SchemaVersion {
		t.Fatalf("schema_version expected: %s", plan.SchemaVersion)
	}
	if len(plan.Actions) == 0 {
		t.Fatal("actions expected")
	}
	if len(plan.Permissions) == 0 {
		t.Fatal("permissions expected")
	}
	if plan.Result != model.ResultReady {
		t.Fatalf("ready result expected: %s", plan.Result)
	}
}

func TestDoctorReportDefaultsEmptyFindings(t *testing.T) {
	state, _ := Inspect(context.Background(), fakeRegistry{platform: fakeAdapter{}}, "linux")
	report := state.DoctorReport(nil)
	if report.Findings == nil {
		t.Fatal("findings must default to empty slice, not null")
	}
}

// RemoteProvider 的输出必须与本地命令 schema 一致。
func TestRemoteProviderSchemaParity(t *testing.T) {
	provider := &RemoteProvider{
		Registry: fakeRegistry{platform: fakeAdapter{}},
		GOOS:     "linux",
		Version:  "0.4.0-dev",
	}
	ctx := context.Background()

	hello, err := provider.Hello(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var helloPayload struct {
		SchemaVersion string `json:"schema_version"`
		App           string `json:"app"`
		Version       string `json:"version"`
		GOOS          string `json:"goos"`
	}
	if err := json.Unmarshal(hello, &helloPayload); err != nil {
		t.Fatal(err)
	}
	if helloPayload.App != "archielink" || helloPayload.Version != "0.4.0-dev" || helloPayload.GOOS != "linux" {
		t.Fatalf("hello payload mismatch: %s", hello)
	}

	caps, err := provider.Capabilities(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var capsPayload struct {
		SchemaVersion string                 `json:"schema_version"`
		Capabilities  model.CapabilityReport `json:"capabilities"`
	}
	if err := json.Unmarshal(caps, &capsPayload); err != nil {
		t.Fatal(err)
	}
	if !capsPayload.Capabilities.NetworkMutation {
		t.Fatalf("capabilities payload mismatch: %s", caps)
	}

	detect, err := provider.Detect(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var detectReport model.DetectReport
	if err := json.Unmarshal(detect, &detectReport); err != nil {
		t.Fatal(err)
	}
	if detectReport.Mode != model.ModeAuto || detectReport.Platform.OS != "linux" {
		t.Fatalf("remote detect must match local schema: %s", detect)
	}

	plan, err := provider.Plan(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var planReport model.PlanReport
	if err := json.Unmarshal(plan, &planReport); err != nil {
		t.Fatal(err)
	}
	if planReport.SchemaVersion != model.SchemaVersion || len(planReport.Actions) == 0 {
		t.Fatalf("remote plan must match local schema: %s", plan)
	}

	doctor, err := provider.Doctor(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var doctorReport model.DoctorReport
	if err := json.Unmarshal(doctor, &doctorReport); err != nil {
		t.Fatal(err)
	}
	if doctorReport.Findings == nil {
		t.Fatal("remote doctor findings must be non-null")
	}
}

func TestRemoteProviderPropagatesInspectError(t *testing.T) {
	provider := &RemoteProvider{
		Registry: fakeRegistry{err: adapter.ErrUnsupportedPlatform},
		GOOS:     "plan9",
	}
	if _, err := provider.Detect(context.Background()); !errors.Is(err, adapter.ErrUnsupportedPlatform) {
		t.Fatalf("unsupported platform expected: %v", err)
	}
	if _, err := provider.Hello(context.Background()); !errors.Is(err, adapter.ErrUnsupportedPlatform) {
		t.Fatalf("unsupported platform expected: %v", err)
	}
}
