//go:build !darwin

package deviceid

import (
	"context"
	"errors"
)

type KeychainStore struct {
	Service      string
	KeychainPath string
}

func (KeychainStore) Load(context.Context, string) (string, error) {
	return "", errors.New("system keychain device identity is supported only on macOS")
}

func (KeychainStore) Save(context.Context, string, string) error {
	return errors.New("system keychain device identity is supported only on macOS")
}
