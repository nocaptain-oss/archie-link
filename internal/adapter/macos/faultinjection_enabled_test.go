//go:build faultinjection

package macos

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInjectedProbePlanOverridesConfigEndpoint(t *testing.T) {
	t.Setenv(faultModeEnv, "unavailable_endpoint")
	dir := t.TempDir()
	configPath := filepath.Join(dir, "source.conf")
	raw := `[Interface]
PrivateKey = private
DNS = 1.1.1.1

[Peer]
PublicKey = public
Endpoint = 198.51.100.20:2408
AllowedIPs = 0.0.0.0/0
`
	if err := os.WriteFile(configPath, []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	stateDir := filepath.Join(dir, "state")
	runner := &ScriptedRunner{
		onRun: func(command string) error {
			if !strings.HasPrefix(command, "wg-quick up ") {
				return nil
			}
			probeConfig, err := os.ReadFile(filepath.Join(stateDir, probeInterface+".conf"))
			if err != nil {
				t.Errorf("read probe config: %v", err)
				return nil
			}
			if strings.Contains(string(probeConfig), "198.51.100.20") {
				t.Errorf("config endpoint leaked into injected probe: %s", probeConfig)
			}
			if !strings.Contains(string(probeConfig), "Endpoint = 203.0.113.1:500") {
				t.Errorf("injected endpoint missing from probe: %s", probeConfig)
			}
			return nil
		},
	}
	adapter := NewAutomatic(runner, AutomaticOptions{
		ConfigPath:  configPath,
		StateDir:    stateDir,
		ResolverDir: filepath.Join(dir, "resolver"),
	})
	if _, err := adapter.Probe(context.Background(), DefaultProbePlan()); err == nil {
		t.Fatal("expected unavailable endpoint probe to fail")
	}
}