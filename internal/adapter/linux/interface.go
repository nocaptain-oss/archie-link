package linux

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"
)

// createWireguardInterface 建 WireGuard 接口：kernel 优先，失败回退
// wireguard-go 用户态（守护化，随接口删除退出）。
// 返回 true 表示用户态模式。
// Debian/Ubuntu 的 wireguard-go 包把二进制装为 /usr/bin/wireguard，
// 其他发行版多为 /usr/bin/wireguard-go，两个名字都试。
func (a *Automatic) createWireguardInterface(ctx context.Context, name string) (bool, error) {
	runner := commandRunner{a.runner}
	if _, err := runner.Run(ctx, "ip", "link", "add", name, "type", "wireguard"); err == nil {
		return false, nil
	} else if !isKernelWireguardUnavailable(err) {
		return false, fmt.Errorf("create kernel WireGuard interface: %w", err)
	}
	userspaceErr := error(nil)
	for _, helper := range []string{"wireguard-go", "wireguard"} {
		if _, err := runner.Run(ctx, helper, name); err != nil {
			userspaceErr = err
			continue
		}
		if err := a.waitForUserspaceReady(ctx, name, 3*time.Second); err != nil {
			return true, err
		}
		return true, nil
	}
	return false, fmt.Errorf("create userspace WireGuard interface: %w", userspaceErr)
}

// isKernelWireguardUnavailable 判定 kernel 模式不可建（模块缺失/无权限），
// 此时才允许回退 wireguard-go；其他错误如实上抛。
func isKernelWireguardUnavailable(err error) bool {
	text := strings.ToLower(err.Error())
	return strings.Contains(text, "operation not supported") ||
		strings.Contains(text, "operation not permitted") ||
		strings.Contains(text, "protocol not supported")
}

// waitForUserspaceReady 等待 wireguard-go 的 UAPI 套接字就绪，
// 否则紧随其后的 wg setconf 会找不到接口。
func (a *Automatic) waitForUserspaceReady(ctx context.Context, name string, timeout time.Duration) error {
	runner := commandRunner{a.runner}
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if _, err := runner.Run(ctx, "wg", "show", name); err == nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
	return fmt.Errorf("userspace WireGuard interface %s did not become ready", name)
}

// teardownWireguardInterface 删接口（连带用户态进程退出）并确认消失。
func (a *Automatic) teardownWireguardInterface(ctx context.Context, name string, userspace bool) error {
	runner := commandRunner{a.runner}
	if name == "" {
		return nil
	}
	_, _ = runner.Run(ctx, "ip", "link", "del", name)
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := runner.Run(ctx, "ip", "link", "show", name); err != nil {
			return a.stopUserspaceProcess(ctx, name, userspace)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
	return fmt.Errorf("WireGuard interface %s remains after teardown", name)
}

// stopUserspaceProcess 兜底确认 wireguard-go 进程退出；
// wireguard-go 监听接口删除事件自行退出，超时才按精确模式补刀。
// 模式串同时覆盖 wireguard-go 与 Debian/Ubuntu 的 wireguard 两种二进制名。
func (a *Automatic) stopUserspaceProcess(ctx context.Context, name string, userspace bool) error {
	if !userspace {
		return nil
	}
	pattern := "wireguard(-go)? " + name + "($| )"
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		output, err := a.runner.Run(ctx, "pgrep", "-f", pattern)
		if err != nil || strings.TrimSpace(string(output)) == "" {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
	_, _ = commandRunner{a.runner}.Run(ctx, "pkill", "-f", pattern)
	return nil
}

func writeFilePrivate(path string, data []byte) error {
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return err
	}
	return os.Chmod(path, 0o600)
}
