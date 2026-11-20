package macos

import (
	"bufio"
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/nocaptain-oss/archie-link/internal/model"
)

var interfacePattern = regexp.MustCompile(`^([a-zA-Z0-9]+):`)

func (a *Adapter) Snapshot(ctx context.Context) (model.NetworkReport, error) {
	route, err := a.runText(ctx, "route", "-n", "get", "default")
	if err != nil {
		return model.NetworkReport{}, err
	}
	gateway, iface := parseDefaultRoute(route)
	service := a.activeNetworkService(ctx, iface)
	dns, err := a.runText(ctx, "networksetup", "-getdnsservers", service)
	if err != nil {
		return model.NetworkReport{}, err
	}
	proxy, err := a.runText(ctx, "scutil", "--proxy")
	if err != nil {
		return model.NetworkReport{}, err
	}
	ifconfig, ifconfigErr := a.runner.Run(ctx, "ifconfig")
	pgrep, pgrepErr := a.runner.Run(ctx, "pgrep", "-fl", "wireguard-go")
	dnsDetail, dnsDetailErr := a.runner.Run(ctx, "scutil", "--dns")

	tunnels := parseTunnels(ifconfig, ifconfigErr)
	if pgrepErr == nil && strings.Contains(string(pgrep), "wireguard-go") {
		tunnels = append(tunnels, "wireguard-go")
	}
	tunnels = uniqueStrings(tunnels)
	sort.Strings(tunnels)
	serviceDNS := parseDNS(dns)
	dnsServers := serviceDNS
	if len(dnsServers) == 0 && dnsDetailErr == nil {
		dnsServers = parseScutilDNS(string(dnsDetail))
	}

	return model.NetworkReport{
		DefaultInterface: iface,
		NetworkService:   service,
		Gateway:          gateway,
		DNS:              dnsServers,
		ServiceDNS:       serviceDNS,
		ProxyEnabled:     parseProxyEnabled(proxy),
		ExistingTunnels:  tunnels,
	}, nil
}

func (a *Adapter) activeNetworkService(ctx context.Context, iface string) string {
	if order, err := a.runner.Run(ctx, "networksetup", "-listnetworkserviceorder"); err == nil {
		if service := serviceForInterface(string(order), iface); service != "" {
			return service
		}
	}
	if services, err := a.runner.Run(ctx, "networksetup", "-listallnetworkservices"); err == nil {
		if service := firstNetworkService(string(services)); service != "" {
			return service
		}
	}
	return "Wi-Fi"
}

func serviceForInterface(output, iface string) string {
	if iface == "" {
		return ""
	}
	service := ""
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if match := regexp.MustCompile(`^\(\d+\)\s+(.+)$`).FindStringSubmatch(line); len(match) == 2 {
			service = strings.TrimSpace(match[1])
			continue
		}
		if service != "" && strings.Contains(line, "Device: "+iface+")") {
			return service
		}
	}
	return ""
}

func parseScutilDNS(output string) []string {
	seen := map[string]struct{}{}
	result := []string{}
	for _, line := range strings.Split(output, "\n") {
		key, value, ok := strings.Cut(strings.TrimSpace(line), ":")
		if !ok || !strings.HasPrefix(strings.TrimSpace(key), "nameserver[") {
			continue
		}
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
	return result
}

func (a *Adapter) runText(ctx context.Context, name string, args ...string) (string, error) {
	output, err := a.runner.Run(ctx, name, args...)
	if err != nil {
		return "", fmt.Errorf("%s %s: %w", name, strings.Join(args, " "), err)
	}
	return strings.TrimSpace(string(output)), nil
}

func parseDefaultRoute(output string) (gateway, iface string) {
	for _, line := range strings.Split(output, "\n") {
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		switch strings.TrimSpace(key) {
		case "gateway":
			gateway = strings.TrimSpace(value)
		case "interface":
			iface = strings.TrimSpace(value)
		}
	}
	return gateway, iface
}

func parseDNS(output string) []string {
	result := []string{}
	for _, line := range strings.Split(output, "\n") {
		value := strings.TrimSpace(line)
		if value == "" || strings.Contains(strings.ToLower(value), "there aren't any dns servers") {
			continue
		}
		result = append(result, value)
	}
	return result
}

func parseProxyEnabled(output string) bool {
	for _, key := range []string{"HTTPEnable", "HTTPSEnable", "SOCKSEnable"} {
		if regexp.MustCompile(`(?m)^\s*` + key + `\s*:\s*1\s*$`).MatchString(output) {
			return true
		}
	}
	return false
}

func parseTunnels(output []byte, err error) []string {
	if err != nil {
		return []string{}
	}
	tunnels := []string{}
	scanner := bufio.NewScanner(strings.NewReader(string(output)))
	for scanner.Scan() {
		match := interfacePattern.FindStringSubmatch(scanner.Text())
		if len(match) == 2 && strings.HasPrefix(match[1], "utun") {
			tunnels = append(tunnels, match[1])
		}
	}
	return tunnels
}

func uniqueStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}
