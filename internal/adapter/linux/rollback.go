package linux

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/nocaptain-oss/archie-link/internal/model"
)

var ErrRollbackIncomplete = errors.New("rollback is incomplete")

// Rollback 撤销自动模式对系统网络的全部修改：
// 策略规则 → 接口（连带用户态进程）→ 主表保护路由 → DNS → 受管文件，
// 最后校验默认路由与快照一致；任一步失败聚合为 rollback_incomplete。
func (a *Automatic) Rollback(ctx context.Context, snapshot model.NetworkSnapshot) error {
	if a.ownership == nil {
		return nil
	}
	ownership := *a.ownership
	a.ownership = nil

	runner := commandRunner{a.runner}
	failures := []error{}

	failures = append(failures, a.removeRouteEntries(ctx, runner, ownership.ManagedRoutes)...)

	if err := a.teardownWireguardInterface(ctx, ownership.Interface, true); err != nil {
		failures = append(failures, err)
	}

	if err := a.restoreDNS(ctx); err != nil {
		failures = append(failures, err)
	}

	for _, path := range ownership.ManagedFiles {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			failures = append(failures, fmt.Errorf("remove managed file %s: %w", path, err))
		}
	}

	routeOutput, err := a.runner.Run(ctx, "ip", "route", "show", "default")
	if err != nil {
		failures = append(failures, fmt.Errorf("verify default route: %w", err))
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

// removeRouteEntries 按 Ownership.ManagedRoutes 编码逐条删除：
// rule:<pref> / rule6:<pref> 删策略规则，route:main:<dst> 删主表保护路由；
// table: / route:<table>: 条目随接口删除自动失效，无需处理。
// Apply 失败清理与正式 Rollback 共用。
func (a *Automatic) removeRouteEntries(ctx context.Context, runner commandRunner, entries []string) []error {
	failures := []error{}
	for _, entry := range entries {
		switch {
		case strings.HasPrefix(entry, "rule:"):
			priority := strings.TrimPrefix(entry, "rule:")
			if _, err := runner.Run(ctx, "ip", "rule", "del", "pref", priority); err != nil &&
				!isMissingRouteState(err) {
				failures = append(failures, fmt.Errorf("remove policy rule %s: %w", priority, err))
			}
		case strings.HasPrefix(entry, "rule6:"):
			priority := strings.TrimPrefix(entry, "rule6:")
			if _, err := runner.Run(ctx, "ip", "-6", "rule", "del", "pref", priority); err != nil &&
				!isMissingRouteState(err) {
				failures = append(failures, fmt.Errorf("remove IPv6 policy rule %s: %w", priority, err))
			}
		case strings.HasPrefix(entry, "route:main:"):
			destination := strings.TrimPrefix(entry, "route:main:")
			args := []string{"route", "del", destination}
			if strings.Contains(destination, ":") {
				args = append([]string{"-6"}, args...)
			}
			if _, err := runner.Run(ctx, "ip", args...); err != nil &&
				!isMissingRouteState(err) {
				failures = append(failures, fmt.Errorf("remove protected route %s: %w", destination, err))
			}
		}
	}
	return failures
}

func isMissingRouteState(err error) bool {
	if err == nil {
		return false
	}
	text := strings.ToLower(err.Error())
	return strings.Contains(text, "no such") ||
		strings.Contains(text, "does not exist") ||
		strings.Contains(text, "not found") ||
		strings.Contains(text, "invalid argument")
}
