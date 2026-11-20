package linux

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestParsesIpRouteAndResolvConf(t *testing.T) {
	runner := FakeRunner{
		"ip route show default": []byte("default via 192.168.1.1 dev eth0 proto dhcp\n"),
		"cat /etc/resolv.conf":  []byte("nameserver 192.168.1.1\nnameserver 1.1.1.1\n"),
		"resolvectl status":     nil,
		"nmcli device show":     nil,
		"ip link show":          []byte("2: eth0: <BROADCAST,MULTICAST,UP>\n7: wg0: <POINTOPOINT,NOARP,UP>\n"),
	}

	network, err := New(runner).Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if network.DefaultInterface != "eth0" || network.Gateway != "192.168.1.1" {
		t.Fatalf("unexpected network: %#v", network)
	}
	if len(network.DNS) != 2 {
		t.Fatalf("unexpected DNS: %#v", network.DNS)
	}
	if !contains(network.ExistingTunnels, "wg0") {
		t.Fatalf("unexpected tunnels: %#v", network.ExistingTunnels)
	}
}

func TestDetectUsesLookPathNotCommandBuiltin(t *testing.T) {
	runner := FakeRunner{
		"cat /etc/os-release": []byte(`NAME="Ubuntu"
VERSION_ID="22.04"
`),
		"uname -m": []byte("x86_64\n"),
		"id -u":    []byte("0\n"),
	}
	// 标准 Ubuntu/Debian 没有 command 二进制；探测必须走 LookPath 而非 runner。
	// 模拟 root + ip + wg 全在场（Ubuntu 22.04 容器实测场景）。
	present := map[string]bool{"ip": true, "wg": true}
	adapter := New(runner)
	adapter.lookPath = func(name string) (string, error) {
		if present[name] {
			return "/usr/sbin/" + name, nil
		}
		return "", errors.New("not found")
	}
	platform, capabilities, err := adapter.Detect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if platform.OS != "linux" || platform.Version != "22.04" || platform.Arch != "x86_64" {
		t.Fatalf("unexpected platform: %#v", platform)
	}
	if !capabilities.Elevation || !capabilities.NetworkMutation {
		t.Fatalf("root+ip+wg must report elevation and network_mutation: %#v", capabilities)
	}
}

func TestDetectGuidedWhenWgAbsent(t *testing.T) {
	runner := FakeRunner{
		"cat /etc/os-release": []byte(`NAME="Ubuntu"
VERSION_ID="22.04"
`),
		"uname -m": []byte("x86_64\n"),
		"id -u":    []byte("1000\n"),
	}
	present := map[string]bool{"sudo": true, "ip": true}
	adapter := New(runner)
	adapter.lookPath = func(name string) (string, error) {
		if present[name] {
			return "/usr/bin/" + name, nil
		}
		return "", errors.New("not found")
	}
	_, capabilities, err := adapter.Detect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !capabilities.Elevation {
		t.Fatalf("sudo presence must report elevation: %#v", capabilities)
	}
	if capabilities.NetworkMutation {
		t.Fatalf("no wg/wireguard-go must not report network_mutation: %#v", capabilities)
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
