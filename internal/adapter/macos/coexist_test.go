package macos

import (
	"context"
	"testing"

	"github.com/nocaptain-oss/archie-link/internal/coexist"
)

func TestCoexistenceParsesSystemProxyAndDNS(t *testing.T) {
	runner := FakeRunner{
		"route -n get default":                      []byte("gateway: 192.168.1.1\ninterface: en0\n"),
		"netstat -nr -f inet":                       []byte("default 192.168.1.1 UGScg en0\ndefault fe80::1 UGScg utun3\n"),
		"netstat -nr -f inet6":                      nil,
		"scutil --proxy":                            []byte("HTTPEnable : 1\nHTTPProxy : 127.0.0.1\nHTTPPort : 7890\n"),
		"scutil --dns":                              []byte("nameserver[0] : 192.168.1.1\n"),
		"networksetup -listallnetworkservices":      []byte("An asterisk (*) denotes that a network service is disabled.\nWi-Fi\n"),
		"networksetup -getautoproxyurl Wi-Fi":       []byte("URL: (null)\nEnabled: No\n"),
		"networksetup -getproxybypassdomains Wi-Fi": []byte("*.local\n192.168.0.0/16\n"),
		"ifconfig":                    []byte("en0: flags=8863<UP>\nutun3: flags=8051<UP>\n"),
		"lsof -nP -iTCP -sTCP:LISTEN": []byte("node 123 user 20u IPv4 TCP 127.0.0.1:8688 (LISTEN)\n"),
		"cat /etc/hosts":              []byte("127.0.0.1 localhost\n"),
		"pgrep -fl wireguard-go":      nil,
		"pgrep -fl ShadowsocksX":      nil,
		"pgrep -fl Clash":             nil,
		"pgrep -fl Surge":             nil,
		"pfctl -s info":               []byte("Status: Disabled\n"),
	}

	profile, err := New(runner).Coexistence(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(profile.Proxies) != 1 || profile.Proxies[0].Owner != coexist.OwnerKnownThirdParty {
		t.Fatalf("proxies=%#v", profile.Proxies)
	}
	if len(profile.DNSResolvers) == 0 {
		t.Fatalf("dns=%#v", profile.DNSResolvers)
	}
	if profile.HostsDigest == "" {
		t.Fatal("hosts digest is empty")
	}
	if !hasListener(profile.Listeners, 8688) {
		t.Fatalf("listeners=%#v", profile.Listeners)
	}
	if !hasUnknownRoute(profile.Routes) {
		t.Fatalf("routes=%#v", profile.Routes)
	}
}

func TestCoexistenceMarksProxyIncompleteWhenPACReadFails(t *testing.T) {
	runner := FakeRunner{
		"route -n get default":                 []byte("gateway: 192.168.1.1\ninterface: en0\n"),
		"netstat -nr -f inet":                  []byte("default 192.168.1.1 UGScg en0\n"),
		"netstat -nr -f inet6":                 nil,
		"scutil --proxy":                       []byte("HTTPEnable : 0\n"),
		"scutil --dns":                         []byte("nameserver[0] : 192.168.1.1\n"),
		"networksetup -listallnetworkservices": []byte("Wi-Fi\n"),
		"ifconfig":                             []byte("en0: flags=8863<UP>\n"),
		"lsof -nP -iTCP -sTCP:LISTEN":          []byte(""),
		"cat /etc/hosts":                       []byte("127.0.0.1 localhost\n"),
		"pfctl -s info":                        []byte("Status: Disabled\n"),
	}

	profile, err := New(runner).Coexistence(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !hasIncompleteLayer(profile, coexist.LayerProxy) {
		t.Fatalf("incomplete=%#v", profile.IncompleteLayers)
	}
}

func hasListener(listeners []coexist.ListenerRecord, port int) bool {
	for _, listener := range listeners {
		if listener.Port == port {
			return true
		}
	}
	return false
}

func hasUnknownRoute(routes []coexist.RouteRecord) bool {
	for _, route := range routes {
		if route.Destination == "default" && route.Owner == coexist.OwnerUnknown {
			return true
		}
	}
	return false
}

func hasIncompleteLayer(profile coexist.Profile, want coexist.Layer) bool {
	for _, layer := range profile.IncompleteLayers {
		if layer == want {
			return true
		}
	}
	return false
}
