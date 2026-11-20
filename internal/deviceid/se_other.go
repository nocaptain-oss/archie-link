//go:build !darwin || !cgo

package deviceid

import "context"

// SecureEnclaveStore 在非 darwin 或未启用 cgo 时不可用；EnsureKey 恒返回错误，
// Manager 必须回退到常规 SecretStore。
type SecureEnclaveStore struct{}

func (SecureEnclaveStore) EnsureKey(ctx context.Context, installID string) (Identity, error) {
	return Identity{}, ErrEnclaveUnavailable
}

func (SecureEnclaveStore) Sign(ctx context.Context, installID string, challenge []byte) ([]byte, error) {
	return nil, ErrEnclaveUnavailable
}
