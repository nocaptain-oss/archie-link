//go:build !licensetest

package cli

import (
	"crypto/ed25519"
	"errors"

	"github.com/nocaptain-oss/archie-link/internal/entitlement"
)

func maybeTestEntitlement(string) (entitlement.Provider, bool, error) {
	return nil, false, nil
}

func runtimePublicKeyOverride() (string, bool) {
	return "", false
}

func decodePublicKey(string) (ed25519.PublicKey, error) {
	return nil, errors.New("development public key is unavailable in release builds")
}
