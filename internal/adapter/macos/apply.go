package macos

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/nocaptain-oss/archie-link/internal/model"
)

const (
	managedResolverMarker = "# managed by archielink"
	ownedConfigName       = "archieauto"
)

func (a *Automatic) Apply(ctx context.Context, plan model.ApplyPlan) (model.Ownership, error) {
	if a.ownership != nil {
		return *a.ownership, nil
	}
	if err := validateEndpoint(plan.Endpoint); err != nil {
		return model.Ownership{}, err
	}
	configPath := strings.TrimSpace(plan.ConfigPath)
	if configPath == "" {
		configPath = a.options.ConfigPath
	}
	if configPath == "" {
		return model.Ownership{}, errors.New("WireGuard source config is not configured")
	}
	if strings.TrimSpace(a.options.StateDir) == "" {
		return model.Ownership{}, errors.New("automatic state directory is not configured")
	}
	if err := os.MkdirAll(a.options.StateDir, 0o700); err != nil {
		return model.Ownership{}, err
	}
	if err := os.Chmod(a.options.StateDir, 0o700); err != nil {
		return model.Ownership{}, err
	}

	resolverFiles, err := resolverPaths(a.options.ResolverDir, plan.LocalDNSDomains)
	if err != nil {
		return model.Ownership{}, err
	}
	resolverServers := usableResolverServers(plan.OriginalDNS)
	if len(resolverServers) == 0 && plan.Gateway != "" {
		resolverServers = []string{plan.Gateway}
	}
	if len(resolverFiles) > 0 && len(resolverServers) == 0 {
		return model.Ownership{}, errors.New("local DNS exceptions require at least one usable original resolver")
	}
	for _, path := range resolverFiles {
		if err := validateResolverCollision(path); err != nil {
			return model.Ownership{}, err
		}
	}

	source, err := os.ReadFile(configPath)
	if err != nil {
		return model.Ownership{}, fmt.Errorf("read WireGuard config: %w", err)
	}
	config, err := applyConfig(source, plan.Endpoint)
	if err != nil {
		return model.Ownership{}, err
	}
	ownedConfig := filepath.Join(a.options.StateDir, ownedConfigName+".conf")
	if err := writePrivateFile(ownedConfig, config); err != nil {
		return model.Ownership{}, err
	}
	managedFiles := []string{ownedConfig}

	cleanupFiles := func() {
		for _, path := range managedFiles {
			_ = os.Remove(path)
		}
	}
	for _, path := range resolverFiles {
		raw := managedResolverMarker + "\n"
		for _, server := range resolverServers {
			raw += "nameserver " + server + "\n"
		}
		raw += "search_order 1\ntimeout 2\n"
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			cleanupFiles()
			return model.Ownership{}, err
		}
		if err := os.WriteFile(path, []byte(raw), 0o644); err != nil {
			cleanupFiles()
			return model.Ownership{}, err
		}
		managedFiles = append(managedFiles, path)
	}
	directHosts, err := a.resolveDirectHosts(ctx, plan.LocalDNSDomains)
	if err != nil {
		cleanupFiles()
		return model.Ownership{}, err
	}
	plan.DirectHosts = directHosts

	runner := commandRunner{runner: a.runner}
	if len(resolverFiles) > 0 {
		_, _ = runner.Run(ctx, "dscacheutil", "-flushcache")
		_, _ = runner.Run(ctx, "killall", "-HUP", "mDNSResponder")
	}
	if _, err := runner.Run(ctx, "wg-quick", "up", ownedConfig); err != nil {
		cleanupFiles()
		return model.Ownership{}, fmt.Errorf("start WireGuard interface: %w", err)
	}

	interfaceName, err := a.ownedInterface(ctx)
	if err != nil {
		_, _ = runner.Run(context.WithoutCancel(ctx), "wg-quick", "down", ownedConfig)
		cleanupFiles()
		return model.Ownership{}, err
	}
	managedRoutes, err := a.applyRoutes(ctx, plan)
	if err != nil {
		_, _ = runner.Run(context.WithoutCancel(ctx), "wg-quick", "down", ownedConfig)
		a.removeRoutes(context.WithoutCancel(ctx), managedRoutes)
		cleanupFiles()
		return model.Ownership{}, err
	}
	if !waitForHandshake(ctx, a.runner, interfaceName, 10*time.Second, 200*time.Millisecond) {
		_, _ = runner.Run(context.WithoutCancel(ctx), "wg-quick", "down", ownedConfig)
		a.removeRoutes(context.WithoutCancel(ctx), managedRoutes)
		cleanupFiles()
		return model.Ownership{}, fmt.Errorf("WireGuard handshake did not complete on %s", interfaceName)
	}
	_, _ = runner.Run(ctx, "dscacheutil", "-flushcache")
	_, _ = runner.Run(ctx, "killall", "-HUP", "mDNSResponder")
	if err := a.installDNS(ctx, plan.NetworkService); err != nil {
		_, _ = runner.Run(context.WithoutCancel(ctx), "wg-quick", "down", ownedConfig)
		a.removeRoutes(context.WithoutCancel(ctx), managedRoutes)
		cleanupFiles()
		return model.Ownership{}, err
	}

	ownership := model.Ownership{
		Interface:     interfaceName,
		Endpoint:      plan.Endpoint,
		ManagedFiles:  append([]string(nil), managedFiles...),
		ManagedRoutes: managedRoutes,
		DirectHosts:   append([]string(nil), plan.DirectHosts...),
	}
	a.ownership = &ownership
	if err := injectAfterApply(ctx, interfaceName); err != nil {
		return model.Ownership{}, err
	}
	return ownership, nil
}

