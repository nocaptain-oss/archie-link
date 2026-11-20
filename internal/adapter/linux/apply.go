package linux

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/nocaptain-oss/archie-link/internal/model"
)

// systemIPv6Enabled 通过 /proc/net/if_inet6 判断当前网络命名空间是否启用 IPv6。
func systemIPv6Enabled() bool {
	data, err := os.ReadFile("/proc/net/if_inet6")
	return err == nil && len(strings.TrimSpace(string(data))) > 0
}

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
	stateDir := strings.TrimSpace(a.options.StateDir)
	if stateDir == "" {
		return model.Ownership{}, errors.New("automatic state directory is not configured")
	}
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		return model.Ownership{}, err
	}
	if err := os.Chmod(stateDir, 0o700); err != nil {
		return model.Ownership{}, err
	}

	source, err := os.ReadFile(configPath)
	if err != nil {
		return model.Ownership{}, fmt.Errorf("read WireGuard config: %w", err)
	}
	setconf, meta, err := buildSetConf(source, plan.Endpoint, 0)
	if err != nil {
		return model.Ownership{}, err
	}
	setconfPath := filepath.Join(stateDir, autoInterface+".setconf")
	if err := writeFilePrivate(setconfPath, setconf); err != nil {
		return model.Ownership{}, err
	}
	managedFiles := []string{setconfPath}
	cleanupFiles := func() {
		for _, path := range managedFiles {
			_ = os.Remove(path)
		}
	}
	runner := commandRunner{a.runner}

	userspace, err := a.createWireguardInterface(ctx, autoInterface)
	if err != nil {
		cleanupFiles()
		return model.Ownership{}, err
	}
	// 失败清理：删已装策略路由/保护路由 → 拆接口（连带用户态进程）→ 删受管文件。
	// 规则不随接口删除，必须显式回滚，否则残留 pref 30500/30501 会被共存 Gate 拦截。
	managedRoutes := []string{}
	fail := func(err error) (model.Ownership, error) {
		_ = a.removeRouteEntries(context.WithoutCancel(ctx), runner, managedRoutes)
		_ = a.teardownWireguardInterface(context.WithoutCancel(ctx), autoInterface, userspace)
		cleanupFiles()
		return model.Ownership{}, err
	}

	if _, err := runner.Run(ctx, "wg", "setconf", autoInterface, setconfPath); err != nil {
		return fail(fmt.Errorf("configure WireGuard interface: %w", err))
	}
	// IPv6 全禁的系统（docker 默认、大量 VPS）分配 v6 地址会 EACCES；
	// 系统未启用 IPv6 时跳过 v6 隧道地址，v4 照常。
	ipv6Enabled := systemIPv6Enabled()
	for _, address := range meta.Addresses {
		if strings.Contains(address, ":") && !ipv6Enabled {
			continue
		}
		if _, err := runner.Run(ctx, "ip", "address", "add", address, "dev", autoInterface); err != nil &&
			!isAlreadyExists(err) {
			return fail(fmt.Errorf("assign tunnel address %s: %w", address, err))
		}
	}
	mtu := meta.MTU
	if mtu <= 0 {
		mtu = defaultMTU
	}
	if _, err := runner.Run(ctx, "ip", "link", "set", "dev", autoInterface, "mtu",
		strconv.Itoa(mtu), "up"); err != nil {
		return fail(fmt.Errorf("bring WireGuard interface up: %w", err))
	}

	routes, err := a.applyRouting(ctx, plan, meta)
	managedRoutes = routes
	if err != nil {
		return fail(err)
	}
	if !waitForHandshake(ctx, a.runner, autoInterface, 10*time.Second, 200*time.Millisecond) {
		return fail(fmt.Errorf("WireGuard handshake did not complete on %s", autoInterface))
	}

	directHosts, err := a.resolveDirectHosts(ctx, plan.LocalDNSDomains)
	if err != nil {
		return fail(err)
	}
	routes, err = a.pinDirectRoutes(ctx, runner, plan, directHosts, managedRoutes)
	managedRoutes = routes
	if err != nil {
		return fail(err)
	}

	dnsFiles, err := a.installDNS(ctx)
	if err != nil {
		return fail(err)
	}
	managedFiles = append(managedFiles, dnsFiles...)

	ownership := model.Ownership{
		Interface:     autoInterface,
		Endpoint:      plan.Endpoint,
		ManagedFiles:  append([]string(nil), managedFiles...),
		ManagedRoutes: managedRoutes,
		DirectHosts:   directHosts,
	}
	a.ownership = &ownership
	return ownership, nil
}

