//go:build linux

package cli

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/nocaptain-oss/archie-link/internal/adapter"
	"github.com/nocaptain-oss/archie-link/internal/adapter/linux"
	"github.com/nocaptain-oss/archie-link/internal/entitlement"
	appstate "github.com/nocaptain-oss/archie-link/internal/state"
)

// Linux 生命周期装配（V0.5 切片 3）：
// 状态目录 ARCHIE_LINK_STATE_DIR > $XDG_STATE_HOME/ArchieLink > ~/.local/state/ArchieLink；
// 配置只认 ARCHIE_LINK_WG_CONFIG；Automatic 为 kernel WireGuard 优先、
// wireguard-go 用户态回退（docs/platform/LINUX.md 切片 3 节）。
func defaultLifecycleDependencies() (*LifecycleDependencies, error) {
	home, err := effectiveHome()
	if err != nil {
		return nil, err
	}
	stateDir := strings.TrimSpace(os.Getenv("ARCHIE_LINK_STATE_DIR"))
	if stateDir == "" {
		xdgState := strings.TrimSpace(os.Getenv("XDG_STATE_HOME"))
		if xdgState != "" {
			stateDir = filepath.Join(xdgState, "ArchieLink")
		} else {
			stateDir = filepath.Join(home, ".local", "state", "ArchieLink")
		}
	}
	store := appstate.Store{Dir: stateDir}
	// sudo 运行时把状态文件归还给发起用户，与 macOS 行为一致。
	if uid, uidErr := strconv.Atoi(os.Getenv("SUDO_UID")); uidErr == nil {
		if gid, gidErr := strconv.Atoi(os.Getenv("SUDO_GID")); gidErr == nil {
			store.OwnerUID = uid
			store.OwnerGID = gid
			store.SetOwnership = true
		}
	}
	installID, err := store.LoadOrCreateInstallID()
	if err != nil {
		return nil, err
	}
	configPath := strings.TrimSpace(os.Getenv("ARCHIE_LINK_WG_CONFIG"))
	provider, err := lifecycleEntitlementLinux(installID)
	if err != nil {
		return nil, err
	}
	automatic := linux.NewAutomatic(adapter.ExecRunner{}, linux.AutomaticOptions{
		ConfigPath: configPath,
		StateDir:   stateDir,
	})
	return &LifecycleDependencies{
		Automatic:   automatic,
		Entitlement: provider,
		Store:       store,
		InstallID:   installID,
		ConfigPath:  configPath,
		StateDir:    stateDir,
	}, nil
}

// lifecycleEntitlementLinux Linux 仅支持测试授权与签名授权；
// 设备级租约绑定依赖 deviceid 的 Linux 实现，延后（见 LINUX.md 已知限制）。
func lifecycleEntitlementLinux(installID string) (entitlement.Provider, error) {
	if provider, handled, err := maybeTestEntitlement(installID); handled {
		return provider, err
	}
	licensePath := strings.TrimSpace(os.Getenv("ARCHIE_LINK_LICENSE_FILE"))
	if licensePath == "" {
		return nil, nil
	}
	data, err := os.ReadFile(licensePath)
	if err != nil {
		return nil, err
	}
	publicKeyText, overrideEnabled := runtimePublicKeyOverride()
	if overrideEnabled {
		if publicKeyText == "" {
			return nil, errors.New("ARCHIE_LINK_PUBLIC_KEY is required in development license builds")
		}
		publicKey, err := decodePublicKey(publicKeyText)
		if err != nil {
			return nil, err
		}
		return entitlement.SignedProvider{Data: data, PublicKey: publicKey}, nil
	}
	root, err := entitlement.EmbeddedTrustRoot()
	if err != nil {
		return nil, err
	}
	return entitlement.PinnedSignedProvider{Data: data, TrustRoot: root}, nil
}
