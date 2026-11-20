package linux

import (
	"context"
	"net"
	"os/exec"
	"strings"

	"github.com/nocaptain-oss/archie-link/internal/adapter"
	"github.com/nocaptain-oss/archie-link/internal/coexist"
	"github.com/nocaptain-oss/archie-link/internal/model"
)

const (
	autoInterface  = "archieauto"
	probeInterface = "archieprobe"
	// 与 wg-quick(8) 相同的基准表号，占用时向上自增。
	baseRoutingTable = 51820
	// 策略路由规则显式 priority，低于 main(32766)，先 suppress 后 not-fwmark。
	mainSuppressRulePriority = 30500
	notFwmarkRulePriority   = 30501
	defaultMTU              = 1420
	tunnelDNS               = "1.1.1.1"
)

type AutomaticOptions struct {
	ConfigPath string
	StateDir   string
	// ResolvConfPath 覆盖 /etc/resolv.conf 路径；测试注入用。
	ResolvConfPath string
	// ResolveIPv4 解析保护域名；测试注入用。
	ResolveIPv4 func(context.Context, string) ([]string, error)
	// LookPath 探测工具在场性；测试注入用。
	LookPath func(string) (string, error)
}

// resolvConf 返回生效的 resolv.conf 路径。
func (a *Automatic) resolvConf() string {
	if path := strings.TrimSpace(a.options.ResolvConfPath); path != "" {
		return path
	}
	return resolvConfPath
}

// Automatic 是 Linux 的 app.Automatic 实现：kernel WireGuard 优先、
// wireguard-go 用户态回退，路由走 wg-quick 式策略路由，主表默认路由不动。
type Automatic struct {
	runner    adapter.Runner
	options   AutomaticOptions
	ownership *model.Ownership
	lookPath  func(string) (string, error)
}

func NewAutomatic(runner adapter.Runner, options AutomaticOptions) *Automatic {
	if options.ResolveIPv4 == nil {
		options.ResolveIPv4 = lookupIPv4
	}
	lookPath := options.LookPath
	if lookPath == nil {
		lookPath = exec.LookPath
	}
	return &Automatic{runner: runner, options: options, lookPath: lookPath}
}

func lookupIPv4(ctx context.Context, host string) ([]string, error) {
	addresses, err := net.DefaultResolver.LookupIP(ctx, "ip4", host)
	if err != nil {
		return nil, err
	}
	result := []string{}
	for _, address := range addresses {
		if ipv4 := address.To4(); ipv4 != nil {
			result = append(result, ipv4.String())
		}
	}
	return result, nil
}

func (*Automatic) Name() string {
	return "linux"
}

func (a *Automatic) Detect(ctx context.Context) (model.PlatformReport, model.CapabilityReport, error) {
	adapter := New(a.runner)
	adapter.lookPath = a.lookPath
	return adapter.Detect(ctx)
}

func (a *Automatic) Coexistence(ctx context.Context) (coexist.Profile, error) {
	return New(a.runner).Coexistence(ctx)
}

func (a *Automatic) Capture(ctx context.Context) (model.NetworkSnapshot, error) {
	network, err := New(a.runner).Snapshot(ctx)
	if err != nil {
		return model.NetworkSnapshot{}, err
	}
	return model.NetworkSnapshot{
		SchemaVersion:    model.SchemaVersion,
		DefaultInterface: network.DefaultInterface,
		Gateway:          network.Gateway,
		DNS:              append([]string(nil), network.DNS...),
		ResolverDNS:      append([]string(nil), network.DNS...),
		ProxyEnabled:     network.ProxyEnabled,
		ExistingTunnels:  append([]string(nil), network.ExistingTunnels...),
		ManagedFiles:     []string{},
	}, nil
}

func (a *Automatic) Resume(ownership model.Ownership) {
	copy := ownership
	a.ownership = &copy
}

func (a *Automatic) resolveDirectHosts(ctx context.Context, domains []string) ([]string, error) {
	result := []string{}
	seen := map[string]struct{}{}
	for _, domain := range domains {
		addresses, err := a.options.ResolveIPv4(ctx, domain)
		if err != nil {
			return nil, err
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
