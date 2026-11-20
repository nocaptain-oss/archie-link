package doctor

import (
	"testing"

	"github.com/nocaptain-oss/archie-link/internal/model"
)

func TestClassifiesProxyAndTunnelConflict(t *testing.T) {
	report := model.DoctorReport{
		Mode: model.ModeAuto,
		Network: model.NetworkReport{
			DefaultInterface: "en0",
			Gateway:          "192.168.1.1",
			DNS:              []string{"192.168.1.1"},
			ProxyEnabled:     true,
			ExistingTunnels:  []string{"ShadowsocksX-NG", "wireguard-go"},
		},
	}

	findings := Analyze(report)
	if !hasFinding(findings, "multiple_tunnels") {
		t.Fatalf("findings=%#v", findings)
	}
	if !hasFinding(findings, "proxy_enabled") {
		t.Fatalf("findings=%#v", findings)
	}
}

func TestClassifiesMissingRouteAndDNS(t *testing.T) {
	report := model.DoctorReport{
		Mode: model.ModeGuided,
		Capabilities: model.CapabilityReport{
			Shell: true,
		},
	}

	findings := Analyze(report)
	for _, code := range []string{"no_default_route", "dns_missing", "permission_required"} {
		if !hasFinding(findings, code) {
			t.Fatalf("missing %s: %#v", code, findings)
		}
	}
}

func TestIgnoresBareSystemUTUNInterfaces(t *testing.T) {
	report := model.DoctorReport{
		Mode: model.ModeAuto,
		Network: model.NetworkReport{
			DefaultInterface: "en0",
			Gateway:          "192.168.1.1",
			DNS:              []string{"192.168.1.1"},
			ExistingTunnels:  []string{"utun0", "utun1", "utun2"},
		},
	}

	findings := Analyze(report)
	if hasFinding(findings, "multiple_tunnels") || hasFinding(findings, "existing_tunnel") {
		t.Fatalf("system utun interfaces should not be classified as VPN: %#v", findings)
	}
}

func hasFinding(findings []model.Finding, code string) bool {
	for _, finding := range findings {
		if finding.Code == code {
			return true
		}
	}
	return false
}
