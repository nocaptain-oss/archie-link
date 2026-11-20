//go:build acceptance

package acceptance

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestCoexistenceSmoke(t *testing.T) {
	if os.Getenv("ARCHIE_LINK_ACCEPTANCE") != "1" {
		t.Skip("set ARCHIE_LINK_ACCEPTANCE=1 to run host acceptance")
	}
	binary := filepath.Join(t.TempDir(), "archielink")
	build := exec.Command("go", "build", "-o", binary, "./cmd/archielink")
	build.Dir = "../.."
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build failed: %v\n%s", err, output)
	}

	inspect := runCoexistenceJSON(t, binary, "inspect")
	if inspect["schema_version"] != "1" || inspect["os"] == "" {
		t.Fatalf("inspect=%v", inspect)
	}
	gate := runCoexistenceJSON(t, binary, "gate")
	if gate["schema_version"] != "1" || gate["mode"] == "" {
		t.Fatalf("gate=%v", gate)
	}
	if _, ok := gate["allowed"].(bool); !ok {
		t.Fatalf("gate allowed missing: %v", gate)
	}
}

func runCoexistenceJSON(t *testing.T, binary, subcommand string) map[string]any {
	t.Helper()
	var stdout, stderr bytes.Buffer
	cmd := exec.Command(binary, "coexist", subcommand, "--json")
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("%s failed: %v stderr=%s", subcommand, err, stderr.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("%s invalid JSON: %v\n%s", subcommand, err, stdout.String())
	}
	return payload
}
