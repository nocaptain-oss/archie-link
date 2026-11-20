package linux

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/nocaptain-oss/archie-link/internal/model"
)

func (a *Adapter) Snapshot(ctx context.Context) (model.NetworkReport, error) {
	route, err := a.runText(ctx, "ip", "route", "show", "default")
	if err != nil {
		return model.NetworkReport{}, err
	}
	resolv, err := a.runText(ctx, "cat", "/etc/resolv.conf")
	if err != nil {
		return model.NetworkReport{}, err
	}
	_, _ = a.runTextOptional(ctx, "resolvectl", "status")
	_, _ = a.runTextOptional(ctx, "nmcli", "device", "show")
	links, _ := a.runTextOptional(ctx, "ip", "link", "show")

	gateway, iface := parseDefaultRoute(route)
	tunnels := parseTunnels(links)
	sort.Strings(tunnels)
	return model.NetworkReport{
		DefaultInterface: iface,
		Gateway:          gateway,
		DNS:              parseDNS(resolv),
		ProxyEnabled:     proxyFromEnvironment(),
		ExistingTunnels:  tunnels,
	}, nil
}

func (a *Adapter) runText(ctx context.Context, name string, args ...string) (string, error) {
	output, err := a.runner.Run(ctx, name, args...)
	if err != nil {
		return "", fmt.Errorf("%s %s: %w", name, strings.Join(args, " "), err)
	}
	return strings.TrimSpace(string(output)), nil
}

func (a *Adapter) runTextOptional(ctx context.Context, name string, args ...string) (string, error) {
	output, err := a.runner.Run(ctx, name, args...)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(output)), nil
}

func parseDefaultRoute(output string) (gateway, iface string) {
	fields := strings.Fields(output)
	for i := 0; i < len(fields); i++ {
		switch fields[i] {
		case "via":
			if i+1 < len(fields) {
				gateway = fields[i+1]
			}
		case "dev":
			if i+1 < len(fields) {
				iface = fields[i+1]
			}
		}
	}
	return gateway, iface
}

func parseDNS(output string) []string {
	seen := map[string]struct{}{}
	result := []string{}
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 || fields[0] != "nameserver" {
			continue
		}
		if _, exists := seen[fields[1]]; exists {
			continue
		}
		seen[fields[1]] = struct{}{}
		result = append(result, fields[1])
	}
	return result
}

func parseTunnels(output string) []string {
	result := []string{}
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		name := strings.TrimSuffix(fields[1], ":")
		if strings.HasPrefix(name, "wg") || strings.Contains(strings.ToLower(name), "wireguard") {
			result = append(result, name)
		}
	}
	return result
}

func proxyFromEnvironment() bool {
	for _, key := range []string{"HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY"} {
		if strings.TrimSpace(os.Getenv(key)) != "" {
			return true
		}
	}
	return false
}
