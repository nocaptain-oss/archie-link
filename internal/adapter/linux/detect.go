package linux

import (
	"context"
	"strings"

	"github.com/nocaptain-oss/archie-link/internal/model"
)

func (a *Adapter) Detect(ctx context.Context) (model.PlatformReport, model.CapabilityReport, error) {
	osRelease, err := a.runText(ctx, "cat", "/etc/os-release")
	if err != nil {
		return model.PlatformReport{}, model.CapabilityReport{}, err
	}
	arch, err := a.runText(ctx, "uname", "-m")
	if err != nil {
		return model.PlatformReport{}, model.CapabilityReport{}, err
	}
	uid, err := a.runText(ctx, "id", "-u")
	if err != nil {
		return model.PlatformReport{}, model.CapabilityReport{}, err
	}
	// Ubuntu/Debian 等主流发行版没有 command 二进制（command 是 shell 内建），
	// 不能经 runner 执行 `command -v`；用 Go 原生 LookPath 探测工具在场性。
	// （V0.5 期间实测：EulerOS 恰带 /usr/bin/command shim 故旧写法碰巧可用，
	// 标准 Ubuntu 上四项探测全空，network_mutation 被漏报为 false。）
	sudo, _ := a.lookPath("sudo")
	ipCommand, _ := a.lookPath("ip")
	wgCommand, _ := a.lookPath("wg")
	wireguardGo, _ := a.lookPath("wireguard-go")

	elevationAvailable := uid == "0" || sudo != ""
	wireGuardAvailable := wgCommand != "" || wireguardGo != ""

	return model.PlatformReport{
		OS:      "linux",
		Version: parseOSVersion(osRelease),
		Arch:    arch,
	}, model.CapabilityReport{
		Shell:           true,
		Elevation:       elevationAvailable,
		NetworkMutation: elevationAvailable && ipCommand != "" && wireGuardAvailable,
		GUI:             false,
	}, nil
}

func parseOSVersion(output string) string {
	values := map[string]string{}
	for _, line := range strings.Split(output, "\n") {
		key, value, ok := strings.Cut(strings.TrimSpace(line), "=")
		if !ok {
			continue
		}
		values[key] = strings.Trim(value, `"'`)
	}
	if values["VERSION_ID"] != "" {
		return values["VERSION_ID"]
	}
	return values["ID"]
}
