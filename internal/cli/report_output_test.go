package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func reportTestDeps() Dependencies {
	return Dependencies{
		Registry: fakeRegistry{adapter: fakeAdapter{}},
		GOOS:     "darwin",
	}
}

func TestDetectOutputFileMatchesStdoutJSON(t *testing.T) {
	deps := reportTestDeps()
	target := filepath.Join(t.TempDir(), "detect.json")
	var stdout, stderr bytes.Buffer
	code := RunWithDeps([]string{"detect", "--json", "--output", target}, &stdout, &stderr, deps)
	if code != 0 {
		t.Fatalf("exit=%d stderr=%s", code, stderr.String())
	}
	file, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(file, stdout.Bytes()) {
		t.Fatalf("file and stdout differ:\nfile=%s\nstdout=%s", file, stdout.Bytes())
	}
	info, err := os.Stat(target)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode=%v", info.Mode().Perm())
	}
}

func TestDetectOutputFileWorksWithTextMode(t *testing.T) {
	deps := reportTestDeps()
	target := filepath.Join(t.TempDir(), "detect.json")
	var stdout, stderr bytes.Buffer
	code := RunWithDeps([]string{"detect", "--output", target}, &stdout, &stderr, deps)
	if code != 0 {
		t.Fatalf("exit=%d stderr=%s", code, stderr.String())
	}
	file, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if len(file) == 0 || file[0] != '{' {
		t.Fatalf("report file must be JSON even in text mode: %q", file)
	}
}

func TestDoctorOutputFileMatchesStdoutJSON(t *testing.T) {
	deps := reportTestDeps()
	target := filepath.Join(t.TempDir(), "doctor.json")
	var stdout, stderr bytes.Buffer
	code := RunWithDeps([]string{"doctor", "--json", "--output", target}, &stdout, &stderr, deps)
	if code != 0 {
		t.Fatalf("exit=%d stderr=%s", code, stderr.String())
	}
	file, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(file, stdout.Bytes()) {
		t.Fatalf("file and stdout differ:\nfile=%s\nstdout=%s", file, stdout.Bytes())
	}
	info, err := os.Stat(target)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode=%v", info.Mode().Perm())
	}
}

func TestReportOutputRefusesOverwrite(t *testing.T) {
	deps := reportTestDeps()
	target := filepath.Join(t.TempDir(), "existing.json")
	if err := os.WriteFile(target, []byte("existing"), 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	code := RunWithDeps([]string{"doctor", "--output", target}, &stdout, &stderr, deps)
	if code != 2 {
		t.Fatalf("exit=%d stderr=%s", code, stderr.String())
	}
	file, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(file) != "existing" {
		t.Fatalf("existing file was overwritten: %q", file)
	}
}

func TestPlanRejectsOutputFlag(t *testing.T) {
	deps := reportTestDeps()
	target := filepath.Join(t.TempDir(), "plan.json")
	var stdout, stderr bytes.Buffer
	code := RunWithDeps([]string{"plan", "--output", target}, &stdout, &stderr, deps)
	if code != 2 || !bytes.Contains(stderr.Bytes(), []byte("--output")) {
		t.Fatalf("exit=%d stderr=%s", code, stderr.String())
	}
}

func TestVersionRejectsOutputFlag(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := RunWithDeps([]string{"version", "--output", "/dev/null"}, &stdout, &stderr, Dependencies{})
	if code != 2 || !bytes.Contains(stderr.Bytes(), []byte("--output")) {
		t.Fatalf("exit=%d stderr=%s", code, stderr.String())
	}
}
