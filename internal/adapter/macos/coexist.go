package macos

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"regexp"
	"strconv"
	"strings"

	"github.com/nocaptain-oss/archie-link/internal/coexist"
	"github.com/nocaptain-oss/archie-link/internal/model"
)

var listenerPattern = regexp.MustCompile(`TCP\s+(\S+):(\d+)\s+\(LISTEN\)`)

func (a *Adapter) Coexistence(ctx context.Context) (coexist.Profile, error) {
	profile := coexist.Profile{
		SchemaVersion:    model.SchemaVersion,
		OS:               "darwin",
		Routes:           []coexist.RouteRecord{},
		DNSResolvers:     []coexist.DNSResolver{},
		Proxies:          []coexist.ProxyRecord{},
		Listeners:        []coexist.ListenerRecord{},
		VPNs:             []coexist.VPNRecord{},
		Protected:        []coexist.ProtectedEndpoint{},
		IncompleteLayers: []coexist.Layer{},
	}

	profile.VPNs = a.discoverVPNs(ctx, &profile)
	profile.Routes = a.discoverRoutes(ctx, profile.VPNs, &profile)
	profile.RouteRules = []coexist.RouteRule{}
	a.discoverDNS(ctx, &profile)
	a.discoverProxies(ctx, &profile, defaultRouteInterface(profile.Routes))
	a.discoverListeners(ctx, &profile)
	a.digestHosts(ctx, &profile)
	if _, err := a.runner.Run(ctx, "pfctl", "-s", "info"); err != nil {
		addIncomplete(&profile, coexist.LayerFirewall)
	}

	return profile, nil
}

func (a *Adapter) discoverVPNs(ctx context.Context, profile *coexist.Profile) []coexist.VPNRecord {
	vpns := []coexist.VPNRecord{}
	knownProcess := ""
	for _, process := range []string{"wireguard-go", "ShadowsocksX", "Clash", "Surge"} {
		output, err := a.runner.Run(ctx, "pgrep", "-fl", process)
		if err == nil && strings.TrimSpace(string(output)) != "" {
			knownProcess = process
			vpns = append(vpns, coexist.VPNRecord{Name: process, Kind: "process", Owner: coexist.OwnerKnownThirdParty})
		}
	}

	output, err := a.runner.Run(ctx, "ifconfig")
	if err != nil {
		addIncomplete(profile, coexist.LayerVPN)
		return vpns
	}
	for _, line := range strings.Split(string(output), "\n") {
		match := interfacePattern.FindStringSubmatch(line)
		if len(match) != 2 || !strings.HasPrefix(match[1], "utun") {
			continue
		}
		if knownProcess == "" {
			continue
		}
		vpns = append(vpns, coexist.VPNRecord{
			Name: match[1], Kind: "tunnel", Owner: coexist.OwnerKnownThirdParty,
		})
	}
	return uniqueVPNs(vpns)
}

func (a *Adapter) discoverRoutes(ctx context.Context, vpns []coexist.VPNRecord, profile *coexist.Profile) []coexist.RouteRecord {
	routes := []coexist.RouteRecord{}
	output, err := a.runner.Run(ctx, "netstat", "-nr", "-f", "inet")
	if err == nil {
		routes = append(routes, parseNetstatRoutes(string(output))...)
	}
	if _, err := a.runner.Run(ctx, "netstat", "-nr", "-f", "inet6"); err != nil {
		addIncomplete(profile, coexist.LayerRoute)
	}

	if len(routes) == 0 {
		defaultOutput, err := a.runner.Run(ctx, "route", "-n", "get", "default")
		if err != nil {
			addIncomplete(profile, coexist.LayerRoute)
			return routes
		}
		gateway, iface := parseDefaultRoute(string(defaultOutput))
		routes = append(routes, coexist.RouteRecord{Destination: "default", Gateway: gateway, Interface: iface})
	}

	for i := range routes {
		routes[i].Owner = routeOwner(routes[i].Interface, vpns)
	}
	return routes
}

func (a *Adapter) discoverDNS(ctx context.Context, profile *coexist.Profile) {
	output, err := a.runner.Run(ctx, "scutil", "--dns")
	if err != nil {
		addIncomplete(profile, coexist.LayerDNS)
		return
	}
	for _, address := range parseScutilDNS(string(output)) {
		profile.DNSResolvers = append(profile.DNSResolvers, coexist.DNSResolver{
			Address: address,
			Source:  "scutil",
			Owner:   coexist.OwnerSystem,
		})
	}
	if len(profile.DNSResolvers) == 0 {
		addIncomplete(profile, coexist.LayerDNS)
	}
}

func (a *Adapter) discoverProxies(ctx context.Context, profile *coexist.Profile, iface string) {
	proxyOutput, proxyErr := a.runner.Run(ctx, "scutil", "--proxy")
	service := a.activeNetworkService(ctx, iface)
	if proxyErr != nil || service == "" {
		addIncomplete(profile, coexist.LayerProxy)
	}

	bypass := []string{}
	pacComplete := true
	if service == "" {
		pacComplete = false
	} else {
		bypassOutput, bypassErr := a.runner.Run(ctx, "networksetup", "-getproxybypassdomains", service)
		if bypassErr != nil {
			pacComplete = false
		} else {
			bypass = parseBypassDomains(string(bypassOutput))
		}
		pacOutput, pacErr := a.runner.Run(ctx, "networksetup", "-getautoproxyurl", service)
		if pacErr != nil {
			pacComplete = false
		} else if url, enabled := parsePAC(string(pacOutput)); enabled && url != "" {
			profile.Proxies = append(profile.Proxies, coexist.ProxyRecord{
				Kind: "pac", Enabled: true, URL: url, Bypass: bypass,
				Owner: coexist.OwnerKnownThirdParty, Complete: pacComplete,
			})
		}
	}

	if proxyErr == nil {
		for _, proxy := range parseScutilProxies(string(proxyOutput), bypass) {
			proxy.Complete = pacComplete
			profile.Proxies = append(profile.Proxies, proxy)
		}
	}
	if !pacComplete {
		addIncomplete(profile, coexist.LayerProxy)
	}
}

