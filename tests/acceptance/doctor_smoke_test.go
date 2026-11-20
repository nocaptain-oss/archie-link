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

func TestDoctorSmoke(t *testing.T) {
	if os.Getenv("ARCHIE_LINK_ACCEPTANCE") != "1" {
		t.Skip("set ARCHIE_LINK_ACCEPTANCE=1 to run host acceptance")
	}

	binary := filepath.Join(t.TempDir(), "archielink")
	build := exec.Command("go", "build", "-o", binary, "./cmd/archielink")
	build.Dir = "../.."
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build failed: %v\n%s", err, output)
	}

	for _, command := range [][]string{
		{"detect", "--json"},
		{"plan", "--json"},
		{"doctor", "--json"},
	} {
		t.Run(command[0], func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			cmd := exec.Command(binary, command...)
			cmd.Stdout = &stdout
			cmd.Stderr = &stderr
			err := cmd.Run()
			if err != nil {
				exitCode := -1
				if exitError, ok := err.(*exec.ExitError); ok {
					exitCode = exitError.ExitCode()
				}
				if exitCode != 3 && exitCode != 5 {
					t.Fatalf("exit=%d stderr=%s", exitCode, stderr.String())
				}
			}
			var payload map[string]any
			if jsonErr := json.Unmarshal(stdout.Bytes(), &payload); jsonErr != nil {
				t.Fatalf("invalid JSON: %v\n%s", jsonErr, stdout.String())
			}
			if payload["schema_version"] != "1" {
				t.Fatalf("schema_version=%v", payload["schema_version"])
			}
		})
	}
}
