package adapter

import (
	"context"
	"errors"
	"testing"

	"github.com/nocaptain-oss/archie-link/internal/model"
)

func TestSelectMode(t *testing.T) {
	tests := []struct {
		name string
		caps model.CapabilityReport
		want model.Mode
	}{
		{"auto", model.CapabilityReport{Shell: true, Elevation: true, NetworkMutation: true}, model.ModeAuto},
		{"guided", model.CapabilityReport{Shell: true}, model.ModeGuided},
		{"unsupported", model.CapabilityReport{}, model.ModeUnsupported},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := SelectMode(tc.caps); got != tc.want {
				t.Fatalf("got %s want %s", got, tc.want)
			}
		})
	}
}

func TestRegistryRejectsUnknownPlatform(t *testing.T) {
	registry := NewRegistry()
	if _, err := registry.For("plan9"); !errors.Is(err, ErrUnsupportedPlatform) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestGuardRunnerBlocksNetworkWrites(t *testing.T) {
	runner := GuardRunner{Next: FakeRunner{}}
	for _, command := range []string{
		"route add default 1.1.1.1",
		"networksetup -setdnsservers Wi-Fi 1.1.1.1",
		"wg-quick up warp",
		"iptables -A OUTPUT",
		"nft add rule inet filter output",
		"systemctl restart NetworkManager",
	} {
		if _, err := runner.Run(context.Background(), "sh", "-c", command); !errors.Is(err, ErrWriteCommandBlocked) {
			t.Fatalf("command %q was not blocked: %v", command, err)
		}
	}
}

func TestGuardRunnerAllowsReadOnlyWinHTTPProxyQuery(t *testing.T) {
	runner := GuardRunner{Next: FakeRunner{}}
	if _, err := runner.Run(context.Background(), "netsh", "winhttp", "show", "proxy"); err != nil {
		t.Fatalf("read-only WinHTTP query blocked: %v", err)
	}
}

func TestGuardRunnerAllowsReadOnlyFirewallSnapshots(t *testing.T) {
	runner := GuardRunner{Next: FakeRunner{}}
	for _, command := range []struct {
		name string
		args []string
	}{
		{"iptables-save", nil},
		{"nft", []string{"list", "ruleset"}},
	} {
		if _, err := runner.Run(context.Background(), command.name, command.args...); err != nil {
			t.Fatalf("read-only firewall snapshot blocked: %s %v: %v", command.name, command.args, err)
		}
	}
}

type FakeRunner struct{}

func (FakeRunner) Run(context.Context, string, ...string) ([]byte, error) {
	return []byte("ok"), nil
}
