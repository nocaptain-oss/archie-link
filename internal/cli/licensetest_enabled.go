//go:build licensetest

package cli

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"os"
	"strings"

	"github.com/nocaptain-oss/archie-link/internal/entitlement"
)

func maybeTestEntitlement(installID string) (entitlement.Provider, bool, error) {
	if os.Getenv("ARCHIE_LINK_TEST_LICENSE") != "1" {
		return nil, false, nil
	}
	provider := entitlement.NewTestProvider(func(value *entitlement.Entitlement) {
		value.InstallID = installID
	})
	return provider, true, nil
}

func runtimePublicKeyOverride() (string, bool) {
	value := strings.TrimSpace(os.Getenv("ARCHIE_LINK_PUBLIC_KEY"))
	return value, value != ""
}

func decodePublicKey(value string) (ed25519.PublicKey, error) {
	if raw, err := base64.StdEncoding.DecodeString(value); err == nil && len(raw) == ed25519.PublicKeySize {
		return ed25519.PublicKey(raw), nil
	}
	if raw, err := hex.DecodeString(value); err == nil && len(raw) == ed25519.PublicKeySize {
		return ed25519.PublicKey(raw), nil
	}
	return nil, errors.New("ARCHIE_LINK_PUBLIC_KEY must be a base64 or hex Ed25519 public key")
}
