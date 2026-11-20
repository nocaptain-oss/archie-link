package doctor

import (
	"strconv"
	"strings"

	"github.com/nocaptain-oss/archie-link/internal/model"
)

func Analyze(report model.DoctorReport) []model.Finding {
	var findings []model.Finding

	if report.Mode == model.ModeUnsupported {
		findings = append(findings, model.Finding{
			Code:    "unsupported_os",
			Level:   "error",
			Message: "the current host cannot run network diagnostics",
		})
	}
	if report.Mode == model.ModeGuided && report.Capabilities.Shell {
		findings = append(findings, model.Finding{
			Code:    "permission_required",
			Level:   "warning",
			Message: "shell is available but network changes require a guided workflow",
		})
	}
	if report.Network.DefaultInterface == "" || report.Network.Gateway == "" {
		findings = append(findings, model.Finding{
			Code:    "no_default_route",
			Level:   "error",
			Message: "no default IPv4 route or gateway was detected",
		})
	}
	if len(report.Network.DNS) == 0 {
		findings = append(findings, model.Finding{
			Code:    "dns_missing",
			Level:   "warning",
			Message: "no explicit DNS servers were detected",
		})
	}
	if report.Network.ProxyEnabled {
		findings = append(findings, model.Finding{
			Code:    "proxy_enabled",
			Level:   "warning",
			Message: "a system proxy is enabled and may change routing behavior",
		})
	}

	tunnels := meaningfulTunnels(report.Network.ExistingTunnels)
	switch len(tunnels) {
	case 0:
	case 1:
		findings = append(findings, model.Finding{
			Code:    "existing_tunnel",
			Level:   "info",
			Message: "an existing tunnel was detected: " + tunnels[0],
		})
	default:
		findings = append(findings, model.Finding{
			Code:    "multiple_tunnels",
			Level:   "warning",
			Message: "multiple tunnels were detected; starting another tunnel may conflict",
		})
	}

	return dedupe(findings)
}

func meaningfulTunnels(tunnels []string) []string {
	result := make([]string, 0, len(tunnels))
	for _, tunnel := range tunnels {
		if isBareUTUN(tunnel) {
			continue
		}
		result = append(result, tunnel)
	}
	return result
}

func isBareUTUN(name string) bool {
	suffix, ok := strings.CutPrefix(name, "utun")
	if !ok || suffix == "" {
		return false
	}
	_, err := strconv.Atoi(suffix)
	return err == nil
}

func dedupe(findings []model.Finding) []model.Finding {
	seen := make(map[string]struct{}, len(findings))
	result := make([]model.Finding, 0, len(findings))
	for _, finding := range findings {
		if _, ok := seen[finding.Code]; ok {
			continue
		}
		seen[finding.Code] = struct{}{}
		result = append(result, finding)
	}
	return result
}