func (a *Automatic) resolveDirectHosts(ctx context.Context, domains []string) ([]string, error) {
	result := []string{}
	seen := map[string]struct{}{}
	for _, domain := range domains {
		addresses, err := a.options.ResolveIPv4(ctx, domain)
		if err != nil {
			return nil, fmt.Errorf("resolve protected domain %s: %w", domain, err)
		}
		for _, address := range addresses {
			if _, exists := seen[address]; exists {
				continue
			}
			seen[address] = struct{}{}
			result = append(result, address)
		}
	}
	return result, nil
}

func usableResolverServers(values []string) []string {
	result := []string{}
	seen := map[string]struct{}{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || strings.Contains(value, "%") {
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

func (a *Automatic) ownedInterface(ctx context.Context) (string, error) {
	output, err := a.runner.Run(ctx, "wg", "show", "interfaces")
	if err != nil {
		return "", fmt.Errorf("read WireGuard interfaces: %w", err)
	}
	fields := strings.Fields(string(output))
	if len(fields) == 0 {
		return "", errors.New("WireGuard did not report an owned interface")
	}
	return fields[len(fields)-1], nil
}

func (a *Automatic) applyRoutes(ctx context.Context, plan model.ApplyPlan) ([]string, error) {
	runner := commandRunner{runner: a.runner}
	managed := []string{}
	if plan.Endpoint.Address != "" {
		route := plan.Endpoint.Address + "/32"
		args := []string{"-n", "add", "-inet", route}
		switch {
		case plan.Gateway != "":
			args = append(args, plan.Gateway)
		case plan.DefaultInterface != "":
			args = append(args, "-interface", plan.DefaultInterface)
		default:
			return managed, fmt.Errorf("WireGuard endpoint %s needs a gateway or interface", route)
		}
		if _, err := runner.Run(ctx, "route", args...); err != nil {
			if !strings.Contains(strings.ToLower(err.Error()), "file exists") {
				return managed, fmt.Errorf("protect WireGuard endpoint %s: %w", route, err)
			}
		} else {
			managed = append(managed, route)
		}
	}
	for _, cidr := range plan.AlwaysDirectCIDRs {
		args := []string{"-n", "add", "-inet", cidr}
		if plan.DefaultInterface != "" {
			args = append(args, "-interface", plan.DefaultInterface)
		} else if plan.Gateway != "" {
			args = append(args, plan.Gateway)
		} else {
			return managed, fmt.Errorf("direct route %s needs a gateway or interface", cidr)
		}
		if _, err := runner.Run(ctx, "route", args...); err != nil {
			return managed, fmt.Errorf("install protected route %s: %w", cidr, err)
		}
		managed = append(managed, cidr)
	}
	for _, host := range plan.DirectHosts {
		route := host + "/32"
		args := []string{"-n", "add", "-inet", route}
		switch {
		case plan.Gateway != "":
			args = append(args, plan.Gateway)
		case plan.DefaultInterface != "":
			args = append(args, "-interface", plan.DefaultInterface)
		default:
			return managed, fmt.Errorf("direct host %s needs a gateway or interface", host)
		}
		if _, err := runner.Run(ctx, "route", args...); err != nil &&
			!strings.Contains(strings.ToLower(err.Error()), "file exists") {
			return managed, fmt.Errorf("install protected host route %s: %w", route, err)
		}
		managed = append(managed, route)
	}
	return managed, nil
}

func (a *Automatic) removeRoutes(ctx context.Context, routes []string) {
	for _, route := range routes {
		_, _ = a.runner.Run(ctx, "route", "-n", "delete", "-inet", route)
	}
}

func applyConfig(input []byte, endpoint model.Endpoint) ([]byte, error) {
	scanner := bufio.NewScanner(strings.NewReader(string(input)))
	section := ""
	endpointSeen := false
	lines := []string{}
	for scanner.Scan() {
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") {
			if section == "Peer" && !endpointSeen {
				return nil, errors.New("WireGuard peer has no Endpoint")
			}
			section = strings.Trim(trimmed, "[]")
			lines = append(lines, line)
			continue
		}
		key, _, _ := strings.Cut(trimmed, "=")
		key = strings.TrimSpace(key)
		if err := rejectDangerousWireGuardKey(key); err != nil {
			return nil, err
		}
		switch section {
		case "Interface":
			// wg-quick's macOS DNS monitor rewrites the service DNS after
			// rollback; ArchieLink owns the DNS lifecycle instead.
			if key == "DNS" {
				continue
			}
			if key == "Table" {
				continue
			}
		case "Peer":
			if key == "Endpoint" {
				lines = append(lines, "Endpoint = "+endpointKey(endpoint))
				endpointSeen = true
				continue
			}
		}
		lines = append(lines, line)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if section == "Peer" && !endpointSeen {
		return nil, errors.New("WireGuard peer has no Endpoint")
	}
	return []byte(strings.Join(lines, "\n") + "\n"), nil
}

func (a *Automatic) installDNS(ctx context.Context, service string) error {
	service = strings.TrimSpace(service)
	if service == "" {
		service = "Wi-Fi"
	}
	runner := commandRunner{runner: a.runner}
	if _, err := runner.Run(ctx, "networksetup", "-setdnsservers", service, "1.1.1.1"); err != nil {
		return fmt.Errorf("install tunnel DNS: %w", err)
	}
	_, _ = runner.Run(ctx, "dscacheutil", "-flushcache")
	_, _ = runner.Run(ctx, "killall", "-HUP", "mDNSResponder")
	return nil
}

func resolverPaths(dir string, domains []string) ([]string, error) {
	seen := map[string]struct{}{}
	result := []string{}
	for _, domain := range domains {
		domain = strings.TrimSpace(strings.TrimSuffix(domain, "."))
		if domain == "" || strings.ContainsAny(domain, `/\`) || strings.Contains(domain, "..") {
			return nil, fmt.Errorf("invalid local DNS domain: %q", domain)
		}
		path := filepath.Join(dir, domain)
		if _, ok := seen[path]; ok {
			continue
		}
		seen[path] = struct{}{}
		result = append(result, path)
	}
	return result, nil
}

func validateResolverCollision(path string) error {
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !strings.HasPrefix(string(raw), managedResolverMarker+"\n") {
		return fmt.Errorf("resolver %s exists and is not managed by archielink", path)
	}
	return nil
}

func writePrivateFile(path string, data []byte) error {
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return err
	}
	return os.Chmod(path, 0o600)
}
