package linux

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const resolvConfPath = "/etc/resolv.conf"

const managedResolverMarker = "# managed by archielink"

// resolvectlAvailable：resolvectl 在场且 systemd-resolved 应答中。
func (a *Automatic) resolvectlAvailable(ctx context.Context) bool {
	if _, err := a.lookPath("resolvectl"); err != nil {
		return false
	}
	_, err := commandRunner{a.runner}.Run(ctx, "resolvectl", "status")
	return err == nil
}

// installDNS 装隧道 DNS：systemd-resolved 在场时按接口下发 1.1.1.1
// 并接管全部域名（'~.'，随接口删除自清理）；否则回退 /etc/resolv.conf
// 字节级备份/恢复，备份文件入 ManagedFiles。
func (a *Automatic) installDNS(ctx context.Context) ([]string, error) {
	runner := commandRunner{a.runner}
	if a.resolvectlAvailable(ctx) {
		if _, err := runner.Run(ctx, "resolvectl", "dns", autoInterface, tunnelDNS); err != nil {
			return nil, fmt.Errorf("install tunnel DNS via resolvectl: %w", err)
		}
		if _, err := runner.Run(ctx, "resolvectl", "domain", autoInterface, "~."); err != nil {
			return nil, fmt.Errorf("route DNS domains via resolvectl: %w", err)
		}
		return nil, nil
	}
	stateDir := strings.TrimSpace(a.options.StateDir)
	if stateDir == "" {
		return nil, errors.New("automatic state directory is not configured")
	}
	original, err := os.ReadFile(a.resolvConf())
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", a.resolvConf(), err)
	}
	backupPath := filepath.Join(stateDir, "resolv.conf.backup")
	if err := writeFilePrivate(backupPath, original); err != nil {
		return nil, fmt.Errorf("back up %s: %w", a.resolvConf(), err)
	}
	managed := []byte(managedResolverMarker + "\nnameserver " + tunnelDNS + "\n")
	if err := os.WriteFile(a.resolvConf(), managed, 0o644); err != nil {
		_ = os.Remove(backupPath)
		return nil, fmt.Errorf("write %s: %w", a.resolvConf(), err)
	}
	return []string{backupPath}, nil
}

// restoreDNS 回滚 DNS：resolved 模式随接口删除自清理（尽力 revert）；
// resolv.conf 模式按备份字节级还原。
func (a *Automatic) restoreDNS(ctx context.Context) error {
	failures := []error{}
	if _, err := a.lookPath("resolvectl"); err == nil {
		// 接口删除后按接口配置已不存在；revert 的失败不视为回滚失败。
		_, _ = commandRunner{a.runner}.Run(ctx, "resolvectl", "revert", autoInterface)
	}
	stateDir := strings.TrimSpace(a.options.StateDir)
	if stateDir == "" {
		return errors.New("automatic state directory is not configured")
	}
	backupPath := filepath.Join(stateDir, "resolv.conf.backup")
	original, err := os.ReadFile(backupPath)
	if errors.Is(err, os.ErrNotExist) {
		return errors.Join(failures...)
	}
	if err != nil {
		return errors.Join(append(failures, fmt.Errorf("read DNS backup %s: %w", backupPath, err))...)
	}
	if err := os.WriteFile(a.resolvConf(), original, 0o644); err != nil {
		return errors.Join(append(failures, fmt.Errorf("restore %s: %w", a.resolvConf(), err))...)
	}
	if err := os.Remove(backupPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		failures = append(failures, fmt.Errorf("remove DNS backup %s: %w", backupPath, err))
	}
	return errors.Join(failures...)
}
