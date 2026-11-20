package windows

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestParsesWindowsRouteAndDNS(t *testing.T) {
	runner := FakeRunner{
		routeCommand: []byte("192.168.1.1|Wi-Fi|12\n"),
		dnsCommand:   []byte("Wi-Fi|192.168.1.1,1.1.1.1\n"),
		proxyCommand: []byte(
			"0\n",
		),
		adapterCommand: []byte("Wi-Fi|Up\n"),
		processCommand: []byte("wireguard-go.exe\n"),
	}

	network, err := New(runner).Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if network.Gateway != "192.168.1.1" || network.DefaultInterface != "Wi-Fi" {
		t.Fatalf("unexpected route: %#v", network)
	}
	if len(network.DNS) != 2 {
		t.Fatalf("unexpected DNS: %#v", network.DNS)
	}
	if !contains(network.ExistingTunnels, "wireguard-go.exe") {
		t.Fatalf("unexpected tunnels: %#v", network.ExistingTunnels)
	}
}

func TestProcessFilterIgnoresArchielinkAndDeduplicates(t *testing.T) {
	if !strings.Contains(processScript, "^(wireguard|wireguard-go|warp-svc|openvpn)") {
		t.Fatalf("process filter is too broad: %s", processScript)
	}
	got := parseLines("wireguard-go.exe\nwireguard-go.exe\n")
	if len(got) != 1 || got[0] != "wireguard-go.exe" {
		t.Fatalf("unexpected deduplicated processes: %#v", got)
	}
}

type FakeRunner map[string][]byte

func (f FakeRunner) Run(_ context.Context, name string, args ...string) ([]byte, error) {
	key := strings.TrimSpace(strings.Join(append([]string{name}, args...), " "))
	if output, ok := f[key]; ok {
		return output, nil
	}
	return nil, errors.New("unexpected command: " + key)
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
