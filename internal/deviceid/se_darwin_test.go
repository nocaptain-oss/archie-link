//go:build darwin && cgo

package deviceid

import (
	"context"
	"crypto/ecdsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"strings"
	"testing"
)

// TestSecureEnclaveKeyLifecycle 在真实硬件上验证 Secure Enclave 主路径。
// 无 SE 的设备（如无 T2 的 Intel Mac）上，SecKeyCreateRandomKey 会拒绝
// Secure Enclave tokenID，此时该测试记录回退事实而不是失败。
func TestSecureEnclaveKeyLifecycle(t *testing.T) {
	store := SecureEnclaveStore{Service: "com.archielink.device.test"}
	defer func() {
		if err := store.ResetKey("install-se"); err != nil {
			t.Logf("cleanup reset failed: %v", err)
		}
	}()

	identity, err := store.EnsureKey(context.Background(), "install-se")
	if err != nil {
		// 真实 SE 缺失属预期回退；错误必须来自钥匙串层而不是代码缺陷。
		if strings.Contains(err.Error(), "key creation failed") || strings.Contains(err.Error(), "key lookup failed") {
			t.Skipf("secure enclave unavailable on this device: %v", err)
		}
		t.Fatalf("unexpected secure enclave failure: %v", err)
	}
	if !identity.SecureEnclave || identity.KeyFormat != "se-p256" {
		t.Fatalf("unexpected identity flags: %#v", identity)
	}

	again, err := store.EnsureKey(context.Background(), "install-se")
	if err != nil {
		t.Fatal(err)
	}
	if again.PublicKey != identity.PublicKey || again.KeyID != identity.KeyID {
		t.Fatalf("enclave key not stable: %#v vs %#v", identity, again)
	}

	challenge := []byte("server-challenge")
	signature, err := store.Sign(context.Background(), "install-se", challenge)
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
	digest := sha256.Sum256(challenge)
	if !ecdsa.VerifyASN1(publicKey, digest[:], signature) {
		t.Fatal("secure enclave signature did not verify")
	}

	if err := store.ResetKey("install-se"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Sign(context.Background(), "install-se", challenge); err == nil {
		t.Fatal("signing after reset must fail")
	}
}