// applyRouting 装策略路由：AllowedIPs 含 0.0.0.0/0 走 wg-quick 式
// fwmark + 独立表 + 两条显式 priority 规则（主表默认路由不动）；
// 拆分隧道时按 AllowedIPs 在主表装 dev 路由（随接口删除自动清理）。
func (a *Automatic) applyRouting(ctx context.Context, plan model.ApplyPlan, meta configMeta) ([]string, error) {
	runner := commandRunner{a.runner}
	managed := []string{}
	if !meta.hasDefaultRoute() {
		for _, cidr := range meta.AllowedIPs {
			if _, err := runner.Run(ctx, "ip", "route", "add", cidr, "dev", autoInterface); err != nil &&
				!isAlreadyExists(err) {
				return managed, fmt.Errorf("install split route %s: %w", cidr, err)
			}
			managed = append(managed, "route:main:"+cidr)
		}
		return managed, nil
	}
	table, err := a.pickRoutingTable(ctx)
	if err != nil {
		return managed, err
	}
	if _, err := runner.Run(ctx, "wg", "set", autoInterface, "fwmark", strconv.Itoa(table)); err != nil {
		return managed, fmt.Errorf("set WireGuard fwmark: %w", err)
	}
	managed = append(managed, "table:"+strconv.Itoa(table))
	if _, err := runner.Run(ctx, "ip", "route", "add", "0.0.0.0/0", "dev", autoInterface,
		"table", strconv.Itoa(table)); err != nil && !isAlreadyExists(err) {
		return managed, fmt.Errorf("install tunnel default route: %w", err)
	}
	managed = append(managed, "route:"+strconv.Itoa(table)+":0.0.0.0/0")
	if _, err := runner.Run(ctx, "ip", "rule", "add", "pref", strconv.Itoa(mainSuppressRulePriority),
		"table", "main", "suppress_prefixlength", "0"); err != nil && !isAlreadyExists(err) {
		return managed, fmt.Errorf("install main suppress rule: %w", err)
	}
	managed = append(managed, "rule:"+strconv.Itoa(mainSuppressRulePriority))
	if _, err := runner.Run(ctx, "ip", "rule", "add", "pref", strconv.Itoa(notFwmarkRulePriority),
		"not", "fwmark", strconv.Itoa(table), "table", strconv.Itoa(table)); err != nil && !isAlreadyExists(err) {
		return managed, fmt.Errorf("install tunnel policy rule: %w", err)
	}
	managed = append(managed, "rule:"+strconv.Itoa(notFwmarkRulePriority))
	// IPv6：系统有 v6 默认路由且 AllowedIPs 含 ::/0 才接管，
	// 防止 v4 全隧道时 v6 明文绕行。
	if meta.hasIPv6DefaultRoute() && a.hasIPv6DefaultRoute(ctx) {
		if _, err := runner.Run(ctx, "ip", "-6", "route", "add", "::/0", "dev", autoInterface,
			"table", strconv.Itoa(table)); err != nil && !isAlreadyExists(err) {
			return managed, fmt.Errorf("install IPv6 tunnel default route: %w", err)
		}
		managed = append(managed, "route:"+strconv.Itoa(table)+":::/0")
		if _, err := runner.Run(ctx, "ip", "-6", "rule", "add", "pref",
			strconv.Itoa(mainSuppressRulePriority), "table", "main", "suppress_prefixlength", "0"); err != nil &&
			!isAlreadyExists(err) {
			return managed, fmt.Errorf("install IPv6 main suppress rule: %w", err)
		}
		managed = append(managed, "rule6:"+strconv.Itoa(mainSuppressRulePriority))
		if _, err := runner.Run(ctx, "ip", "-6", "rule", "add", "pref",
			strconv.Itoa(notFwmarkRulePriority), "not", "fwmark", strconv.Itoa(table),
			"table", strconv.Itoa(table)); err != nil && !isAlreadyExists(err) {
			return managed, fmt.Errorf("install IPv6 tunnel policy rule: %w", err)
		}
		managed = append(managed, "rule6:"+strconv.Itoa(notFwmarkRulePriority))
	}
	return managed, nil
}

