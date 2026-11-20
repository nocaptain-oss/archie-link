package windows

import (
	"context"
	"testing"

	"github.com/nocaptain-oss/archie-link/internal/coexist"
)

func TestCoexistenceParsesWindowsProxyAndRoutes(t *testing.T) {
	runner := FakeRunner{
		routeCommand:      []byte("192.168.1.1|WLAN|12\n"),
		routeListCommand:  []byte("0.0.0.0/0|192.168.1.1|WLAN\n"),
		dnsCommand:        []byte("WLAN|192.168.1.1\n"),
		nrptCommand:       nil,
		wininetCommand:    []byte("ProxyEnable=1\nProxyServer=127.0.0.1:7890\nAutoConfigURL=\nProxyOverride=<local>\n"),
		winHTTPCommand:    []byte("\nDirect access (no proxy server).\n"),
		vpnCommand:        []byte(""),
		vpnProcessCommand: nil,
		listenerCommand:   []byte("127.0.0.1|8688|node.exe\n"),
		hostsCommand:      []byte("127.0.0.1 localhost\n"),
		firewallCommand:   []byte("Domain|True\nPrivate|True\n"),
		adapterCommand:    []byte("WLAN|Up\n"),
	}

	profile, err := New(runner).Coexistence(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(profile.Proxies) == 0 || profile.Proxies[0].Owner != coexist.OwnerKnownThirdParty {
		t.Fatalf("proxies=%#v", profile.Proxies)
	}
	if profile.HostsDigest == "" {
		t.Fatal("hosts digest is empty")
	}
	if len(profile.Listeners) == 0 || profile.Listeners[0].Port != 8688 {
		t.Fatalf("listeners=%#v", profile.Listeners)
	}
}

func TestCoexistenceMarksFirewallIncomplete(t *testing.T) {
	runner := FakeRunner{
		routeCommand:      []byte("192.168.1.1|WLAN|12\n"),
		routeListCommand:  []byte("0.0.0.0/0|192.168.1.1|WLAN\n"),
		dnsCommand:        []byte("WLAN|192.168.1.1\n"),
		nrptCommand:       nil,
		wininetCommand:    []byte("ProxyEnable=0\n"),
		winHTTPCommand:    []byte("Direct access\n"),
		vpnCommand:        []byte(""),
		vpnProcessCommand: nil,
		listenerCommand:   []byte(""),
		hostsCommand:      []byte("127.0.0.1 localhost\n"),
		adapterCommand:    []byte("WLAN|Up\n"),
	}

	profile, err := New(runner).Coexistence(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !hasIncomplete(profile, coexist.LayerFirewall) {
		t.Fatalf("incomplete=%#v", profile.IncompleteLayers)
	}
}

func hasIncomplete(profile coexist.Profile, want coexist.Layer) bool {
	for _, layer := range profile.IncompleteLayers {
		if layer == want {
			return true
		}
	}
	return false
}
