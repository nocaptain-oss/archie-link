package linux

import (
	"context"
	"testing"

	"github.com/nocaptain-oss/archie-link/internal/coexist"
)

func TestCoexistenceParsesRoutesAndResolved(t *testing.T) {
	runner := FakeRunner{
		"ip route show":           []byte("default via 192.168.1.1 dev eth0\n"),
		"ip route show table all": []byte("default dev wg0 table 51820\n"),
		"ip -6 route show":        []byte(""),
		"ip rule show":            []byte("32765: from all lookup 51820\n"),
		"ip link show":            []byte("2: eth0: <BROADCAST,MULTICAST,UP>\n7: wg0: <POINTOPOINT,NOARP,UP>\n"),
		"cat /etc/resolv.conf":    []byte("nameserver 127.0.0.53\n"),
		"resolvectl status":       []byte("Current DNS Server: 192.168.1.1\n"),
		"nmcli device show":       nil,
		"cat /etc/hosts":          []byte("127.0.0.1 localhost\n"),
		"ss -lnt":                 []byte("LISTEN 0 128 127.0.0.1:8688 0.0.0.0:*\n"),
		"systemctl is-active":     []byte("active\n"),
		"iptables-save":           []byte("*filter\n"),
		"nft list ruleset":        []byte("table inet filter {}\n"),
	}

	profile, err := New(runner).Coexistence(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(profile.Routes) == 0 || len(profile.DNSResolvers) == 0 {
		t.Fatalf("profile=%#v", profile)
	}
	if len(profile.RouteRules) != 1 || profile.RouteRules[0].Owner != coexist.OwnerKnownThirdParty {
		t.Fatalf("route rules=%#v", profile.RouteRules)
	}
	if profile.HostsDigest == "" {
		t.Fatal("hosts digest is empty")
	}
	if !hasLinuxPort(profile.Listeners, 8688) {
		t.Fatalf("listeners=%#v", profile.Listeners)
	}
}

func TestCoexistenceMarksFirewallIncomplete(t *testing.T) {
	runner := FakeRunner{
		"ip route show":           []byte("default via 192.168.1.1 dev eth0\n"),
		"ip route show table all": []byte(""),
		"ip -6 route show":        []byte(""),
		"ip rule show":            []byte(""),
		"ip link show":            []byte("2: eth0: <UP>\n"),
		"cat /etc/resolv.conf":    []byte("nameserver 192.168.1.1\n"),
		"resolvectl status":       nil,
		"nmcli device show":       nil,
		"cat /etc/hosts":          []byte("127.0.0.1 localhost\n"),
		"ss -lnt":                 []byte(""),
		"systemctl is-active":     []byte("active\n"),
	}

	profile, err := New(runner).Coexistence(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !hasLinuxLayer(profile, coexist.LayerFirewall) {
		t.Fatalf("incomplete=%#v", profile.IncompleteLayers)
	}
}

func hasLinuxPort(listeners []coexist.ListenerRecord, port int) bool {
	for _, listener := range listeners {
		if listener.Port == port {
			return true
		}
	}
	return false
}

func hasLinuxLayer(profile coexist.Profile, want coexist.Layer) bool {
	for _, layer := range profile.IncompleteLayers {
		if layer == want {
			return true
		}
	}
	return false
}
