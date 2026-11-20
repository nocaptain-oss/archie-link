package macos

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestSnapshotParsesGatewayAndDNS(t *testing.T) {
	runner := FakeRunner{
		"route -n get default":              []byte("gateway: 192.168.1.1\ninterface: en0\n"),
		"networksetup -getdnsservers Wi-Fi": []byte("192.168.1.1\n"),
		"scutil --proxy":                    []byte("HTTPEnable : 0\nHTTPSEnable : 0\n"),
		"ifconfig":                          []byte("utun0: flags=8051<UP>\nutun3: flags=8051<UP>\n"),
		"pgrep -fl wireguard-go":            []byte("123 wireguard-go utun\n"),
	}

	network, err := New(runner).Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if network.DefaultInterface != "en0" || network.Gateway != "192.168.1.1" {
		t.Fatalf("unexpected network: %#v", network)
	}
	if len(network.DNS) != 1 || network.DNS[0] != "192.168.1.1" {
		t.Fatalf("unexpected DNS: %#v", network.DNS)
	}
	if !contains(network.ExistingTunnels, "utun0") ||
		!contains(network.ExistingTunnels, "wireguard-go") {
		t.Fatalf("unexpected tunnels: %#v", network.ExistingTunnels)
	}
}

func TestDetectReportsSupportedMacOS(t *testing.T) {
	runner := FakeRunner{
		"sw_vers -productVersion": []byte("12.7.6\n"),
		"uname -m":                []byte("x86_64\n"),
		"id -u":                   []byte("501\n"),
	}

	platform, capabilities, err := New(runner).Detect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if platform.OS != "darwin" || platform.Version != "12.7.6" || platform.Arch != "x86_64" {
		t.Fatalf("unexpected platform: %#v", platform)
	}
	if !capabilities.Shell || capabilities.Elevation || !capabilities.NetworkMutation {
		t.Fatalf("unexpected capabilities: %#v", capabilities)
	}
}

func TestDetectRecognizesRoot(t *testing.T) {
	runner := FakeRunner{
		"sw_vers -productVersion": []byte("12.7.6\n"),
		"uname -m":                []byte("arm64\n"),
		"id -u":                   []byte("0\n"),
	}

	_, capabilities, err := New(runner).Detect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !capabilities.Elevation {
		t.Fatalf("root should have elevation: %#v", capabilities)
	}
}

func TestSnapshotAllowsNoDNS(t *testing.T) {
	runner := FakeRunner{
		"route -n get default":              []byte("gateway: 192.168.1.1\ninterface: en0\n"),
		"networksetup -getdnsservers Wi-Fi": []byte("There aren't any DNS Servers set on Wi-Fi.\n"),
		"scutil --proxy":                    []byte("HTTPEnable : 0\n"),
		"ifconfig":                          []byte("en0: flags=8863<UP>\n"),
		"pgrep -fl wireguard-go":            nil,
	}

	network, err := New(runner).Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(network.DNS) != 0 {
		t.Fatalf("DNS should be empty: %#v", network.DNS)
	}
}

func TestCaptureSeparatesExplicitServiceDNSFromResolverFallback(t *testing.T) {
	runner := FakeRunner{
		"route -n get default":              []byte("gateway: 192.168.1.1\ninterface: en0\n"),
		"networksetup -getdnsservers Wi-Fi": []byte("There aren't any DNS Servers set on Wi-Fi.\n"),
		"scutil --proxy":                    []byte("HTTPEnable : 0\n"),
		"scutil --dns":                      []byte("nameserver[0] : fe80::1%4d\nnameserver[1] : 192.168.1.1\n"),
		"ifconfig":                          []byte("en0: flags=8863<UP>\n"),
		"pgrep -fl wireguard-go":            nil,
	}
	snapshot, err := NewAutomatic(runner, AutomaticOptions{}).Capture(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.DNS) != 0 {
		t.Fatalf("explicit service DNS should remain empty: %#v", snapshot.DNS)
	}
	if len(snapshot.ResolverDNS) == 0 {
		t.Fatal("resolver-chain DNS should be retained for local exceptions")
	}
}

func TestServiceForInterfaceUsesHardwareMapping(t *testing.T) {
	output := "An asterisk (*) denotes that a network service is disabled.\n" +
		"(1) Љ\n(Hardware Port: Љ, Device: usbmodem1410)\n\n" +
		"(2) Wi-Fi\n(Hardware Port: Wi-Fi, Device: en0)\n\n" +
		"(3) Thunderbolt Bridge\n(Hardware Port: Thunderbolt Bridge, Device: bridge0)\n"
	if got := serviceForInterface(output, "en0"); got != "Wi-Fi" {
		t.Fatalf("service=%q", got)
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
