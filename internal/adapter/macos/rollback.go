package macos

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/nocaptain-oss/archie-link/internal/model"
)

var ErrRollbackIncomplete = errors.New("rollback is incomplete")

func (a *Automatic) Rollback(ctx context.Context, snapshot model.NetworkSnapshot) error {
	if a.ownership == nil {
		return nil
	}
	ownership := *a.ownership
	a.ownership = nil

	runner := commandRunner{runner: a.runner}
	failures := []error{}
	ownedConfig := filepath.Join(a.options.StateDir, ownedConfigName+".conf")
	if _, err := runner.Run(ctx, "wg-quick", "down", ownedConfig); err != nil &&
		!isMissingWireguardInterface(err) {
		failures = append(failures, err)
	}
	// A daemon killed mid-flight leaves wg-quick unable to find its
	// interface; destroy it and drop the stale name/socket so the next
	// start does not collide.
	if err := a.cleanupInterfaceArtifacts(ctx, ownership.Interface); err != nil {
		failures = append(failures, err)
	}

	for _, path := range ownership.ManagedFiles {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			failures = append(failures, fmt.Errorf("remove managed file %s: %w", path, err))
		}
	}

	service := snapshot.NetworkService
	if service == "" {
		service = "Wi-Fi"
	}
	dnsServers := usableResolverServers(snapshot.DNS)
	dnsArgs := []string{"-setdnsservers", service}
	if len(dnsServers) == 0 {
		dnsArgs = append(dnsArgs, "Empty")
	} else {
		dnsArgs = append(dnsArgs, dnsServers...)
	}
	if _, err := runner.Run(ctx, "networksetup", dnsArgs...); err != nil {
		failures = append(failures, fmt.Errorf("restore DNS: %w", err))
	}
	_, _ = runner.Run(ctx, "dscacheutil", "-flushcache")
	_, _ = runner.Run(ctx, "killall", "-HUP", "mDNSResponder")

	for _, route := range ownership.ManagedRoutes {
		if _, err := runner.Run(ctx, "route", "-n", "delete", "-inet", route); err != nil &&
			!strings.Contains(strings.ToLower(err.Error()), "not in table") {
			failures = append(failures, fmt.Errorf("restore route %s: %w", route, err))
		}
	}

	if _, err := runner.Run(ctx, "scutil", "--proxy"); err != nil {
		failures = append(failures, fmt.Errorf("verify proxy state: %w", err))
	}

	routeOutput, err := a.runner.Run(ctx, "route", "-n", "get", "default")
	if err != nil {
		failures = append(failures, fmt.Errorf("verify original default route: %w", err))
	} else {
		gateway, iface := parseDefaultRoute(string(routeOutput))
		if snapshot.Gateway != "" && gateway != snapshot.Gateway {
			failures = append(failures, fmt.Errorf("default gateway is %s, want %s", gateway, snapshot.Gateway))
		}
		if snapshot.DefaultInterface != "" && iface != snapshot.DefaultInterface {
			failures = append(failures, fmt.Errorf("default interface is %s, want %s", iface, snapshot.DefaultInterface))
		}
	}

	if len(failures) > 0 {
		return errors.Join(append([]error{ErrRollbackIncomplete}, failures...)...)
	}
	return nil
}

func isMissingWireguardInterface(err error) bool {
	text := strings.ToLower(err.Error())
	return strings.Contains(text, "not found") ||
		strings.Contains(text, "is not a wireguard interface") ||
		strings.Contains(text, "no such file")
}

func (a *Automatic) cleanupInterfaceArtifacts(ctx context.Context, interfaceName string) error {
	if interfaceName == "" {
		return nil
	}
	runner := commandRunner{runner: a.runner}
	_, _ = runner.Run(ctx, "ifconfig", interfaceName, "destroy")
	_ = os.Remove(filepath.Join(a.options.WireguardRunDir, interfaceName+".sock"))
	namePath := filepath.Join(a.options.WireguardRunDir, ownedConfigName+".name")
	if raw, err := os.ReadFile(namePath); err == nil && strings.TrimSpace(string(raw)) == interfaceName {
		_ = os.Remove(namePath)
	}
	if _, err := os.Stat(namePath); err == nil {
		return fmt.Errorf("WireGuard name file remains after rollback: %s", namePath)
	}
	return nil
}
