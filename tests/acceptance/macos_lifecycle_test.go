//go:build darwin && acceptance

package acceptance

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestMacOSLifecycle(t *testing.T) {
	if os.Getenv("ARCHIE_LINK_ACCEPTANCE") != "1" {
		t.Skip("set ARCHIE_LINK_ACCEPTANCE=1 to run macOS lifecycle acceptance")
	}
	if os.Geteuid() != 0 {
		t.Skip("run macOS lifecycle acceptance as root so network writes can be rolled back")
	}
	configPath := os.Getenv("ARCHIE_LINK_WG_CONFIG")
	if configPath == "" {
		t.Skip("set ARCHIE_LINK_WG_CONFIG to a local WireGuard config path")
	}

	dir := t.TempDir()
	binary := filepath.Join(dir, "archielink")
	build := exec.Command("go", "build", "-tags=licensetest", "-buildvcs=false", "-o", binary, "./cmd/archielink")
	build.Dir = "../.."
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build failed: %v\n%s", err, output)
	}
	env := append(os.Environ(),
		"ARCHIE_LINK_TEST_LICENSE=1",
		"ARCHIE_LINK_STATE_DIR="+filepath.Join(dir, "state"),
		"ARCHIE_LINK_WG_CONFIG="+configPath,
	)
	run := func(args ...string) map[string]any {
		t.Helper()
		cmd := exec.Command(binary, args...)
		cmd.Env = env
		var stdout, stderr bytes.Buffer
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr
		if err := cmd.Run(); err != nil {
			t.Fatalf("%v failed: %v\nstdout=%s\nstderr=%s", args, err, stdout.String(), stderr.String())
		}
		var payload map[string]any
		if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
			t.Fatalf("invalid JSON for %v: %v\n%s", args, err, stdout.String())
		}
		return payload
	}

	t.Cleanup(func() {
		cleanup := exec.Command(binary, "down", "--json")
		cleanup.Env = env
		_ = cleanup.Run()
	})
	if result := run("up", "--yes", "--json"); result["connected"] != true || result["healthy"] != true {
		t.Fatalf("up result=%v", result)
	}
	if result := run("status", "--json"); result["connected"] != true {
		t.Fatalf("status result=%v", result)
	}
	if result := run("down", "--json"); result["connected"] != false {
		t.Fatalf("down result=%v", result)
	}
}
