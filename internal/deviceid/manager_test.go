package deviceid

import (
	"context"
	"crypto/ecdsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"errors"
	"path/filepath"
	"testing"
)

func TestManagerCreatesStableDeviceIdentityAndSigns(t *testing.T) {
	manager := New(FileStore{Path: filepath.Join(t.TempDir(), "device.key")}, "install-1")
	identity, err := manager.LoadOrCreate(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	again, err := manager.LoadOrCreate(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if identity.PublicKey != again.PublicKey || identity.KeyID == "" {
		t.Fatalf("identity=%#v again=%#v", identity, again)
	}

	challenge := []byte("server-challenge")
	signature, err := manager.Sign(context.Background(), challenge)
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
		t.Fatal("signature did not verify")
	}
}

func TestManagerRejectsIdentityCopiedToAnotherInstall(t *testing.T) {
	path := filepath.Join(t.TempDir(), "device.key")
	first := New(FileStore{Path: path}, "install-1")
	if _, err := first.LoadOrCreate(context.Background()); err != nil {
		t.Fatal(err)
	}
	second := New(FileStore{Path: path}, "install-2")
	if _, err := second.LoadOrCreate(context.Background()); !errors.Is(err, ErrIdentityMismatch) {
		t.Fatalf("expected identity mismatch, got %v", err)
	}
}
