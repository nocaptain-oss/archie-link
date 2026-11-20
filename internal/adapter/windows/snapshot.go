package windows

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/nocaptain-oss/archie-link/internal/model"
)

const (
	routeScript   = "$r=Get-NetRoute -DestinationPrefix '0.0.0.0/0' | Sort-Object RouteMetric | Select-Object -First 1; '{0}|{1}|{2}' -f $r.NextHop,$r.InterfaceAlias,$r.InterfaceIndex"
	dnsScript     = "Get-DnsClientServerAddress -AddressFamily IPv4 | ForEach-Object { '{0}|{1}' -f $_.InterfaceAlias, ($_.ServerAddresses -join ',') }"
	proxyScript   = "(Get-ItemProperty 'HKCU:\\Software\\Microsoft\\Windows\\CurrentVersion\\Internet Settings').ProxyEnable"
	adapterScript = "Get-NetAdapter | ForEach-Object { '{0}|{1}' -f $_.Name,$_.Status }"
	processScript = "Get-Process | Where-Object {$_.ProcessName -match '^(wireguard|wireguard-go|warp-svc|openvpn)'} | ForEach-Object { $_.ProcessName }"

	routeCommand   = "powershell -NoProfile -NonInteractive -Command " + routeScript
	dnsCommand     = "powershell -NoProfile -NonInteractive -Command " + dnsScript
	proxyCommand   = "powershell -NoProfile -NonInteractive -Command " + proxyScript
	adapterCommand = "powershell -NoProfile -NonInteractive -Command " + adapterScript
	processCommand = "powershell -NoProfile -NonInteractive -Command " + processScript
)

func (a *Adapter) Snapshot(ctx context.Context) (model.NetworkReport, error) {
	route, err := a.runPowerShell(ctx, routeScript)
	if err != nil {
		return model.NetworkReport{}, err
	}
	dns, err := a.runPowerShell(ctx, dnsScript)
	if err != nil {
		return model.NetworkReport{}, err
	}
	proxy, _ := a.runPowerShellOptional(ctx, proxyScript)
	_, _ = a.runPowerShellOptional(ctx, adapterScript)
	processes, _ := a.runPowerShellOptional(ctx, processScript)

	gateway, iface := parseRoute(route)
	tunnels := parseLines(processes)
	sort.Strings(tunnels)
	return model.NetworkReport{
		DefaultInterface: iface,
		Gateway:          gateway,
		DNS:              parseDNS(dns),
		ProxyEnabled:     strings.TrimSpace(proxy) == "1",
		ExistingTunnels:  tunnels,
	}, nil
}

func (a *Adapter) runPowerShell(ctx context.Context, script string) (string, error) {
	output, err := a.runner.Run(ctx, "powershell", "-NoProfile", "-NonInteractive", "-Command", script)
	if err != nil {
		return "", fmt.Errorf("powershell command failed: %w", err)
	}
	return strings.TrimSpace(string(output)), nil
}

func (a *Adapter) runPowerShellOptional(ctx context.Context, script string) (string, error) {
	output, err := a.runner.Run(ctx, "powershell", "-NoProfile", "-NonInteractive", "-Command", script)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(output)), nil
}

func parseRoute(output string) (gateway, iface string) {
	parts := strings.Split(strings.TrimSpace(output), "|")
	if len(parts) > 0 {
		gateway = strings.TrimSpace(parts[0])
	}
	if len(parts) > 1 {
		iface = strings.TrimSpace(parts[1])
	}
	return gateway, iface
}

func parseDNS(output string) []string {
	seen := map[string]struct{}{}
	result := []string{}
	for _, line := range strings.Split(output, "\n") {
		_, values, ok := strings.Cut(strings.TrimSpace(line), "|")
		if !ok {
			continue
		}
		for _, value := range strings.Split(values, ",") {
			value = strings.TrimSpace(value)
			if value == "" {
				continue
			}
			if _, exists := seen[value]; exists {
				continue
			}
			seen[value] = struct{}{}
			result = append(result, value)
		}
	}
	return result
}

func parseLines(output string) []string {
	seen := map[string]struct{}{}
	result := []string{}
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if _, exists := seen[line]; exists {
			continue
		}
		seen[line] = struct{}{}
		result = append(result, line)
	}
	sort.Strings(result)
	return result
}
