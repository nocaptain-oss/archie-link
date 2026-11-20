package entitlement

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"testing"
	"time"
)

func TestRejectsExpiredAndFeatureMismatch(t *testing.T) {
	request := Request{
		SkillID: "archie-link", InstallID: "test-install", Feature: "network_apply",
	}
	expired := NewTestProvider(func(e *Entitlement) {
		e.ExpiresAt = time.Now().Add(-time.Minute)
	})
	if _, err := expired.Verify(context.Background(), request); !errors.Is(err, ErrExpired) {
		t.Fatalf("expected expired error, got %v", err)
	}

	mismatch := NewTestProvider(func(e *Entitlement) {
		e.Features = []string{"diagnostics"}
	})
	if _, err := mismatch.Verify(context.Background(), request); !errors.Is(err, ErrFeatureNotGranted) {
		t.Fatalf("expected feature error, got %v", err)
	}
}

func TestRejectsRevokedAndInstallMismatch(t *testing.T) {
	request := Request{
		SkillID: "archie-link", InstallID: "test-install", Feature: "network_apply",
	}
	revoked := NewTestProvider(func(e *Entitlement) {
		now := time.Now().UTC()
		e.RevokedAt = &now
	})
	if _, err := revoked.Verify(context.Background(), request); !errors.Is(err, ErrRevoked) {
		t.Fatalf("expected revoked error, got %v", err)
	}

	mismatch := NewTestProvider(func(e *Entitlement) {
		e.InstallID = "another-install"
	})
	if _, err := mismatch.Verify(context.Background(), request); !errors.Is(err, ErrInstallMismatch) {
		t.Fatalf("expected install mismatch, got %v", err)
	}
}

func TestEd25519RejectsTamperedPayload(t *testing.T) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	entitlement := validEntitlement()
	payload, err := json.Marshal(entitlement)
	if err != nil {
		t.Fatal(err)
	}
	signature := ed25519.Sign(privateKey, payload)
	data, err := json.Marshal(SignedPayload{
		Payload:   payload,
		Signature: base64.StdEncoding.EncodeToString(signature),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := VerifyEd25519(data, publicKey); err != nil {
		t.Fatal(err)
	}

	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(data, &envelope); err != nil {
		t.Fatal(err)
	}
	envelope["payload"] = json.RawMessage(`{"skill_id":"archie-link","license_id":"tampered"}`)
	tampered, err := json.Marshal(envelope)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := VerifyEd25519(tampered, publicKey); !errors.Is(err, ErrInvalidSignature) {
		t.Fatalf("expected invalid signature, got %v", err)
	}
}

func TestSignedProviderAppliesRequestChecks(t *testing.T) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal(validEntitlement())
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(SignedPayload{
		Payload:   payload,
		Signature: base64.StdEncoding.EncodeToString(ed25519.Sign(privateKey, payload)),
	})
	if err != nil {
		t.Fatal(err)
	}
	provider := SignedProvider{Data: data, PublicKey: publicKey}
	if _, err := provider.Verify(context.Background(), Request{
		SkillID: "archie-link", InstallID: "install-1", Feature: "network_apply",
	}); err != nil {
		t.Fatal(err)
	}
}

func TestPinnedSignedProviderUsesKeyedEnvelope(t *testing.T) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	root := TrustRoot{Keys: []TrustKey{{
		ID: "release", PublicKey: publicKey,
		ValidFrom: now.Add(-time.Hour), ValidUntil: now.Add(time.Hour),
	}}}
	payload, err := json.Marshal(validEntitlement())
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(KeyedPayload{
		KeyID: "release", Payload: payload,
		Signature: base64.StdEncoding.EncodeToString(ed25519.Sign(privateKey, payload)),
	})
	if err != nil {
		t.Fatal(err)
	}
	provider := PinnedSignedProvider{Data: data, TrustRoot: root, Now: func() time.Time { return now }}
	if _, err := provider.Verify(context.Background(), Request{
		SkillID: "archie-link", InstallID: "install-1", Feature: "network_apply",
	}); err != nil {
		t.Fatal(err)
	}
}

func validEntitlement() Entitlement {
	now := time.Now().UTC()
	return Entitlement{
		SkillID:   "archie-link",
		LicenseID: "test-license",
		InstallID: "install-1",
		Features:  []string{"diagnostics", "network_apply"},
		IssuedAt:  now.Add(-time.Hour),
		ExpiresAt: now.Add(time.Hour),
	}
}