func defaultRouteInterface(routes []coexist.RouteRecord) string {
	for _, route := range routes {
		if route.Destination == "default" {
			return route.Interface
		}
	}
	return ""
}

func (a *Adapter) discoverListeners(ctx context.Context, profile *coexist.Profile) {
	output, err := a.runner.Run(ctx, "lsof", "-nP", "-iTCP", "-sTCP:LISTEN")
	if err != nil {
		addIncomplete(profile, coexist.LayerListener)
		return
	}
	for _, line := range strings.Split(string(output), "\n") {
		match := listenerPattern.FindStringSubmatch(line)
		if len(match) != 3 {
			continue
		}
		port, _ := strconv.Atoi(match[2])
		fields := strings.Fields(line)
		process := ""
		if len(fields) > 0 {
			process = fields[0]
		}
		profile.Listeners = append(profile.Listeners, coexist.ListenerRecord{
			Address: match[1], Port: port, Process: process, Owner: listenerOwner(process),
		})
	}
}

func (a *Adapter) digestHosts(ctx context.Context, profile *coexist.Profile) {
	output, err := a.runner.Run(ctx, "cat", "/etc/hosts")
	if err != nil {
		addIncomplete(profile, coexist.LayerHosts)
		return
	}
	digest := sha256.Sum256(output)
	profile.HostsDigest = hex.EncodeToString(digest[:])
}

func parseNetstatRoutes(output string) []coexist.RouteRecord {
	routes := []coexist.RouteRecord{}
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 4 || fields[0] != "default" {
			continue
		}
		iface := fields[len(fields)-1]
		routes = append(routes, coexist.RouteRecord{
			Destination: "default",
			Gateway:     fields[1],
			Interface:   iface,
		})
	}
	return routes
}

func routeOwner(iface string, vpns []coexist.VPNRecord) coexist.Owner {
	if iface == "" {
		return coexist.OwnerUnknown
	}
	if strings.HasPrefix(iface, "en") || strings.HasPrefix(iface, "wl") || strings.HasPrefix(iface, "eth") {
		return coexist.OwnerSystem
	}
	for _, vpn := range vpns {
		if vpn.Name == iface {
			return vpn.Owner
		}
	}
	return coexist.OwnerUnknown
}

func parseScutilProxies(output string, bypass []string) []coexist.ProxyRecord {
	values := map[string]string{}
	for _, line := range strings.Split(output, "\n") {
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		values[strings.TrimSpace(key)] = strings.TrimSpace(value)
	}

	proxies := []coexist.ProxyRecord{}
	for _, item := range []struct {
		enable string
		host   string
		port   string
		kind   string
	}{
		{"HTTPEnable", "HTTPProxy", "HTTPPort", "http"},
		{"HTTPSEnable", "HTTPSProxy", "HTTPSPort", "https"},
		{"SOCKSEnable", "SOCKSProxy", "SOCKSPort", "socks"},
	} {
		if values[item.enable] != "1" || values[item.host] == "" {
			continue
		}
		url := values[item.host]
		if values[item.port] != "" {
			url += ":" + values[item.port]
		}
		proxies = append(proxies, coexist.ProxyRecord{
			Kind: item.kind, Enabled: true, URL: url, Bypass: bypass,
			Owner: coexist.OwnerKnownThirdParty, Complete: true,
		})
	}
	return proxies
}

func firstNetworkService(output string) string {
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "An asterisk") || strings.HasPrefix(line, "*") {
			continue
		}
		return line
	}
	return ""
}

func parseBypassDomains(output string) []string {
	result := []string{}
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.Contains(strings.ToLower(line), "aren't any bypass") {
			continue
		}
		result = append(result, line)
	}
	return result
}

func parsePAC(output string) (string, bool) {
	var url string
	var enabled bool
	for _, line := range strings.Split(output, "\n") {
		key, value, ok := strings.Cut(strings.TrimSpace(line), ":")
		if !ok {
			continue
		}
		switch strings.TrimSpace(key) {
		case "URL":
			if strings.TrimSpace(value) != "(null)" {
				url = strings.TrimSpace(value)
			}
		case "Enabled":
			enabled = strings.EqualFold(strings.TrimSpace(value), "Yes")
		}
	}
	return url, enabled
}

func listenerOwner(process string) coexist.Owner {
	lower := strings.ToLower(process)
	if strings.Contains(lower, "archielink") {
		return coexist.OwnerArchielink
	}
	for _, known := range []string{"clash", "surge", "shadowsocks", "node", "python", "ollama"} {
		if strings.Contains(lower, known) {
			return coexist.OwnerKnownThirdParty
		}
	}
	return coexist.OwnerUnknown
}

func addIncomplete(profile *coexist.Profile, layer coexist.Layer) {
	for _, existing := range profile.IncompleteLayers {
		if existing == layer {
			return
		}
	}
	profile.IncompleteLayers = append(profile.IncompleteLayers, layer)
}

func uniqueVPNs(vpns []coexist.VPNRecord) []coexist.VPNRecord {
	seen := map[string]struct{}{}
	result := []coexist.VPNRecord{}
	for _, vpn := range vpns {
		key := vpn.Name + "|" + vpn.Kind
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, vpn)
	}
	return result
}
