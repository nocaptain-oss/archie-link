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

func TestRollbackOrderAndIdempotency(t *testing.T) {
	dir := t.TempDir()
	resolverDir := filepath.Join(dir, "resolver")
	ownedConfig := filepath.Join(dir, "state", "archieauto.conf")
	resolverPath := filepath.Join(resolverDir, "api.deepseek.com")
	if err := os.MkdirAll(filepath.Dir(ownedConfig), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ownedConfig, []byte("[Interface]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(resolverDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(resolverPath, []byte(managedResolverMarker+"\nnameserver 192.168.1.1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runner := &ScriptedRunner{responses: map[string][]byte{
		"route -n get default": []byte("gateway: 192.168.1.1\ninterface: en0\n"),
	}}
	adapter := NewAutomatic(runner, AutomaticOptions{
		StateDir:        filepath.Join(dir, "state"),
		ResolverDir:     resolverDir,
		WireguardRunDir: filepath.Join(dir, "wireguard"),
	})
	adapter.ownership = &model.Ownership{
		Interface:    "utun12",
		ManagedFiles: []string{ownedConfig, resolverPath},
		ManagedRoutes: []string{
			"0.0.0.0/1", "128.0.0.0/1", "10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16",
		},
	}
	snapshot := model.NetworkSnapshot{
		DefaultInterface: "en0",
		NetworkService:   "Wi-Fi",
		Gateway:          "192.168.1.1",
		DNS:              []string{"fe80::1%4d", "192.168.1.1"},
	}
	if err := adapter.Rollback(context.Background(), snapshot); err != nil {
		t.Fatal(err)
	}
	assertOrder(t, runner.commands,
		"wg-quick down "+ownedConfig,
		"networksetup -setdnsservers Wi-Fi 192.168.1.1",
		"route -n delete -inet 0.0.0.0/1",
		"scutil --proxy",
		"route -n get default",
	)
	if _, err := os.Stat(ownedConfig); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("owned config remains: %v", err)
	}
	if _, err := os.Stat(resolverPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("managed resolver remains: %v", err)
	}
	firstCount := len(runner.commands)
	if err := adapter.Rollback(context.Background(), snapshot); err != nil {
		t.Fatal(err)
	}
	if len(runner.commands) != firstCount {
		t.Fatalf("second rollback was not idempotent: %#v", runner.commands)
	}
}

func TestRollbackContinuesAfterSingleFailure(t *testing.T) {
	dir := t.TempDir()
	runner := &ScriptedRunner{
		responses: map[string][]byte{
			"route -n get default": []byte("gateway: 192.168.1.1\ninterface: en0\n"),
		},
		fail: map[string]error{
			"networksetup -setdnsservers Wi-Fi 192.168.1.1": errors.New("dns locked"),
		},
	}
	adapter := NewAutomatic(runner, AutomaticOptions{
		StateDir:        dir,
		WireguardRunDir: filepath.Join(dir, "wireguard"),
	})
	adapter.ownership = &model.Ownership{
		Interface:     "utun12",
		ManagedRoutes: []string{"0.0.0.0/1"},
	}
	err := adapter.Rollback(context.Background(), model.NetworkSnapshot{
		DefaultInterface: "en0",
		NetworkService:   "Wi-Fi",
		Gateway:          "192.168.1.1",
		DNS:              []string{"192.168.1.1"},
	})
	if !errors.Is(err, ErrRollbackIncomplete) {
		t.Fatalf("expected rollback incomplete, got %v", err)
	}
	if !containsCommand(runner.commands, "route -n get default") {
		t.Fatalf("rollback stopped before final verification: %#v", runner.commands)
	}
}

func TestRollbackFailsOnUnexpectedDownError(t *testing.T) {
	dir := t.TempDir()
	stateDir := filepath.Join(dir, "state")
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		t.Fatal(err)
	}
	ownedConfig := filepath.Join(stateDir, ownedConfigName+".conf")
	if err := os.WriteFile(ownedConfig, []byte("[Interface]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runner := &ScriptedRunner{
		responses: map[string][]byte{
			"route -n get default": []byte("gateway: 192.168.1.1\ninterface: en0\n"),
		},
		fail: map[string]error{
			"wg-quick down " + ownedConfig: errors.New("permission denied"),
		},
	}
	adapter := NewAutomatic(runner, AutomaticOptions{
		StateDir:        stateDir,
		ResolverDir:     filepath.Join(dir, "resolver"),
		WireguardRunDir: filepath.Join(dir, "wireguard"),
	})
	adapter.ownership = &model.Ownership{
		Interface:    "utun12",
		ManagedFiles: []string{ownedConfig},
	}
	err := adapter.Rollback(context.Background(), model.NetworkSnapshot{
		DefaultInterface: "en0",
		NetworkService:   "Wi-Fi",
		Gateway:          "192.168.1.1",
	})
	if !errors.Is(err, ErrRollbackIncomplete) {
		t.Fatalf("unexpected down error must fail rollback, got %v", err)
	}
	if !containsCommand(runner.commands, "wg-quick down "+ownedConfig) {
		t.Fatalf("wg-quick down was not attempted: %#v", runner.commands)
	}
}

func TestRollbackCleansStaleInterfaceWhenDownFails(t *testing.T) {
	dir := t.TempDir()
	runDir := filepath.Join(dir, "wireguard")
	if err := os.MkdirAll(runDir, 0o700); err != nil {
		t.Fatal(err)
	}
	namePath := filepath.Join(runDir, ownedConfigName+".name")
	if err := os.WriteFile(namePath, []byte("utun12\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	sockPath := filepath.Join(runDir, "utun12.sock")
	if err := os.WriteFile(sockPath, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	stateDir := filepath.Join(dir, "state")
	ownedConfig := filepath.Join(stateDir, ownedConfigName+".conf")
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ownedConfig, []byte("[Interface]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runner := &ScriptedRunner{
		responses: map[string][]byte{
			"route -n get default": []byte("gateway: 192.168.1.1\ninterface: en0\n"),
		},
		fail: map[string]error{
			"wg-quick down " + ownedConfig: errors.New("wg-quick: `archieauto' is not a WireGuard interface"),
		},
	}
	adapter := NewAutomatic(runner, AutomaticOptions{
		StateDir:        stateDir,
		ResolverDir:     filepath.Join(dir, "resolver"),
		WireguardRunDir: runDir,
	})
	adapter.ownership = &model.Ownership{
		Interface:    "utun12",
		ManagedFiles: []string{ownedConfig},
	}
	err := adapter.Rollback(context.Background(), model.NetworkSnapshot{
		DefaultInterface: "en0",
		NetworkService:   "Wi-Fi",
		Gateway:          "192.168.1.1",
	})
	if err != nil {
		t.Fatalf("stale interface rollback should succeed: %v", err)
	}
	if !containsCommand(runner.commands, "ifconfig utun12 destroy") {
		t.Fatalf("stale interface was not destroyed: %#v", runner.commands)
	}
	for _, path := range []string{namePath, sockPath} {
		if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("stale WireGuard artifact remains: %s", path)
		}
	}
}

func assertOrder(t *testing.T, commands []string, wants ...string) {
	t.Helper()
	last := -1
	for _, want := range wants {
		index := indexOf(commands, want)
		if index < 0 {
			t.Fatalf("missing command %q: %#v", want, commands)
		}
		if index <= last {
			t.Fatalf("command %q out of order: %#v", want, commands)
		}
		last = index
	}
}

func indexOf(commands []string, want string) int {
	for i, command := range commands {
		if command == want {
			return i
		}
	}
	return -1
}

func containsCommand(commands []string, want string) bool {
	for _, command := range commands {
		if strings.HasPrefix(command, want) {
			return true
		}
	}
	return false
}
