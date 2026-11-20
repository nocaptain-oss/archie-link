package deviceid

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
)

// fakeEnclave 模拟安全硬件：EnsureKey 签发固定身份，Sign 输出标记。
// 用于验证 Manager 的编排逻辑，不依赖真实 Secure Enclave 芯片。
type fakeEnclave struct {
	created   map[string]Identity
	failFirst bool
}

func newFakeEnclave() *fakeEnclave {
	return &fakeEnclave{created: map[string]Identity{}}
}

func (f *fakeEnclave) EnsureKey(ctx context.Context, installID string) (Identity, error) {
	if f.failFirst {
		f.failFirst = false
		return Identity{}, ErrEnclaveUnavailable
	}
	if identity, ok := f.created[installID]; ok {
		return identity, nil
	}
	private, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return Identity{}, err
	}
	publicDER, err := x509.MarshalPKIXPublicKey(&private.PublicKey)
	if err != nil {
		return Identity{}, err
	}
	identity := Identity{
		InstallID:     installID,
		PublicKey:     base64.StdEncoding.EncodeToString(publicDER),
		KeyID:         "fake-key-id",
		SecureEnclave: true,
		KeyFormat:     "se-p256",
	}
	f.created[installID] = identity
	return identity, nil
}

func (f *fakeEnclave) Sign(ctx context.Context, installID string, challenge []byte) ([]byte, error) {
	if _, ok := f.created[installID]; !ok {
		return nil, ErrSecretNotFound
	}
	digest := sha256.Sum256(challenge)
	return append([]byte{0x30}, digest[:]...), nil
}

func TestManagerUsesEnclaveIdentityWhenAvailable(t *testing.T) {
	enclave := newFakeEnclave()
	store := FileStore{Path: filepath.Join(t.TempDir(), "device.key")}
	manager := NewWithEnclave(store, "install-1", enclave)

	identity, err := manager.LoadOrCreate(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !identity.SecureEnclave || identity.KeyFormat != "se-p256" || identity.KeyID != "fake-key-id" {
		t.Fatalf("expected secure enclave identity, got %#v", identity)
	}

	again, err := manager.LoadOrCreate(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if again.PublicKey != identity.PublicKey || !again.SecureEnclave {
		t.Fatalf("identity not stable across reloads: %#v vs %#v", identity, again)
	}

	signature, err := manager.Sign(context.Background(), []byte("challenge"))
	if err != nil {
		t.Fatal(err)
	}
	if len(signature) == 0 {
		t.Fatal("expected non-empty enclave signature")
	}

	// 设备身份秘密里不得携带私钥字节。
	secret, err := store.Load(context.Background(), "install-1")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := base64.StdEncoding.DecodeString(secret)
	if err != nil {
		t.Fatal(err)
	}
	var stored storedIdentity
	if err := json.Unmarshal(raw, &stored); err != nil {
		t.Fatal(err)
	}
	if stored.PrivateKey != "" {
		t.Fatal("secure enclave identity must not persist private key bytes")
	}
}

func TestManagerFallsBackToKeychainWhenEnclaveFails(t *testing.T) {
	enclave := newFakeEnclave()
	enclave.failFirst = true
	manager := NewWithEnclave(FileStore{Path: filepath.Join(t.TempDir(), "device.key")}, "install-1", enclave)

	identity, err := manager.LoadOrCreate(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if identity.SecureEnclave {
		t.Fatalf("expected keychain fallback, got %#v", identity)
	}
	if identity.KeyFormat != "pkcs8-p256" {
		t.Fatalf("expected software key format, got %q", identity.KeyFormat)
	}

	signature, err := manager.Sign(context.Background(), []byte("challenge"))
	if err != nil {
		t.Fatal(err)
	}
	rawPublic, err := base64.StdEncoding.DecodeString(identity.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := x509.ParsePKIXPublicKey(rawPublic)
	if err != nil {
		t.Fatal(err)
	}
	publicKey, ok := parsed.(*ecdsa.PublicKey)
	if !ok {
		t.Fatalf("unexpected public key type %T", parsed)
	}
	digest := sha256.Sum256([]byte("challenge"))
	if !ecdsa.VerifyASN1(publicKey, digest[:], signature) {
		t.Fatal("fallback signature did not verify")
	}
}

func TestManagerEnclaveSignFailsClosedWithoutBackend(t *testing.T) {
	enclave := newFakeEnclave()
	store := FileStore{Path: filepath.Join(t.TempDir(), "device.key")}
	seed := NewWithEnclave(store, "install-1", enclave)
	if _, err := seed.LoadOrCreate(context.Background()); err != nil {
		t.Fatal(err)
	}

	// 同一个 SE 身份，但新 Manager 没有接入硬件后端：必须 fail closed。
	orphan := New(store, "install-1")
	if _, err := orphan.Sign(context.Background(), []byte("challenge")); !errors.Is(err, ErrEnclaveUnavailable) {
		t.Fatalf("expected enclave unavailable, got %v", err)
	}
}
