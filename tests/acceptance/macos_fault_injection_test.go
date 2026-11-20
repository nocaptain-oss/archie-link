//go:build darwin && acceptance

package acceptance

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestMacOSFaultInjection(t *testing.T) {
	if os.Getenv("ARCHIE_LINK_ACCEPTANCE") != "1" {
		t.Skip("set ARCHIE_LINK_ACCEPTANCE=1 to run macOS fault injection")
	}
	if os.Geteuid() != 0 {
		t.Skip("run macOS fault injection as root")
	}
	configPath := os.Getenv("ARCHIE_LINK_WG_CONFIG")
	if configPath == "" {
		t.Skip("set ARCHIE_LINK_WG_CONFIG to a local WireGuard config path")
	}
	if strings.TrimSpace(processOutput(t, "wireguard-go")) != "" {
		t.Skip("refusing fault injection while another wireguard-go process exists")
	}
	if _, err := os.Stat("/etc/resolver/api.deepseek.com"); err == nil {
		t.Skip("refusing fault injection while an unmanaged DeepSeek resolver exists")
	}
	beforeDNS := commandOutput(t, "networksetup", "-getdnsservers", "Wi-Fi")
	beforeGateway := commandOutput(t, "route", "-n", "get", "default")

	dir := t.TempDir()
	binary := filepath.Join(dir, "archielink-fault")
	build := exec.Command("go", "build", "-tags=faultinjection,licensetest", "-buildvcs=false", "-o", binary, "./cmd/archielink")
	build.Dir = "../.."
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("fault-injection build failed: %v\n%s", err, output)
	}

	cases := []struct {
		mode     string
		exitCode int
		phase    string
	}{
		{mode: "unavailable_endpoint", exitCode: 5, phase: "planned"},
		{mode: "dns_write_failure", exitCode: 6, phase: "restored"},
		{mode: "daemon_exit", exitCode: 6, phase: "restored"},
		{mode: "health_403", exitCode: 7, phase: "restored"},
	}
	for _, tc := range cases {
		t.Run(tc.mode, func(t *testing.T) {
			stateDir := filepath.Join(dir, "state-"+tc.mode)
			env := append(os.Environ(),
				"ARCHIE_LINK_TEST_LICENSE=1",
				"ARCHIE_LINK_STATE_DIR="+stateDir,
				"ARCHIE_LINK_WG_CONFIG="+configPath,
				"ARCHIE_LINK_FAULT="+tc.mode,
			)
			t.Cleanup(func() {
				cleanup := exec.Command(binary, "down", "--json")
				cleanup.Env = env
				_ = cleanup.Run()
			})
			cmd := exec.Command(binary, "up", "--yes", "--json")
			cmd.Env = env
			var stdout, stderr bytes.Buffer
			cmd.Stdout = &stdout
			cmd.Stderr = &stderr
			err := cmd.Run()
			exitCode := 0
			if exitError, ok := err.(*exec.ExitError); ok {
				exitCode = exitError.ExitCode()
			} else if err != nil {
				t.Fatalf("run failed: %v stderr=%s", err, stderr.String())
			}
			if exitCode != tc.exitCode {
				t.Fatalf("exit=%d want=%d stdout=%s stderr=%s", exitCode, tc.exitCode, stdout.String(), stderr.String())
			}
			var payload map[string]any
			if jsonErr := json.Unmarshal(stdout.Bytes(), &payload); jsonErr != nil {
				t.Fatalf("invalid JSON: %v stdout=%s stderr=%s", jsonErr, stdout.String(), stderr.String())
			}
			if got, _ := payload["phase"].(string); got != tc.phase {
				t.Fatalf("phase=%q want=%q payload=%v", got, tc.phase, payload)
			}
			assertRestored(t, beforeDNS, beforeGateway)
		})
	}
}

func commandOutput(t *testing.T, name string, args ...string) string {
	t.Helper()
	output, err := exec.Command(name, args...).CombinedOutput()
	if err != nil {
		t.Fatalf("%s %s failed: %v\n%s", name, strings.Join(args, " "), err, output)
	}
	return strings.TrimSpace(string(output))
}

func assertRestored(t *testing.T, beforeDNS, beforeGateway string) {
	t.Helper()
	afterDNS := commandOutput(t, "networksetup", "-getdnsservers", "Wi-Fi")
	if afterDNS != beforeDNS {
		t.Fatalf("DNS was not restored: before=%q after=%q", beforeDNS, afterDNS)
	}
	afterGateway := commandOutput(t, "route", "-n", "get", "default")
	beforeGW := gatewayValue(beforeGateway)
	afterGW := gatewayValue(afterGateway)
	if beforeGW != "" && afterGW != beforeGW {
		t.Fatalf("default gateway was not restored: before=%q after=%q", beforeGW, afterGW)
	}
	if _, err := os.Stat("/etc/resolver/api.deepseek.com"); err == nil {
		t.Fatal("managed DeepSeek resolver remains after rollback")
	}
	if strings.TrimSpace(processOutput(t, "wireguard-go")) != "" {
		t.Fatal("wireguard-go remains after rollback")
	}
}

func processOutput(t *testing.T, pattern string) string {
	t.Helper()
	output, err := exec.Command("pgrep", "-fl", pattern).CombinedOutput()
	if err == nil {
		return strings.TrimSpace(string(output))
	}
	if exitError, ok := err.(*exec.ExitError); ok && exitError.ExitCode() == 1 {
		return ""
	}
	t.Fatalf("pgrep %s failed: %v\n%s", pattern, err, output)
	return ""
}

func gatewayValue(output string) string {
	for _, line := range strings.Split(output, "\n") {
		key, value, ok := strings.Cut(line, ":")
		if ok && strings.TrimSpace(key) == "gateway" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
