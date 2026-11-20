package coexist

import "github.com/nocaptain-oss/archie-link/internal/model"

func Evaluate(profile Profile) GateDecision {
	if profile.SchemaVersion != "1" || profile.OS == "" {
		return GateDecision{
			SchemaVersion: model.SchemaVersion,
			Mode:          model.ModeUnsupported,
			Findings:      []model.Finding{{Code: "unsupported_os", Level: "error", Message: "coexistence profile is incomplete"}},
		}
	}

	allowed := true
	findings := []model.Finding{}
	if len(profile.IncompleteLayers) > 0 {
		allowed = false
		findings = append(findings, model.Finding{
			Code:    "snapshot_incomplete",
			Level:   "error",
			Message: "one or more network layers could not be inspected",
		})
	}
	if !profile.BaselineComplete {
		allowed = false
		findings = append(findings, model.Finding{
			Code:    "baseline_incomplete",
			Level:   "error",
			Message: "protected endpoint baseline has not completed",
		})
	}
	if len(profile.DNSResolvers) == 0 {
		allowed = false
		findings = append(findings, model.Finding{
			Code:    "dns_missing",
			Level:   "error",
			Message: "no DNS resolvers were discovered",
		})
	}

	for _, route := range profile.Routes {
		if route.Destination == "default" && route.Owner == OwnerUnknown {
			allowed = false
			findings = append(findings, model.Finding{
				Code:    "default_route_unknown_owner",
				Level:   "error",
				Message: "the default route is not owned by a recognized network component",
			})
		}
	}
	for _, rule := range profile.RouteRules {
		if rule.Owner == OwnerUnknown {
			allowed = false
			findings = append(findings, model.Finding{
				Code:    "route_rule_unknown_owner",
				Level:   "error",
				Message: "a policy route references an unrecognized table",
			})
		}
	}
	for _, resolver := range profile.DNSResolvers {
		if resolver.Owner == OwnerUnknown {
			allowed = false
			findings = append(findings, model.Finding{
				Code:    "dns_resolver_unknown",
				Level:   "error",
				Message: "a DNS resolver is not owned by a recognized network component",
			})
		}
	}
	for _, proxy := range profile.Proxies {
		if !proxy.Complete {
			allowed = false
			findings = append(findings, model.Finding{
				Code:    "snapshot_incomplete",
				Level:   "error",
				Message: "proxy state is incomplete",
			})
		}
		if proxy.Enabled && proxy.Owner == OwnerUnknown {
			allowed = false
			findings = append(findings, model.Finding{
				Code:    "transparent_proxy_detected",
				Level:   "error",
				Message: "an enabled proxy has no recognized owner",
			})
		}
	}
	for _, vpn := range profile.VPNs {
		if vpn.Owner == OwnerUnknown {
			allowed = false
			findings = append(findings, model.Finding{
				Code:    "existing_vpn_conflict",
				Level:   "error",
				Message: "an existing VPN or tunnel has no recognized owner",
			})
		}
	}

	if !allowed {
		return GateDecision{
			SchemaVersion:    model.SchemaVersion,
			Mode:             model.ModeGuided,
			Allowed:          false,
			Findings:         dedupe(findings),
			IncompleteLayers: profile.IncompleteLayers,
		}
	}
	return GateDecision{
		SchemaVersion:    model.SchemaVersion,
		Mode:             model.ModeAuto,
		Allowed:          true,
		Findings:         dedupe(findings),
		IncompleteLayers: []Layer{},
	}
}

func dedupe(findings []model.Finding) []model.Finding {
	seen := map[string]struct{}{}
	result := []model.Finding{}
	for _, finding := range findings {
		if _, ok := seen[finding.Code]; ok {
			continue
		}
		seen[finding.Code] = struct{}{}
		result = append(result, finding)
	}
	return result
}
