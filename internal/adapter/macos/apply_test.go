package macos

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nocaptain-oss/archie-link/internal/model"
)

func TestApplyWritesResolverBeforeTunnel(t *testing.T) {
	dir := t.TempDir()
	configPath := writeApplyConfig(t, dir)
	resolverDir := filepath.Join(dir, "resolver")
	runner := &ScriptedRunner{
		responses: map[string][]byte{
			"wg show interfaces":               []byte("utun12\n"),
			"wg show utun12 latest-handshakes": []byte("1 2\n"),
		},
		onRun: func(command string) error {
			if !strings.HasPrefix(command, "wg-quick up ") {
				return nil
			}
			config, err := os.ReadFile(filepath.Join(dir, "state", "archieauto.conf"))
			if err != nil {
				return err
			}
			if strings.Contains(string(config), "DNS =") {
				return errors.New("DNS must not be delegated to wg-quick")
			}
			if strings.Contains(string(config), "Table = off") {
				return errors.New("full tunnel must let wg-quick manage routes")
			}
			raw, err := os.ReadFile(filepath.Join(resolverDir, "api.deepseek.com"))
			if err != nil {
				return errors.New("resolver was not written before tunnel startup")
			}
			if !strings.Contains(string(raw), "managed by archielink") ||
				!strings.Contains(string(raw), "nameserver 192.168.1.1") {
				return errors.New("resolver content is not managed or has no original DNS")
			}
			if strings.Contains(string(raw), "%4d") {
				return errors.New("scoped link-local resolver leaked into /etc/resolver")
			}
			if !strings.Contains(string(raw), "search_order 1") ||
				!strings.Contains(string(raw), "timeout 2") {
				return errors.New("local resolver is missing bounded lookup settings")
			}
			return nil
		},
	}
	adapter := NewAutomatic(runner, AutomaticOptions{
		ConfigPath:  configPath,
		StateDir:    filepath.Join(dir, "state"),
		ResolverDir: resolverDir,
		ResolveIPv4: func(context.Context, string) ([]string, error) {
			return []string{"198.51.100.10"}, nil
		},
	})
	ownership, err := adapter.Apply(context.Background(), model.ApplyPlan{
		Endpoint:          model.Endpoint{Address: "188.114.97.1", Port: 500},
		LocalDNSDomains:   []string{"api.deepseek.com"},
		ProtectionDomains: []string{"api.skillhub.cn"},
		AlwaysDirectCIDRs: []string{
			"10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16",
		},
		ConfigPath:       configPath,
		OriginalDNS:      []string{"fe80::1%4d", "192.168.1.1"},
		Gateway:          "192.168.1.1",
		DefaultInterface: "en0",
	})
	if err != nil {
		t.Fatal(err)
	}
	if ownership.Interface != "utun12" {
		t.Fatalf("ownership=%#v", ownership)
	}
	if runner.count("wg-quick up ") != 1 {
		t.Fatalf("commands=%#v", runner.commands)
	}
	if runner.count("route -n add -inet 198.51.100.10/32 192.168.1.1") != 1 {
		t.Fatalf("protected host route missing: %#v", runner.commands)
	}
	if runner.count("route -n add -inet 188.114.97.1/32 192.168.1.1") != 1 {
		t.Fatalf("WireGuard endpoint route missing: %#v", runner.commands)
	}
	if runner.count("networksetup -setdnsservers Wi-Fi 1.1.1.1") != 1 {
		t.Fatalf("tunnel DNS was not installed: %#v", runner.commands)
	}
}

func TestApplyIsIdempotentAndKeepsProtectionRoutes(t *testing.T) {
	dir := t.TempDir()
	configPath := writeApplyConfig(t, dir)
	runner := &ScriptedRunner{
		responses: map[string][]byte{
			"wg show interfaces":               []byte("utun12\n"),
			"wg show utun12 latest-handshakes": []byte("1 2\n"),
		},
	}
	adapter := NewAutomatic(runner, AutomaticOptions{
		ConfigPath:  configPath,
		StateDir:    filepath.Join(dir, "state"),
		ResolverDir: filepath.Join(dir, "resolver"),
	})
	plan := model.ApplyPlan{
		Endpoint:          model.Endpoint{Address: "188.114.97.1", Port: 500},
		AlwaysDirectCIDRs: []string{"10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16"},
		ConfigPath:        configPath,
		Gateway:           "192.168.1.1",
		DefaultInterface:  "en0",
	}
	first, err := adapter.Apply(context.Background(), plan)
	if err != nil {
		t.Fatal(err)
	}
	second, err := adapter.Apply(context.Background(), plan)
	if err != nil {
		t.Fatal(err)
	}
	if first.Interface != second.Interface {
		t.Fatalf("first=%#v second=%#v", first, second)
	}
	if runner.count("wg-quick up ") != 1 {
		t.Fatalf("tunnel started more than once: %#v", runner.commands)
	}
	for _, cidr := range plan.AlwaysDirectCIDRs {
		if runner.count("route -n add -inet "+cidr) != 1 {
			t.Fatalf("missing protection route %s: %#v", cidr, runner.commands)
		}
	}
}

