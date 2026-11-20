package windows

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"strings"

	"github.com/nocaptain-oss/archie-link/internal/coexist"
	"github.com/nocaptain-oss/archie-link/internal/model"
)

const (
	routeListScript  = "Get-NetRoute | ForEach-Object { '{0}|{1}|{2}' -f $_.DestinationPrefix,$_.NextHop,$_.InterfaceAlias }"
	nrptScript       = "Get-DnsClientNrptPolicy"
	wininetScript    = "$p=Get-ItemProperty 'HKCU:\\Software\\Microsoft\\Windows\\CurrentVersion\\Internet Settings'; 'ProxyEnable={0}' -f $p.ProxyEnable; 'ProxyServer={0}' -f $p.ProxyServer; 'AutoConfigURL={0}' -f $p.AutoConfigURL; 'ProxyOverride={0}' -f $p.ProxyOverride"
	vpnScript        = "Get-VpnConnection | ForEach-Object { '{0}|{1}' -f $_.Name,$_.ConnectionStatus }"
	vpnProcessScript = "Get-Process | Where-Object {$_.ProcessName -match '^(wireguard|openvpn|warp-svc)'} | ForEach-Object { '{0}|{1}' -f $_.ProcessName,$_.Id }"
	listenerScript   = "Get-NetTCPConnection -State Listen | ForEach-Object { '{0}|{1}|{2}' -f $_.LocalAddress,$_.LocalPort,$_.OwningProcess }"
	hostsScript      = "Get-Content C:\\Windows\\System32\\drivers\\etc\\hosts"
	firewallScript   = "Get-NetFirewallProfile | ForEach-Object { '{0}|{1}' -f $_.Name,$_.Enabled }"
	powershellPrefix = "[Console]::OutputEncoding = New-Object System.Text.UTF8Encoding($false); $OutputEncoding = [Console]::OutputEncoding; "

	routeListCommand  = "powershell -NoProfile -NonInteractive -Command " + powershellPrefix + routeListScript
	nrptCommand       = "powershell -NoProfile -NonInteractive -Command " + powershellPrefix + nrptScript
	wininetCommand    = "powershell -NoProfile -NonInteractive -Command " + powershellPrefix + wininetScript
	winHTTPCommand    = "netsh winhttp show proxy"
	vpnCommand        = "powershell -NoProfile -NonInteractive -Command " + powershellPrefix + vpnScript
	vpnProcessCommand = "powershell -NoProfile -NonInteractive -Command " + powershellPrefix + vpnProcessScript
	listenerCommand   = "powershell -NoProfile -NonInteractive -Command " + powershellPrefix + listenerScript
	hostsCommand      = "powershell -NoProfile -NonInteractive -Command " + powershellPrefix + hostsScript
	firewallCommand   = "powershell -NoProfile -NonInteractive -Command " + powershellPrefix + firewallScript
)

func (a *Adapter) Coexistence(ctx context.Context) (coexist.Profile, error) {
	profile := coexist.Profile{
		SchemaVersion:    model.SchemaVersion,
		OS:               "windows",
		Routes:           []coexist.RouteRecord{},
		DNSResolvers:     []coexist.DNSResolver{},
		Proxies:          []coexist.ProxyRecord{},
		Listeners:        []coexist.ListenerRecord{},
		VPNs:             []coexist.VPNRecord{},
		Protected:        []coexist.ProtectedEndpoint{},
		IncompleteLayers: []coexist.Layer{},
	}

	profile.VPNs = a.discoverWindowsVPNs(ctx, &profile)
	profile.Routes = a.discoverWindowsRoutes(ctx, &profile)
	profile.RouteRules = []coexist.RouteRule{}
	a.discoverWindowsDNS(ctx, &profile)
	a.discoverWindowsProxies(ctx, &profile)
	a.discoverWindowsListeners(ctx, &profile)
	a.digestWindowsHosts(ctx, &profile)
	if _, err := a.runPowerShellScript(ctx, firewallScript); err != nil {
		addWindowsIncomplete(&profile, coexist.LayerFirewall)
	}
	return profile, nil
}

