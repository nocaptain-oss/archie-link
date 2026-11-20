package linux

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nocaptain-oss/archie-link/internal/model"
)

// recordingRunner 按完整命令返回预设输出并记录全部命令，供序列断言。
type recordingRunner struct {
	outputs  map[string][]byte
	failures map[string]error
	commands []string
}

func (r *recordingRunner) Run(_ context.Context, name string, args ...string) ([]byte, error) {
	command := strings.TrimSpace(strings.Join(append([]string{name}, args...), " "))
	r.commands = append(r.commands, command)
	if err, ok := r.failures[command]; ok {
		if output, hasOutput := r.outputs[command]; hasOutput {
			return output, err
		}
		return nil, err
	}
	if output, ok := r.outputs[command]; ok {
		return output, nil
	}
	return []byte(""), nil
}

func (r *recordingRunner) ran(command string) bool {
	for _, item := range r.commands {
		if item == command {
			return true
		}
	}
	return false
}

func (r *recordingRunner) ranPrefix(prefix string) bool {
	for _, item := range r.commands {
		if strings.HasPrefix(item, prefix) {
			return true
		}
	}
	return false
}

const sampleWarpConfig = `[Interface]
PrivateKey = TESTPRIVATEKEY=
Address = 172.16.0.2/32
DNS = 1.1.1.1
MTU = 1420
[Peer]
PublicKey = TESTPEERPUBLICKEY=
AllowedIPs = 0.0.0.0/0, ::/0
Endpoint = engage.cloudflareclient.com:2408
`

// testEnvironment 准备配置、状态目录与 resolv.conf 注入路径。
type testEnvironment struct {
	dir          string
	configPath   string
	resolvPath   string
	resolvBackup string
}

func newTestEnvironment(t *testing.T) testEnvironment {
	t.Helper()
	dir := t.TempDir()
	env := testEnvironment{
		dir:          dir,
		configPath:   filepath.Join(dir, "warp.conf"),
		resolvPath:   filepath.Join(dir, "resolv.conf"),
		resolvBackup: "nameserver 127.0.0.53\n",
	}
	if err := os.WriteFile(env.configPath, []byte(sampleWarpConfig), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(env.resolvPath, []byte(env.resolvBackup), 0o644); err != nil {
		t.Fatal(err)
	}
	return env
}

func (e testEnvironment) automatic(runner *recordingRunner) *Automatic {
	return NewAutomatic(runner, AutomaticOptions{
		ConfigPath:     e.configPath,
		StateDir:       filepath.Join(e.dir, "state"),
		ResolvConfPath: e.resolvPath,
		ResolveIPv4: func(context.Context, string) ([]string, error) {
			return []string{"203.0.113.10"}, nil
		},
		LookPath: func(string) (string, error) {
			return "", errors.New("not found")
		},
	})
}

func TestBuildSetConfFiltersQuickKeysAndReplacesEndpoint(t *testing.T) {
	setconf, meta, err := buildSetConf([]byte(sampleWarpConfig), model.Endpoint{Address: "162.159.192.1", Port: 2408}, 0)
	if err != nil {
		t.Fatal(err)
	}
	text := string(setconf)
	if !strings.Contains(text, "PrivateKey = TESTPRIVATEKEY=") {
		t.Fatalf("private key must survive: %s", text)
	}
	if !strings.Contains(text, "Endpoint = 162.159.192.1:2408") {
		t.Fatalf("endpoint must be replaced: %s", text)
	}
	for _, banned := range []string{"Address", "DNS", "MTU", "engage.cloudflareclient.com"} {
		if strings.Contains(text, banned) {
			t.Fatalf("wg-quick key %s must not reach setconf: %s", banned, text)
		}
	}
	if len(meta.Addresses) != 1 || meta.Addresses[0] != "172.16.0.2/32" {
		t.Fatalf("unexpected addresses: %#v", meta.Addresses)
	}
	if meta.MTU != 1420 {
		t.Fatalf("unexpected MTU: %d", meta.MTU)
	}
	if !meta.hasDefaultRoute() || !meta.hasIPv6DefaultRoute() {
		t.Fatalf("full tunnel expected: %#v", meta.AllowedIPs)
	}
}

func TestBuildSetConfRejectsHookDirectives(t *testing.T) {
	config := strings.Replace(sampleWarpConfig, "MTU = 1420", "PostUp = ip rule add pref 1", 1)
	_, _, err := buildSetConf([]byte(config), model.Endpoint{Address: "162.159.192.1", Port: 2408}, 0)
	if err == nil || !strings.Contains(err.Error(), "PostUp") {
		t.Fatalf("PostUp must be rejected: %v", err)
	}
}

func TestBuildSetConfEnsuresKeepaliveForLazyHandshake(t *testing.T) {
	setconf, _, err := buildSetConf([]byte(sampleWarpConfig), model.Endpoint{Address: "162.159.192.1", Port: 2408}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(setconf), "PersistentKeepalive = 5") {
		t.Fatalf("missing keepalive must default to 5: %s", setconf)
	}
	probeConf, _, err := buildSetConf([]byte(sampleWarpConfig), model.Endpoint{Address: "162.159.192.1", Port: 2408}, 5)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(probeConf), "PersistentKeepalive = 5") {
		t.Fatalf("probe must force keepalive 5: %s", probeConf)
	}
	existing := strings.Replace(sampleWarpConfig, "Endpoint = ", "PersistentKeepalive = 25\nEndpoint = ", 1)
	preserved, _, err := buildSetConf([]byte(existing), model.Endpoint{Address: "162.159.192.1", Port: 2408}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(preserved), "PersistentKeepalive = 25") {
		t.Fatalf("explicit keepalive must be preserved: %s", preserved)
	}
}

