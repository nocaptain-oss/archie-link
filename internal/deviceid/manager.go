package deviceid

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
)

var (
	ErrIdentityMismatch   = errors.New("device identity belongs to another installation")
	ErrSecretNotFound     = errors.New("device identity secret was not found")
	ErrEnclaveUnavailable = errors.New("secure enclave signer is unavailable on this device")
)

type Identity struct {
	InstallID     string `json:"install_id"`
	PublicKey     string `json:"public_key"`
	KeyID         string `json:"key_id"`
	SecureEnclave bool   `json:"secure_enclave"`
	KeyFormat     string `json:"key_format"`
}

type storedIdentity struct {
	Identity
	PrivateKey string `json:"private_key"`
}

type SecretStore interface {
	Load(context.Context, string) (string, error)
	Save(context.Context, string, string) error
}

type Manager struct {
	store     SecretStore
	installID string
	enclave   EnclaveBackend
}

func New(store SecretStore, installID string) *Manager {
	return &Manager{store: store, installID: installID}
}

// NewWithEnclave 在常规 SecretStore 之外接入安全硬件签名后端。
// enclave 创建失败时 LoadOrCreate 自动回退 Keychain 软件密钥；
// 回退后的身份如实标记 SecureEnclave=false，不得对外宣称使用了 Secure Enclave。
func NewWithEnclave(store SecretStore, installID string, enclave EnclaveBackend) *Manager {
	return &Manager{store: store, installID: installID, enclave: enclave}
}

func (m *Manager) LoadOrCreate(ctx context.Context) (Identity, error) {
	secret, err := m.store.Load(ctx, m.installID)
	if err == nil {
		stored, parseErr := decodeSecret(secret)
		if parseErr != nil {
			return Identity{}, parseErr
		}
		if stored.InstallID != m.installID {
			return Identity{}, ErrIdentityMismatch
		}
		return stored.Identity, nil
	}
	if !errors.Is(err, ErrSecretNotFound) {
		return Identity{}, err
	}

	if m.enclave != nil {
		identity, enclaveErr := m.enclave.EnsureKey(ctx, m.installID)
		if enclaveErr == nil {
			encoded, encodeErr := json.Marshal(storedIdentity{Identity: identity})
			if encodeErr == nil {
				if saveErr := m.store.Save(ctx, m.installID, base64.StdEncoding.EncodeToString(encoded)); saveErr == nil {
					return identity, nil
				}
			}
		}
		// Secure Enclave 不可用：安全回退到软件密钥，不中断设备身份建立。
	}

	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return Identity{}, err
	}
	privateDER, err := x509.MarshalPKCS8PrivateKey(privateKey)
	if err != nil {
		return Identity{}, err
	}
	publicDER, err := x509.MarshalPKIXPublicKey(&privateKey.PublicKey)
	if err != nil {
		return Identity{}, err
	}
	publicDigest := sha256.Sum256(publicDER)
	identity := Identity{
		InstallID:     m.installID,
		PublicKey:     base64.StdEncoding.EncodeToString(publicDER),
		KeyID:         hex.EncodeToString(publicDigest[:8]),
		SecureEnclave: false,
		KeyFormat:     "pkcs8-p256",
	}
	encoded, err := json.Marshal(storedIdentity{
		Identity: identity, PrivateKey: base64.StdEncoding.EncodeToString(privateDER),
	})
	if err != nil {
		return Identity{}, err
	}
	if err := m.store.Save(ctx, m.installID, base64.StdEncoding.EncodeToString(encoded)); err != nil {
		return Identity{}, err
	}
	return identity, nil
}

func (m *Manager) Sign(ctx context.Context, challenge []byte) ([]byte, error) {
	secret, err := m.store.Load(ctx, m.installID)
	if err != nil {
		if errors.Is(err, ErrSecretNotFound) {
			return nil, ErrIdentityMismatch
		}
		return nil, err
	}
	stored, err := decodeSecret(secret)
	if err != nil {
		return nil, err
	}
	if stored.InstallID != m.installID {
		return nil, ErrIdentityMismatch
	}
	if stored.SecureEnclave {
		if m.enclave == nil {
			return nil, ErrEnclaveUnavailable
		}
		return m.enclave.Sign(ctx, m.installID, challenge)
	}
	privateDER, err := base64.StdEncoding.DecodeString(stored.PrivateKey)
	if err != nil {
		return nil, err
	}
	privateKey, err := x509.ParsePKCS8PrivateKey(privateDER)
	if err != nil {
		return nil, err
	}
	ecdsaKey, ok := privateKey.(*ecdsa.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("unexpected private key type %T", privateKey)
	}
	digest := sha256.Sum256(challenge)
	return ecdsa.SignASN1(rand.Reader, ecdsaKey, digest[:])
}

func decodeSecret(secret string) (storedIdentity, error) {
	if secret == "" {
		return storedIdentity{}, ErrSecretNotFound
	}
	raw, err := base64.StdEncoding.DecodeString(secret)
	if err != nil {
		return storedIdentity{}, err
	}
	var stored storedIdentity
	if err := json.Unmarshal(raw, &stored); err != nil {
		return storedIdentity{}, err
	}
	if stored.KeyFormat == "se-p256" {
		// 安全硬件身份：私钥不可导出，秘密载荷只携带公钥元数据。
		if stored.InstallID == "" || stored.PublicKey == "" || stored.KeyID == "" {
			return storedIdentity{}, errors.New("device identity secret is incomplete")
		}
		return stored, nil
	}
	if stored.InstallID == "" || stored.PublicKey == "" ||
		stored.KeyID == "" || stored.PrivateKey == "" {
		return storedIdentity{}, errors.New("device identity secret is incomplete")
	}
	return stored, nil
}
