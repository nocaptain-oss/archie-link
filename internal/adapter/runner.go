package adapter

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

var ErrWriteCommandBlocked = errors.New("network write command blocked")

type Runner interface {
	Run(context.Context, string, ...string) ([]byte, error)
}

type ExecRunner struct{}

func (ExecRunner) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, args...).CombinedOutput()
}

type GuardRunner struct {
	Next Runner
}

func (g GuardRunner) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	command := strings.ToLower(strings.TrimSpace(strings.Join(append([]string{name}, args...), " ")))
	if command == "netsh winhttp show proxy" || command == "iptables-save" || command == "nft list ruleset" {
		return g.Next.Run(ctx, name, args...)
	}
	for _, forbidden := range forbiddenWritePrefixes {
		if strings.Contains(command, forbidden) {
			return nil, fmt.Errorf("%w: %s", ErrWriteCommandBlocked, command)
		}
	}
	return g.Next.Run(ctx, name, args...)
}

var forbiddenWritePrefixes = []string{
	"route add",
	"route -q -n add",
	"networksetup -setdnsservers",
	"wg-quick up",
	"iptables",
	"nft add",
	"nft delete",
	"nft flush",
	"ip route add",
	"ip route del",
	"ip route delete",
	"resolvectl dns",
	"nmcli connection modify",
	"systemctl start",
	"systemctl stop",
	"systemctl restart",
	"new-netroute",
	"remove-netroute",
	"set-dnsclientserveraddress",
	"netsh",
}