func TestApplyKernelFullTunnelCommandSequence(t *testing.T) {
	env := newTestEnvironment(t)
	runner := &recordingRunner{
		outputs: map[string][]byte{
			"wg show archieauto latest-handshakes": []byte("TESTPEERPUBLICKEY\t1834\n"),
		},
	}
	automatic := env.automatic(runner)
	ownership, err := automatic.Apply(context.Background(), model.ApplyPlan{
		Endpoint:          model.Endpoint{Address: "162.159.192.1", Port: 2408},
		LocalDNSDomains:   []string{"api.deepseek.com"},
		AlwaysDirectCIDRs: []string{"10.0.0.0/8"},
		ConfigPath:        env.configPath,
		Gateway:           "172.17.0.1",
		DefaultInterface:  "eth0",
	})
	if err != nil {
		t.Fatal(err)
	}
	essential := []string{
		"ip link add archieauto type wireguard",
		"wg setconf archieauto " + filepath.Join(env.dir, "state", "archieauto.setconf"),
		"ip address add 172.16.0.2/32 dev archieauto",
		"ip link set dev archieauto mtu 1420 up",
		"wg set archieauto fwmark 51820",
		"ip route add 0.0.0.0/0 dev archieauto table 51820",
		"ip rule add pref 30500 table main suppress_prefixlength 0",
		"ip rule add pref 30501 not fwmark 51820 table 51820",
		"ip route add 162.159.192.1/32 via 172.17.0.1",
		"ip route add 10.0.0.0/8 via 172.17.0.1",
		"ip route add 203.0.113.10/32 via 172.17.0.1",
	}
	for _, command := range essential {
		if !runner.ran(command) {
			t.Fatalf("missing command %q; ran: %v", command, runner.commands)
		}
	}
	if runner.ran("wireguard-go archieauto") {
		t.Fatal("kernel creation succeeded, userspace fallback must not run")
	}
	if runner.ranPrefix("resolvectl") {
		t.Fatal("resolvectl not installed, DNS must fall back to resolv.conf")
	}
	if ownership.Interface != "archieauto" {
		t.Fatalf("unexpected interface: %s", ownership.Interface)
	}
	for _, want := range []string{
		"table:51820", "rule:30500", "rule:30501",
		"route:main:162.159.192.1/32", "route:main:10.0.0.0/8", "route:main:203.0.113.10/32",
	} {
		found := false
		for _, route := range ownership.ManagedRoutes {
			if route == want {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("missing managed route %q: %#v", want, ownership.ManagedRoutes)
		}
	}
	backupFound := false
	for _, path := range ownership.ManagedFiles {
		if strings.HasSuffix(path, "resolv.conf.backup") {
			backupFound = true
		}
	}
	if !backupFound {
		t.Fatalf("resolv.conf backup must be a managed file: %#v", ownership.ManagedFiles)
	}
	managed, err := os.ReadFile(env.resolvPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(managed), "127.0.0.53") {
		t.Fatalf("resolv.conf must point at tunnel DNS: %s", managed)
	}
}

func TestApplyFallsBackToWireguardGoWhenKernelUnavailable(t *testing.T) {
	env := newTestEnvironment(t)
	runner := &recordingRunner{
		failures: map[string]error{
			"ip link add archieauto type wireguard": errors.New(
				"RTNETLINK answers: Operation not supported"),
		},
		outputs: map[string][]byte{
			"wg show archieauto":                   []byte("interface: archieauto\n"),
			"wg show archieauto latest-handshakes": []byte("TESTPEERPUBLICKEY\t1834\n"),
		},
	}
	automatic := env.automatic(runner)
	_, err := automatic.Apply(context.Background(), model.ApplyPlan{
		Endpoint:         model.Endpoint{Address: "162.159.192.1", Port: 2408},
		ConfigPath:       env.configPath,
		Gateway:          "172.17.0.1",
		DefaultInterface: "eth0",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !runner.ran("wireguard-go archieauto") {
		t.Fatalf("userspace fallback expected: %v", runner.commands)
	}
	if !runner.ran("wg show archieauto") {
		t.Fatal("must wait for userspace UAPI readiness before setconf")
	}
}

func TestApplyCleansUpOnHandshakeTimeout(t *testing.T) {
	env := newTestEnvironment(t)
	runner := &recordingRunner{
		failures: map[string]error{
			"ip link show archieauto": errors.New("Device \"archieauto\" does not exist."),
		},
	}
	automatic := env.automatic(runner)
	ctx, cancel := context.WithTimeout(context.Background(), 400*time.Millisecond)
	defer cancel()
	_, err := automatic.Apply(ctx, model.ApplyPlan{
		Endpoint:         model.Endpoint{Address: "162.159.192.1", Port: 2408},
		ConfigPath:       env.configPath,
		Gateway:          "172.17.0.1",
		DefaultInterface: "eth0",
	})
	if err == nil || !strings.Contains(err.Error(), "handshake") {
		t.Fatalf("handshake timeout expected: %v", err)
	}
	if !runner.ran("ip link del archieauto") {
		t.Fatalf("interface teardown expected: %v", runner.commands)
	}
	setconfPath := filepath.Join(env.dir, "state", "archieauto.setconf")
	if _, err := os.Stat(setconfPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("setconf file must be removed after failure: %v", err)
	}
}

func TestApplyCleansUpPolicyRulesWhenDirectHostResolutionFails(t *testing.T) {
	env := newTestEnvironment(t)
	runner := &recordingRunner{
		outputs: map[string][]byte{
			"wg show archieauto latest-handshakes": []byte("TESTPEERPUBLICKEY\t1834\n"),
		},
		failures: map[string]error{
			"ip link show archieauto": errors.New("Device \"archieauto\" does not exist."),
		},
	}
	automatic := NewAutomatic(runner, AutomaticOptions{
		ConfigPath:     env.configPath,
		StateDir:       filepath.Join(env.dir, "state"),
		ResolvConfPath: env.resolvPath,
		ResolveIPv4: func(context.Context, string) ([]string, error) {
			return nil, errors.New("i/o timeout")
		},
		LookPath: func(string) (string, error) {
			return "", errors.New("not found")
		},
	})
	_, err := automatic.Apply(context.Background(), model.ApplyPlan{
		Endpoint:         model.Endpoint{Address: "162.159.192.1", Port: 2408},
		ConfigPath:       env.configPath,
		Gateway:          "172.17.0.1",
		DefaultInterface: "eth0",
		LocalDNSDomains:  []string{"api.deepseek.com"},
	})
	if err == nil || !strings.Contains(err.Error(), "i/o timeout") {
		t.Fatalf("resolution failure expected: %v", err)
	}
	// 路由已装、后续步骤失败时，策略规则必须随失败清理一起删除，
	// 否则残留 pref 30500/30501 会被共存 Gate 当成外来状态拦截。
	if !runner.ran("ip rule del pref 30500") || !runner.ran("ip rule del pref 30501") {
		t.Fatalf("policy rule cleanup expected: %v", runner.commands)
	}
	if !runner.ran("ip link del archieauto") {
		t.Fatalf("interface teardown expected: %v", runner.commands)
	}
}

func TestApplyResolvectlModeSkipsFileManagement(t *testing.T) {
	env := newTestEnvironment(t)
	runner := &recordingRunner{
		outputs: map[string][]byte{
			"wg show archieauto latest-handshakes": []byte("TESTPEERPUBLICKEY\t1834\n"),
			"resolvectl status":                    []byte("Global\n"),
		},
	}
	automatic := NewAutomatic(runner, AutomaticOptions{
		ConfigPath:     env.configPath,
		StateDir:       filepath.Join(env.dir, "state"),
		ResolvConfPath: env.resolvPath,
		ResolveIPv4: func(context.Context, string) ([]string, error) {
			return []string{"203.0.113.10"}, nil
		},
		LookPath: func(name string) (string, error) {
			if name == "resolvectl" {
				return "/usr/bin/resolvectl", nil
			}
			return "", errors.New("not found")
		},
	})
	ownership, err := automatic.Apply(context.Background(), model.ApplyPlan{
		Endpoint:         model.Endpoint{Address: "162.159.192.1", Port: 2408},
		ConfigPath:       env.configPath,
		Gateway:          "172.17.0.1",
		DefaultInterface: "eth0",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !runner.ran("resolvectl dns archieauto 1.1.1.1") {
		t.Fatalf("resolvectl DNS expected: %v", runner.commands)
	}
	if !runner.ran("resolvectl domain archieauto ~.") {
		t.Fatalf("resolvectl domain takeover expected: %v", runner.commands)
	}
	for _, path := range ownership.ManagedFiles {
		if strings.HasSuffix(path, "resolv.conf.backup") {
			t.Fatal("resolvectl mode must not manage resolv.conf files")
		}
	}
	managed, err := os.ReadFile(env.resolvPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(managed) != env.resolvBackup {
		t.Fatalf("resolvectl mode must not touch resolv.conf: %s", managed)
	}
}

func TestRollbackRemovesRulesRoutesInterfaceAndRestoresDNS(t *testing.T) {
	env := newTestEnvironment(t)
	stateDir := filepath.Join(env.dir, "state")
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		t.Fatal(err)
	}
	backup := filepath.Join(stateDir, "resolv.conf.backup")
	if err := os.WriteFile(backup, []byte(env.resolvBackup), 0o600); err != nil {
		t.Fatal(err)
	}
	// 先把注入的 resolv.conf 置为托管态，验证回滚能还原。
	if err := os.WriteFile(env.resolvPath, []byte(managedResolverMarker+"\nnameserver 1.1.1.1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runner := &recordingRunner{
		failures: map[string]error{
			"ip link show archieauto": errors.New("Device \"archieauto\" does not exist."),
		},
		outputs: map[string][]byte{
			"ip route show default": []byte("default via 172.17.0.1 dev eth0\n"),
		},
	}
	automatic := env.automatic(runner)
	automatic.Resume(model.Ownership{
		Interface: "archieauto",
		ManagedFiles: []string{
			filepath.Join(stateDir, "archieauto.setconf"),
			backup,
		},
		ManagedRoutes: []string{
			"table:51820",
			"rule:30500",
			"rule:30501",
			"rule6:30500",
			"route:main:162.159.192.1/32",
			"route:main:10.0.0.0/8",
			"route:51820:0.0.0.0/0",
		},
	})
	err := automatic.Rollback(context.Background(), model.NetworkSnapshot{
		Gateway:          "172.17.0.1",
		DefaultInterface: "eth0",
	})
	if err != nil {
		t.Fatal(err)
	}
	essential := []string{
		"ip rule del pref 30500",
		"ip rule del pref 30501",
		"ip -6 rule del pref 30500",
		"ip route del 162.159.192.1/32",
		"ip route del 10.0.0.0/8",
		"ip link del archieauto",
		"ip route show default",
	}
	for _, command := range essential {
		if !runner.ran(command) {
			t.Fatalf("missing rollback command %q: %v", command, runner.commands)
		}
	}
	restored, err := os.ReadFile(env.resolvPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(restored) != env.resolvBackup {
		t.Fatalf("resolv.conf must be byte-restored: %q", restored)
	}
	if _, err := os.Stat(backup); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("resolv.conf backup must be consumed on restore: %v", err)
	}
}

func TestRollbackIncompleteWhenDefaultRouteDrifts(t *testing.T) {
	env := newTestEnvironment(t)
	runner := &recordingRunner{
		failures: map[string]error{
			"ip link show archieauto": fmt.Errorf("does not exist"),
		},
		outputs: map[string][]byte{
			"ip route show default": []byte("default via 10.9.9.9 dev eth9\n"),
		},
	}
	automatic := env.automatic(runner)
	automatic.Resume(model.Ownership{Interface: "archieauto"})
	err := automatic.Rollback(context.Background(), model.NetworkSnapshot{
		Gateway:          "172.17.0.1",
		DefaultInterface: "eth0",
	})
	if err == nil || !strings.Contains(err.Error(), "10.9.9.9") {
		t.Fatalf("default route drift must fail rollback: %v", err)
	}
}

func TestPickRoutingTableSkipsOccupied(t *testing.T) {
	env := newTestEnvironment(t)
	runner := &recordingRunner{
		outputs: map[string][]byte{
			"ip -4 route show table 51820": []byte("default dev wg9 table 51820\n"),
			"ip -4 route show table 51821": []byte(""),
		},
	}
	automatic := env.automatic(runner)
	table, err := automatic.pickRoutingTable(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if table != 51821 {
		t.Fatalf("occupied table must be skipped: %d", table)
	}
}

func TestApplyRejectsMissingConfig(t *testing.T) {
	env := newTestEnvironment(t)
	automatic := NewAutomatic(&recordingRunner{}, AutomaticOptions{
		ConfigPath:     "",
		StateDir:       filepath.Join(env.dir, "state"),
		ResolvConfPath: env.resolvPath,
	})
	_, err := automatic.Apply(context.Background(), model.ApplyPlan{
		Endpoint: model.Endpoint{Address: "162.159.192.1", Port: 2408},
	})
	if err == nil || !strings.Contains(err.Error(), "config") {
		t.Fatalf("missing config must fail: %v", err)
	}
}