// pinDirectRoutes 在主表装保护路由：端点 /32、AlwaysDirectCIDRs、
// 解析后的保护域名主机，全部经原网关/原接口直连，不经隧道。
// 返回追加后的 managedRoutes。
func (a *Automatic) pinDirectRoutes(
	ctx context.Context,
	runner commandRunner,
	plan model.ApplyPlan,
	directHosts []string,
	managedRoutes []string,
) ([]string, error) {
	pin := func(destination string) error {
		args := []string{"route", "add", destination}
		switch {
		case plan.Gateway != "":
			args = append(args, "via", plan.Gateway)
		case plan.DefaultInterface != "":
			args = append(args, "dev", plan.DefaultInterface)
		default:
			return fmt.Errorf("protected route %s needs a gateway or interface", destination)
		}
		if _, err := runner.Run(ctx, "ip", args...); err != nil && !isAlreadyExists(err) {
			return fmt.Errorf("install protected route %s: %w", destination, err)
		}
		managedRoutes = append(managedRoutes, "route:main:"+destination)
		return nil
	}
	if plan.Endpoint.Address != "" {
		if err := pin(plan.Endpoint.Address + "/32"); err != nil {
			return managedRoutes, err
		}
	}
	for _, cidr := range plan.AlwaysDirectCIDRs {
		if err := pin(cidr); err != nil {
			return managedRoutes, err
		}
	}
	for _, host := range directHosts {
		if err := pin(host + "/32"); err != nil {
			return managedRoutes, err
		}
	}
	return managedRoutes, nil
}

func (a *Automatic) pickRoutingTable(ctx context.Context) (int, error) {
	runner := commandRunner{a.runner}
	table := baseRoutingTable
	for i := 0; i < 100; i++ {
		output, err := runner.Run(ctx, "ip", "-4", "route", "show", "table", strconv.Itoa(table))
		if err != nil {
			// 部分内核（如 Huawei Cloud EulerOS 5.10）查询未创建的表
			// 报 "FIB table does not exist"（exit 2），等价于空表可用。
			if strings.Contains(strings.ToLower(err.Error()), "does not exist") {
				return table, nil
			}
			return 0, fmt.Errorf("inspect routing table %d: %w", table, err)
		}
		if strings.TrimSpace(string(output)) == "" {
			return table, nil
		}
		table++
	}
	return 0, errors.New("no free routing table within range")
}

func (a *Automatic) hasIPv6DefaultRoute(ctx context.Context) bool {
	output, err := a.runner.Run(ctx, "ip", "-6", "route", "show", "default")
	return err == nil && strings.TrimSpace(string(output)) != ""
}

func isAlreadyExists(err error) bool {
	if err == nil {
		return false
	}
	text := strings.ToLower(err.Error())
	return strings.Contains(text, "file exists") ||
		strings.Contains(text, "already exists") ||
		strings.Contains(text, "exists")
}
