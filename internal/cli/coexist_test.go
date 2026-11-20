package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/nocaptain-oss/archie-link/internal/adapter"
	"github.com/nocaptain-oss/archie-link/internal/coexist"
	"github.com/nocaptain-oss/archie-link/internal/model"
)

func TestCoexistGateJSON(t *testing.T) {
	deps := Dependencies{
		Registry: fakeRegistry{adapter: fakeCoexistenceAdapter{}},
		GOOS:     "darwin",
	}
	var stdout, stderr bytes.Buffer
	code := RunWithDeps([]string{"coexist", "gate", "--json"}, &stdout, &stderr, deps)
	if code != 0 {
		t.Fatalf("exit=%d stderr=%s", code, stderr.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload["allowed"] != true {
		t.Fatalf("payload=%s", stdout.String())
	}
	if payload["schema_version"] != "1" {
		t.Fatalf("payload=%s", stdout.String())
	}
}

func TestCoexistBaselineJSON(t *testing.T) {
	dir := t.TempDir()
	profilePath := filepath.Join(dir, "endpoints.json")
	if err := os.WriteFile(profilePath, []byte(`{"endpoints":[{"id":"api","kind":"public_api","upstream_url":"https://example.com","expected":"not_403","baseline_required":true}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	deps := Dependencies{
		Registry:        fakeRegistry{adapter: fakeCoexistenceAdapter{}},
		GOOS:            "darwin",
		BaselineChecker: stubChecker{},
	}
	var stdout, stderr bytes.Buffer
	code := RunWithDeps([]string{"coexist", "baseline", "--profile", profilePath, "--json"}, &stdout, &stderr, deps)
	if code != 0 {
		t.Fatalf("exit=%d stderr=%s", code, stderr.String())
	}
	var payload []map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload) != 1 || payload[0]["ok"] != true {
		t.Fatalf("payload=%s", stdout.String())
	}
}

type fakeCoexistenceAdapter struct {
	fakeAdapter
}

func (fakeCoexistenceAdapter) Coexistence(context.Context) (coexist.Profile, error) {
	return coexist.Profile{
		SchemaVersion: model.SchemaVersion,
		OS:            "darwin",
		Routes: []coexist.RouteRecord{{
			Destination: "default", Gateway: "192.168.1.1", Interface: "en0", Owner: coexist.OwnerSystem,
		}},
		DNSResolvers:     []coexist.DNSResolver{{Address: "192.168.1.1", Owner: coexist.OwnerSystem}},
		RouteRules:       []coexist.RouteRule{},
		Proxies:          []coexist.ProxyRecord{},
		Listeners:        []coexist.ListenerRecord{},
		VPNs:             []coexist.VPNRecord{},
		Protected:        []coexist.ProtectedEndpoint{},
		IncompleteLayers: []coexist.Layer{},
		BaselineComplete: true,
	}, nil
}

var _ adapter.Adapter = fakeCoexistenceAdapter{}

type stubChecker struct{}

func (stubChecker) Status(context.Context, string) (int, error) {
	return 200, nil
}
