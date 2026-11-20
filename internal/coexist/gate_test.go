package coexist

import (
	"testing"

	"github.com/nocaptain-oss/archie-link/internal/model"
)

func TestUnknownDefaultRouteBlocksAuto(t *testing.T) {
	profile := Profile{
		SchemaVersion: "1",
		OS:            "darwin",
		Routes: []RouteRecord{{
			Destination: "default",
			Interface:   "utun9",
			Owner:       OwnerUnknown,
		}},
		DNSResolvers:     []DNSResolver{{Address: "192.168.1.1", Owner: OwnerSystem}},
		BaselineComplete: true,
	}

	decision := Evaluate(profile)
	if decision.Allowed {
		t.Fatal("unknown default route must block auto mode")
	}
	if decision.Mode != model.ModeGuided {
		t.Fatalf("mode=%s", decision.Mode)
	}
	if !hasCode(decision.Findings, "default_route_unknown_owner") {
		t.Fatalf("findings=%#v", decision.Findings)
	}
}

func TestIncompleteLayerBlocksAuto(t *testing.T) {
	profile := Profile{
		SchemaVersion:    "1",
		OS:               "darwin",
		IncompleteLayers: []Layer{LayerProxy},
	}
	decision := Evaluate(profile)
	if decision.Allowed || !hasCode(decision.Findings, "snapshot_incomplete") {
		t.Fatalf("decision=%#v", decision)
	}
}

func TestUnknownGlobalDNSAndProxyBlockAuto(t *testing.T) {
	profile := Profile{
		SchemaVersion: "1",
		OS:            "darwin",
		DNSResolvers:  []DNSResolver{{Address: "10.0.0.53", Owner: OwnerUnknown}},
		Proxies: []ProxyRecord{{
			Kind: "transparent", Enabled: true, Owner: OwnerUnknown, Complete: true,
		}},
	}
	decision := Evaluate(profile)
	if decision.Allowed {
		t.Fatal("unknown global DNS or transparent proxy must block auto mode")
	}
	if !hasCode(decision.Findings, "dns_resolver_unknown") {
		t.Fatalf("findings=%#v", decision.Findings)
	}
	if !hasCode(decision.Findings, "transparent_proxy_detected") {
		t.Fatalf("findings=%#v", decision.Findings)
	}
}

func TestIncompleteBaselineBlocksAuto(t *testing.T) {
	profile := Profile{
		SchemaVersion:    "1",
		OS:               "darwin",
		Routes:           []RouteRecord{{Destination: "default", Interface: "en0", Owner: OwnerSystem}},
		DNSResolvers:     []DNSResolver{{Address: "192.168.1.1", Owner: OwnerSystem}},
		BaselineComplete: false,
	}
	decision := Evaluate(profile)
	if decision.Allowed || !hasCode(decision.Findings, "baseline_incomplete") {
		t.Fatalf("decision=%#v", decision)
	}
}

func TestUnknownVPNBlocksAuto(t *testing.T) {
	profile := Profile{
		SchemaVersion:    "1",
		OS:               "darwin",
		Routes:           []RouteRecord{{Destination: "default", Interface: "en0", Owner: OwnerSystem}},
		DNSResolvers:     []DNSResolver{{Address: "192.168.1.1", Owner: OwnerSystem}},
		VPNs:             []VPNRecord{{Name: "mystery-tunnel", Kind: "tun", Owner: OwnerUnknown}},
		BaselineComplete: true,
	}
	decision := Evaluate(profile)
	if decision.Allowed || !hasCode(decision.Findings, "existing_vpn_conflict") {
		t.Fatalf("decision=%#v", decision)
	}
}

func TestUnknownRouteRuleBlocksAuto(t *testing.T) {
	profile := Profile{
		SchemaVersion:    "1",
		OS:               "linux",
		Routes:           []RouteRecord{{Destination: "default", Interface: "eth0", Owner: OwnerSystem}},
		RouteRules:       []RouteRule{{Priority: "100", Table: "9999", Owner: OwnerUnknown}},
		DNSResolvers:     []DNSResolver{{Address: "192.168.1.1", Owner: OwnerSystem}},
		BaselineComplete: true,
	}
	decision := Evaluate(profile)
	if decision.Allowed || !hasCode(decision.Findings, "route_rule_unknown_owner") {
		t.Fatalf("decision=%#v", decision)
	}
}

func TestKnownStateAllowsAuto(t *testing.T) {
	profile := Profile{
		SchemaVersion: "1",
		OS:            "darwin",
		Routes: []RouteRecord{{
			Destination: "default",
			Gateway:     "192.168.1.1",
			Interface:   "en0",
			Owner:       OwnerSystem,
		}},
		DNSResolvers:     []DNSResolver{{Address: "192.168.1.1", Owner: OwnerSystem}},
		BaselineComplete: true,
	}
	decision := Evaluate(profile)
	if !decision.Allowed || decision.Mode != model.ModeAuto {
		t.Fatalf("decision=%#v", decision)
	}
}

func hasCode(findings []model.Finding, code string) bool {
	for _, finding := range findings {
		if finding.Code == code {
			return true
		}
	}
	return false
}