func (a *Adapter) discoverWindowsVPNs(ctx context.Context, profile *coexist.Profile) []coexist.VPNRecord {
	vpns := []coexist.VPNRecord{}
	if output, err := a.runPowerShellScript(ctx, vpnScript); err == nil {
		for _, line := range nonEmptyLines(string(output)) {
			parts := strings.Split(line, "|")
			if len(parts) < 2 || strings.EqualFold(parts[1], "Disconnected") {
				continue
			}
			vpns = append(vpns, coexist.VPNRecord{Name: parts[0], Kind: "windows-vpn", Owner: coexist.OwnerKnownThirdParty})
		}
	}
	if output, err := a.runPowerShellScript(ctx, vpnProcessScript); err == nil {
		for _, line := range nonEmptyLines(string(output)) {
			parts := strings.Split(line, "|")
			vpns = append(vpns, coexist.VPNRecord{Name: parts[0], Kind: "process", Owner: coexist.OwnerKnownThirdParty})
		}
	}
	return uniqueWindowsVPNs(vpns)
}

func (a *Adapter) discoverWindowsRoutes(ctx context.Context, profile *coexist.Profile) []coexist.RouteRecord {
	output, err := a.runPowerShellScript(ctx, routeListScript)
	if err != nil {
		addWindowsIncomplete(profile, coexist.LayerRoute)
		return []coexist.RouteRecord{}
	}
	routes := []coexist.RouteRecord{}
	for _, line := range nonEmptyLines(string(output)) {
		parts := strings.Split(line, "|")
		if len(parts) < 3 {
			continue
		}
		owner := coexist.OwnerUnknown
		if isPhysicalWindowsInterface(parts[2]) {
			owner = coexist.OwnerSystem
		}
		routes = append(routes, coexist.RouteRecord{
			Destination: parts[0], Gateway: parts[1], Interface: parts[2], Owner: owner,
		})
	}
	return routes
}

func (a *Adapter) discoverWindowsDNS(ctx context.Context, profile *coexist.Profile) {
	output, err := a.runner.Run(ctx, "powershell", "-NoProfile", "-Command", dnsScript)
	if err != nil {
		addWindowsIncomplete(profile, coexist.LayerDNS)
		return
	}
	for _, address := range parseDNS(string(output)) {
		profile.DNSResolvers = append(profile.DNSResolvers, coexist.DNSResolver{
			Address: address, Source: "windns", Owner: coexist.OwnerSystem,
		})
	}
	if _, err := a.runPowerShellScript(ctx, nrptScript); err != nil {
		addWindowsIncomplete(profile, coexist.LayerDNS)
	}
}

func (a *Adapter) discoverWindowsProxies(ctx context.Context, profile *coexist.Profile) {
	wininet, wininetErr := a.runPowerShellScript(ctx, wininetScript)
	winhttp, winhttpErr := a.runner.Run(ctx, "netsh", "winhttp", "show", "proxy")
	if wininetErr != nil || winhttpErr != nil {
		addWindowsIncomplete(profile, coexist.LayerProxy)
	}

	values := parseKeyValueLines(string(wininet))
	bypass := splitBypass(values["ProxyOverride"])
	if values["ProxyEnable"] == "1" && values["ProxyServer"] != "" {
		profile.Proxies = append(profile.Proxies, coexist.ProxyRecord{
			Kind: "wininet", Enabled: true, URL: values["ProxyServer"], Bypass: bypass,
			Owner: coexist.OwnerKnownThirdParty, Complete: wininetErr == nil,
		})
	}
	if values["AutoConfigURL"] != "" {
		profile.Proxies = append(profile.Proxies, coexist.ProxyRecord{
			Kind: "pac", Enabled: true, URL: values["AutoConfigURL"], Bypass: bypass,
			Owner: coexist.OwnerKnownThirdParty, Complete: wininetErr == nil,
		})
	}
	if winhttpErr == nil {
		for _, line := range nonEmptyLines(string(winhttp)) {
			lower := strings.ToLower(line)
			if strings.Contains(lower, "proxy server") && strings.Contains(line, ":") {
				_, value, _ := strings.Cut(line, ":")
				profile.Proxies = append(profile.Proxies, coexist.ProxyRecord{
					Kind: "winhttp", Enabled: true, URL: strings.TrimSpace(value), Bypass: bypass,
					Owner: coexist.OwnerKnownThirdParty, Complete: true,
				})
				break
			}
		}
	}
}

