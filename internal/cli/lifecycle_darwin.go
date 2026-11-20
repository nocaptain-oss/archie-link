//go:build darwin

package cli

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/nocaptain-oss/archie-link/internal/adapter"
	"github.com/nocaptain-oss/archie-link/internal/adapter/macos"
	"github.com/nocaptain-oss/archie-link/internal/deviceid"
	"github.com/nocaptain-oss/archie-link/internal/entitlement"
	appstate "github.com/nocaptain-oss/archie-link/internal/state"
)

func defaultLifecycleDependencies() (*LifecycleDependencies, error) {
	home, err := effectiveHome()
	if err != nil {
		return nil, err
	}
	stateDir := strings.TrimSpace(os.Getenv("ARCHIE_LINK_STATE_DIR"))
	if stateDir == "" {
		stateDir = filepath.Join(home, "Library", "Application Support", "ArchieLink")
	}
	store := appstate.Store{Dir: stateDir}
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
	if configPath == "" {
		configPath = filepath.Join(home, "Library", "Application Support", "CloudflareWARP", "warp.conf")
	}
	provider, err := lifecycleEntitlement(installID, store)
	if err != nil {
		return nil, err
	}
	automatic := macos.NewAutomatic(adapter.ExecRunner{}, macos.AutomaticOptions{
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

func lifecycleEntitlement(installID string, store appstate.Store) (entitlement.Provider, error) {
	if provider, handled, err := maybeTestEntitlement(installID); handled {
		return provider, err
	}
	licensePath := strings.TrimSpace(os.Getenv("ARCHIE_LINK_LICENSE_FILE"))
	publicKeyText, overrideEnabled := runtimePublicKeyOverride()
	if licensePath == "" {
		return nil, nil
	}
	data, err := os.ReadFile(licensePath)
	if err != nil {
		return nil, err
	}
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
	// Secure Enclave 可用时私钥不可导出；不可用时自动回退 Keychain 软件密钥。
	// 身份字段如实标记 secure_enclave，未接入硬件时不得对外宣称使用了 SE。
	device := deviceid.NewWithEnclave(deviceid.KeychainStore{}, installID, deviceid.SecureEnclaveStore{})
	leaseState, err := store.LoadLeaseState()
	if err != nil {
		return nil, err
	}
	return entitlement.LeaseFileProvider{
		Data: data, TrustRoot: root, Device: device,
		RevocationFloor:     leaseState.RevocationFloor,
		LastSeenAt:          leaseState.LastSeenAt,
		ClockReviewRequired: leaseState.ClockReviewRequired,
		RecordSeen: func(now time.Time) error {
			current, err := store.LoadLeaseState()
			if err != nil {
				return err
			}
			rollback := current.ClockReviewRequired || entitlement.ClockRollback(current.LastSeenAt, now)
			current = current.Observe(now)
			current.ClockReviewRequired = rollback
			if err := store.SaveLeaseState(current); err != nil {
				return err
			}
			if rollback {
				return entitlement.ErrClockRollback
			}
			return nil
		},
	}, nil
}
