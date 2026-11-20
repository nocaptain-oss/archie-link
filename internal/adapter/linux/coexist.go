package linux

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"

	"github.com/nocaptain-oss/archie-link/internal/coexist"
	"github.com/nocaptain-oss/archie-link/internal/model"
)

var ssListenPattern = regexp.MustCompile(`^LISTEN\s+\d+\s+\d+\s+(\S+)\s+`)

func (a *Adapter) Coexistence(ctx context.Context) (coexist.Profile, error) {
	profile := coexist.Profile{
		SchemaVersion:    model.SchemaVersion,
		OS:               "linux",
		Routes:           []coexist.RouteRecord{},
		RouteRules:       []coexist.RouteRule{},
		DNSResolvers:     []coexist.DNSResolver{},
		Proxies:          []coexist.ProxyRecord{},
		Listeners:        []coexist.ListenerRecord{},
		VPNs:             []coexist.VPNRecord{},
		Protected:        []coexist.ProtectedEndpoint{},
		IncompleteLayers: []coexist.Layer{},
	}

	profile.VPNs = a.discoverLinuxVPNs(ctx, &profile)
	profile.Routes = a.discoverLinuxRoutes(ctx, &profile)
	a.discoverLinuxDNS(ctx, &profile)
	profile.Proxies = linuxEnvironmentProxies()
	a.discoverLinuxListeners(ctx, &profile)
	a.digestLinuxHosts(ctx, &profile)
	a.discoverLinuxFirewall(ctx, &profile)
	_, _ = a.runTextOptional(ctx, "systemctl", "is-active")
	_, _ = a.runTextOptional(ctx, "nmcli", "device", "show")
	return profile, nil
}

func (a *Adapter) discoverLinuxVPNs(ctx context.Context, profile *coexist.Profile) []coexist.VPNRecord {
	output, err := a.runner.Run(ctx, "ip", "link", "show")
	if err != nil {
		addLinuxIncomplete(profile, coexist.LayerVPN)
		return []coexist.VPNRecord{}
	}
	vpns := []coexist.VPNRecord{}
	for _, line := range strings.Split(string(output), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		name := strings.TrimSuffix(fields[1], ":")
		switch {
		case strings.HasPrefix(name, "wg"):
			vpns = append(vpns, coexist.VPNRecord{Name: name, Kind: "wireguard", Owner: coexist.OwnerKnownThirdParty})
		case strings.HasPrefix(name, "tun"), strings.HasPrefix(name, "tap"):
			vpns = append(vpns, coexist.VPNRecord{Name: name, Kind: "tunnel", Owner: coexist.OwnerUnknown})
		}
	}
	return vpns
}

func (a *Adapter) discoverLinuxRoutes(ctx context.Context, profile *coexist.Profile) []coexist.RouteRecord {
	routes := []coexist.RouteRecord{}
	for _, args := range [][]string{
		{"route", "show"},
		{"route", "show", "table", "all"},
		{"-6", "route", "show"},
	} {
		output, err := a.runner.Run(ctx, "ip", args...)
		if err != nil {
			addLinuxIncomplete(profile, coexist.LayerRoute)
			continue
		}
		routes = append(routes, parseLinuxRoutes(string(output))...)
	}
	rulesOutput, ruleErr := a.runner.Run(ctx, "ip", "rule", "show")
	if ruleErr != nil {
		addLinuxIncomplete(profile, coexist.LayerRoute)
	} else {
		profile.RouteRules = parseLinuxRouteRules(string(rulesOutput), routes)
	}
	return routes
}

func (a *Adapter) discoverLinuxDNS(ctx context.Context, profile *coexist.Profile) {
	resolv, resolvErr := a.runner.Run(ctx, "cat", "/etc/resolv.conf")
	if resolvErr != nil {
		addLinuxIncomplete(profile, coexist.LayerDNS)
	} else {
		for _, address := range parseDNS(string(resolv)) {
			profile.DNSResolvers = append(profile.DNSResolvers, coexist.DNSResolver{
				Address: address, Source: "resolv.conf", Owner: coexist.OwnerSystem,
			})
		}
	}
	if output, err := a.runTextOptional(ctx, "resolvectl", "status"); err == nil {
		for _, line := range strings.Split(output, "\n") {
			key, value, ok := strings.Cut(line, ":")
			if !ok || !strings.Contains(strings.ToLower(key), "dns server") {
				continue
			}
			address := strings.TrimSpace(value)
			if address != "" {
				profile.DNSResolvers = append(profile.DNSResolvers, coexist.DNSResolver{
					Address: address, Source: "resolvectl", Owner: coexist.OwnerSystem,
				})
			}
		}
	}
	profile.DNSResolvers = uniqueLinuxDNS(profile.DNSResolvers)
	if len(profile.DNSResolvers) == 0 {
		addLinuxIncomplete(profile, coexist.LayerDNS)
	}
}