func (a *Adapter) discoverWindowsListeners(ctx context.Context, profile *coexist.Profile) {
	output, err := a.runPowerShellScript(ctx, listenerScript)
	if err != nil {
		addWindowsIncomplete(profile, coexist.LayerListener)
		return
	}
	for _, line := range nonEmptyLines(string(output)) {
		parts := strings.Split(line, "|")
		if len(parts) < 3 {
			continue
		}
		port, _ := strconv.Atoi(parts[1])
		profile.Listeners = append(profile.Listeners, coexist.ListenerRecord{
			Address: parts[0], Port: port, Process: parts[2], Owner: windowsListenerOwner(parts[2]),
		})
	}
}

func (a *Adapter) digestWindowsHosts(ctx context.Context, profile *coexist.Profile) {
	output, err := a.runPowerShellScript(ctx, hostsScript)
	if err != nil {
		addWindowsIncomplete(profile, coexist.LayerHosts)
		return
	}
	sum := sha256.Sum256(output)
	profile.HostsDigest = hex.EncodeToString(sum[:])
}

func parseKeyValueLines(output string) map[string]string {
	result := map[string]string{}
	for _, line := range strings.Split(output, "\n") {
		key, value, ok := strings.Cut(strings.TrimSpace(line), "=")
		if ok {
			result[strings.TrimSpace(key)] = strings.TrimSpace(value)
		}
	}
	return result
}

func (a *Adapter) runPowerShellScript(ctx context.Context, script string) ([]byte, error) {
	return a.runner.Run(ctx, "powershell", "-NoProfile", "-NonInteractive", "-Command", powershellPrefix+script)
}

func splitBypass(value string) []string {
	result := []string{}
	for _, part := range strings.Split(value, ";") {
		part = strings.TrimSpace(part)
		if part != "" {
			result = append(result, part)
		}
	}
	return result
}

func isPhysicalWindowsInterface(name string) bool {
	lower := strings.ToLower(name)
	for _, prefix := range []string{"wlan", "wi-fi", "ethernet", "以太网"} {
		if strings.Contains(lower, prefix) {
			return true
		}
	}
	return false
}

func windowsListenerOwner(process string) coexist.Owner {
	lower := strings.ToLower(process)
	if strings.Contains(lower, "archielink") {
		return coexist.OwnerArchielink
	}
	for _, known := range []string{"node", "python", "clash", "surge", "ollama"} {
		if strings.Contains(lower, known) {
			return coexist.OwnerKnownThirdParty
		}
	}
	return coexist.OwnerUnknown
}

func nonEmptyLines(output string) []string {
	result := []string{}
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			result = append(result, line)
		}
	}
	return result
}

func uniqueWindowsVPNs(vpns []coexist.VPNRecord) []coexist.VPNRecord {
	seen := map[string]struct{}{}
	result := []coexist.VPNRecord{}
	for _, vpn := range vpns {
		key := vpn.Name + "|" + vpn.Kind
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, vpn)
	}
	return result
}

func addWindowsIncomplete(profile *coexist.Profile, layer coexist.Layer) {
	for _, existing := range profile.IncompleteLayers {
		if existing == layer {
			return
		}
	}
	profile.IncompleteLayers = append(profile.IncompleteLayers, layer)
}
