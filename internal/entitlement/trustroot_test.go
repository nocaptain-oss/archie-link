package entitlement

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"testing"
	"time"
)

func TestTrustRootVerifiesCurrentAndRetiredKeys(t *testing.T) {
	currentPublic, currentPrivate, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	oldPublic, oldPrivate, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	root := TrustRoot{Keys: []TrustKey{
		{ID: "current", PublicKey: currentPublic, ValidFrom: now.Add(-time.Hour), ValidUntil: now.Add(time.Hour)},
		{ID: "old", PublicKey: oldPublic, ValidFrom: now.Add(-48 * time.Hour), ValidUntil: now.Add(time.Hour)},
	}}

	for _, tc := range []struct {
		keyID string
		key   ed25519.PrivateKey
	}{
		{keyID: "current", key: currentPrivate},
		{keyID: "old", key: oldPrivate},
	} {
		data := signedKeyedPayload(t, tc.keyID, tc.key, map[string]string{"hello": "world"})
		payload, keyID, err := root.VerifyKeyed(data, now)
		if err != nil {
			t.Fatalf("%s: %v", tc.keyID, err)
		}
		if keyID != tc.keyID || !json.Valid(payload) {
			t.Fatalf("keyID=%s payload=%s", keyID, payload)
		}
	}

	expiredRoot := TrustRoot{Keys: []TrustKey{
		{ID: "old", PublicKey: oldPublic, ValidFrom: now.Add(-48 * time.Hour), ValidUntil: now.Add(-time.Second)},
	}}
	if _, _, err := expiredRoot.VerifyKeyed(signedKeyedPayload(t, "old", oldPrivate, map[string]string{}), now); !errors.Is(err, ErrTrustKeyExpired) {
		t.Fatalf("expected expired key, got %v", err)
	}
}

func TestTrustRootRejectsUnknownAndTamperedKeys(t *testing.T) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	root := TrustRoot{Keys: []TrustKey{{
		ID: "current", PublicKey: publicKey,
		ValidFrom: now.Add(-time.Hour), ValidUntil: now.Add(time.Hour),
	}}}
	data := signedKeyedPayload(t, "unknown", privateKey, map[string]string{"ok": "yes"})
	if _, _, err := root.VerifyKeyed(data, now); !errors.Is(err, ErrUnknownTrustKey) {
		t.Fatalf("expected unknown key, got %v", err)
	}

	var envelope KeyedPayload
	if err := json.Unmarshal(signedKeyedPayload(t, "current", privateKey, map[string]string{"ok": "yes"}), &envelope); err != nil {
		t.Fatal(err)
	}
	envelope.Payload = json.RawMessage(`{"ok":"tampered"}`)
	tampered, err := json.Marshal(envelope)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := root.VerifyKeyed(tampered, now); !errors.Is(err, ErrInvalidSignature) {
		t.Fatalf("expected invalid signature, got %v", err)
	}
}

func signedKeyedPayload(t *testing.T, keyID string, key ed25519.PrivateKey, value any) []byte {
	t.Helper()
	payload, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(KeyedPayload{
		KeyID:     keyID,
		Payload:   payload,
		Signature: base64.StdEncoding.EncodeToString(ed25519.Sign(key, payload)),
	})
	if err != nil {
		t.Fatal(err)
	}
	return data
}