func TestApplyRefusesUnmanagedResolverCollision(t *testing.T) {
	dir := t.TempDir()
	configPath := writeApplyConfig(t, dir)
	resolverDir := filepath.Join(dir, "resolver")
	if err := os.MkdirAll(resolverDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(resolverDir, "api.deepseek.com"), []byte("nameserver 8.8.8.8\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runner := &ScriptedRunner{}
	adapter := NewAutomatic(runner, AutomaticOptions{
		ConfigPath:  configPath,
		StateDir:    filepath.Join(dir, "state"),
		ResolverDir: resolverDir,
	})
	_, err := adapter.Apply(context.Background(), model.ApplyPlan{
		Endpoint:        model.Endpoint{Address: "188.114.97.1", Port: 500},
		LocalDNSDomains: []string{"api.deepseek.com"},
		OriginalDNS:     []string{"192.168.1.1"},
		ConfigPath:      configPath,
	})
	if err == nil {
		t.Fatal("expected unmanaged resolver collision")
	}
	if runner.count("wg-quick up ") != 0 {
		t.Fatalf("tunnel started after resolver collision: %#v", runner.commands)
	}
}

func TestApplyConfigRejectsRootHooks(t *testing.T) {
	input := []byte(`[Interface]
PrivateKey = private
PreUp = /usr/bin/touch /tmp/pwned

[Peer]
PublicKey = public
Endpoint = 188.114.97.1:500
AllowedIPs = 0.0.0.0/0
`)
	if _, err := applyConfig(input, model.Endpoint{Address: "188.114.97.1", Port: 500}); err == nil {
		t.Fatal("expected PreUp rejection")
	}
}

func writeApplyConfig(t *testing.T, dir string) string {
	t.Helper()
	path := filepath.Join(dir, "source.conf")
	raw := `[Interface]
PrivateKey = private
Address = 172.16.0.2/32
DNS = 1.1.1.1
MTU = 1280

[Peer]
PublicKey = public
Endpoint = engage.cloudflareclient.com:2408
AllowedIPs = 0.0.0.0/0
`
	if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

type ScriptedRunner struct {
	commands  []string
	responses map[string][]byte
	fail      map[string]error
	onRun     func(string) error
}

func (r *ScriptedRunner) Run(_ context.Context, name string, args ...string) ([]byte, error) {
	command := strings.TrimSpace(strings.Join(append([]string{name}, args...), " "))
	r.commands = append(r.commands, command)
	if r.onRun != nil {
		if err := r.onRun(command); err != nil {
			return nil, err
		}
	}
	if err := r.fail[command]; err != nil {
		return nil, err
	}
	if output, ok := r.responses[command]; ok {
		return output, nil
	}
	return nil, nil
}

func TestApplyInstallsDNSOnActiveNetworkService(t *testing.T) {
	cases := []struct {
		name           string
		networkService string
		want           string
		rejected       string
	}{
		{"ethernet", "Ethernet", "networksetup -setdnsservers Ethernet 1.1.1.1", "networksetup -setdnsservers Wi-Fi"},
		{"empty falls back to wifi", "", "networksetup -setdnsservers Wi-Fi 1.1.1.1", "networksetup -setdnsservers Ethernet"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			dir := t.TempDir()
			configPath := writeApplyConfig(t, dir)
			runner := &ScriptedRunner{responses: map[string][]byte{
				"wg show interfaces":               []byte("utun12\n"),
				"wg show utun12 latest-handshakes": []byte("1 2\n"),
			}}
			adapter := NewAutomatic(runner, AutomaticOptions{
				ConfigPath:  configPath,
				StateDir:    filepath.Join(dir, "state"),
				ResolverDir: filepath.Join(dir, "resolver"),
			})
			if _, err := adapter.Apply(context.Background(), model.ApplyPlan{
				Endpoint:         model.Endpoint{Address: "188.114.97.1", Port: 500},
				ConfigPath:       configPath,
				Gateway:          "192.168.1.1",
				DefaultInterface: "en0",
				NetworkService:   testCase.networkService,
			}); err != nil {
				t.Fatalf("Apply failed: %v", err)
			}
			if runner.count(testCase.want) != 1 {
				t.Fatalf("expected %q once: %#v", testCase.want, runner.commands)
			}
			if runner.count(testCase.rejected) != 0 {
				t.Fatalf("DNS was also written to the wrong service: %#v", runner.commands)
			}
		})
	}
}

func (r *ScriptedRunner) count(prefix string) int {
	count := 0
	for _, command := range r.commands {
		if strings.HasPrefix(command, prefix) {
			count++
		}
	}
	return count
}