func (a *Adapter) discoverLinuxListeners(ctx context.Context, profile *coexist.Profile) {
	output, err := a.runner.Run(ctx, "ss", "-lnt")
	if err != nil {
		addLinuxIncomplete(profile, coexist.LayerListener)
		return
	}
	for _, line := range strings.Split(string(output), "\n") {
		match := ssListenPattern.FindStringSubmatch(strings.TrimSpace(line))
		if len(match) != 2 {
			continue
		}
		address, portText := splitHostPort(match[1])
		port, _ := strconv.Atoi(portText)
		profile.Listeners = append(profile.Listeners, coexist.ListenerRecord{
			Address: address, Port: port, Owner: coexist.OwnerUnknown,
		})
	}
}

func (a *Adapter) digestLinuxHosts(ctx context.Context, profile *coexist.Profile) {
	output, err := a.runner.Run(ctx, "cat", "/etc/hosts")
	if err != nil {
		addLinuxIncomplete(profile, coexist.LayerHosts)
		return
	}
	sum := sha256.Sum256(output)
	profile.HostsDigest = hex.EncodeToString(sum[:])
}

func (a *Adapter) discoverLinuxFirewall(ctx context.Context, profile *coexist.Profile) {
	_, iptablesErr := a.runner.Run(ctx, "iptables-save")
	_, nftErr := a.runner.Run(ctx, "nft", "list", "ruleset")
	if iptablesErr != nil && nftErr != nil {
		addLinuxIncomplete(profile, coexist.LayerFirewall)
	}
}

func parseLinuxRoutes(output string) []coexist.RouteRecord {
	routes := []coexist.RouteRecord{}
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		route := coexist.RouteRecord{Destination: fields[0]}
		for i := 0; i < len(fields); i++ {
			switch fields[i] {
			case "via":
				if i+1 < len(fields) {
					route.Gateway = fields[i+1]
				}
			case "dev":
				if i+1 < len(fields) {
					route.Interface = fields[i+1]
				}
			case "table":
				if i+1 < len(fields) {
					route.Table = fields[i+1]
				}
			}
		}
		switch {
		case strings.HasPrefix(route.Interface, "eth"), strings.HasPrefix(route.Interface, "wlan"), strings.HasPrefix(route.Interface, "en"):
			route.Owner = coexist.OwnerSystem
		case strings.HasPrefix(route.Interface, "wg"):
			route.Owner = coexist.OwnerKnownThirdParty
		default:
			route.Owner = coexist.OwnerUnknown
		}
		routes = append(routes, route)
	}
	return routes
}

func parseLinuxRouteRules(output string, routes []coexist.RouteRecord) []coexist.RouteRule {
	rules := []coexist.RouteRule{}
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		priority := strings.TrimSuffix(fields[0], ":")
		table := ""
		for i := 0; i < len(fields); i++ {
			if fields[i] == "lookup" && i+1 < len(fields) {
				table = fields[i+1]
				break
			}
		}
		if table == "" {
			continue
		}
		owner := coexist.OwnerUnknown
		if table == "main" || table == "local" || table == "default" {
			owner = coexist.OwnerSystem
		} else {
			for _, route := range routes {
				if route.Table == table && strings.HasPrefix(route.Interface, "wg") {
					owner = coexist.OwnerKnownThirdParty
					break
				}
			}
		}
		rules = append(rules, coexist.RouteRule{Priority: priority, Table: table, Owner: owner})
	}
	return rules
}

func linuxEnvironmentProxies() []coexist.ProxyRecord {
	proxies := []coexist.ProxyRecord{}
	bypass := splitLinuxBypass(os.Getenv("NO_PROXY"))
	for _, key := range []string{"HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY"} {
		value := strings.TrimSpace(os.Getenv(key))
		if value == "" {
			continue
		}
		proxies = append(proxies, coexist.ProxyRecord{
			Kind: "environment", Enabled: true, URL: sanitizeProxyURL(value), Bypass: bypass,
			Owner: coexist.OwnerKnownThirdParty, Complete: true,
		})
	}
	return proxies
}

func sanitizeProxyURL(value string) string {
	parsed, err := url.Parse(value)
	if err != nil {
		return "<redacted>"
	}
	parsed.User = nil
	return parsed.String()
}

func splitLinuxBypass(value string) []string {
	result := []string{}
	for _, part := range strings.Split(value, ",") {
		part = strings.TrimSpace(part)
		if part != "" {
			result = append(result, part)
		}
	}
	return result
}

func splitHostPort(value string) (string, string) {
	value = strings.TrimSpace(value)
	if strings.HasPrefix(value, "[") {
		address, port, ok := strings.Cut(value, "]:")
		if ok {
			return strings.TrimPrefix(address, "["), port
		}
	}
	index := strings.LastIndex(value, ":")
	if index < 0 {
		return value, ""
	}
	return value[:index], value[index+1:]
}

func uniqueLinuxDNS(resolvers []coexist.DNSResolver) []coexist.DNSResolver {
	seen := map[string]struct{}{}
	result := []coexist.DNSResolver{}
	for _, resolver := range resolvers {
		if _, exists := seen[resolver.Address]; exists {
			continue
		}
		seen[resolver.Address] = struct{}{}
		result = append(result, resolver)
	}
	return result
}

func addLinuxIncomplete(profile *coexist.Profile, layer coexist.Layer) {
	for _, existing := range profile.IncompleteLayers {
		if existing == layer {
			return
		}
	}
	profile.IncompleteLayers = append(profile.IncompleteLayers, layer)
}
