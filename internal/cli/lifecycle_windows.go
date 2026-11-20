//go:build windows

package cli

import (
	"os"
	"path/filepath"
	"strings"

	appstate "github.com/nocaptain-oss/archie-link/internal/state"
)

// Windows 引导模式装配（V0.5 切片 1）：只提供状态目录与用户提供的配置路径，
// 不装配 Automatic/Entitlement；生命周期命令输出结构化引导报告（exit 3）。
func defaultLifecycleDependencies() (*LifecycleDependencies, error) {
	home, err := effectiveHome()
	if err != nil {
		return nil, err
	}
	stateDir := strings.TrimSpace(os.Getenv("ARCHIE_LINK_STATE_DIR"))
	if stateDir == "" {
		base := strings.TrimSpace(os.Getenv("LOCALAPPDATA"))
		if base == "" {
			base = filepath.Join(home, "AppData", "Local")
		}
		stateDir = filepath.Join(base, "ArchieLink")
	}
	store := appstate.Store{Dir: stateDir}
	installID, err := store.LoadOrCreateInstallID()
	if err != nil {
		return nil, err
	}
	return &LifecycleDependencies{
		Store:      store,
		InstallID:  installID,
		ConfigPath: strings.TrimSpace(os.Getenv("ARCHIE_LINK_WG_CONFIG")),
		StateDir:   stateDir,
	}, nil
}
