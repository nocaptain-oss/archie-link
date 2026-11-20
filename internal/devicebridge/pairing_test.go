package devicebridge

import (
	"errors"
	"testing"
	"time"
)

func TestPairingSessionIsOneTimeAndFingerprintBound(t *testing.T) {
	now := time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC)
	manager := NewPairingManager(func() time.Time { return now }, time.Minute)
	offer, err := manager.Create("tvbox-1", PlatformAndroidTV)
	if err != nil {
		t.Fatal(err)
	}
	if offer.SessionID == "" || offer.Code == "" || len(offer.Code) != 6 {
		t.Fatalf("offer=%#v", offer)
	}
	if offer.Token == "" {
		t.Fatal("pairing offer has no transport token")
	}
	grant, err := manager.Confirm(offer.SessionID, offer.Code, "sha256:device")
	if err != nil {
		t.Fatal(err)
	}
	if grant.Token == "" || grant.Serial != "tvbox-1" || grant.Fingerprint != "sha256:device" {
		t.Fatalf("grant=%#v", grant)
	}
	if grant.Token != offer.Token {
		t.Fatal("pairing grant changed the transport token")
	}
	if _, err := manager.Confirm(offer.SessionID, offer.Code, "sha256:device"); !errors.Is(err, ErrPairingCode) {
		t.Fatalf("reused session=%v", err)
	}
}

func TestPairingSessionFailsAfterExpiryAndAttemptLimit(t *testing.T) {
	now := time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC)
	manager := NewPairingManager(func() time.Time { return now }, time.Minute)
	offer, err := manager.Create("phone-1", PlatformAndroid)
	if err != nil {
		t.Fatal(err)
	}
	wrongCode := "000000"
	if offer.Code == wrongCode {
		wrongCode = "999999"
	}
	if _, err := manager.Confirm(offer.SessionID, wrongCode, "sha256:device"); !errors.Is(err, ErrPairingCode) {
		t.Fatalf("wrong code=%v", err)
	}
	now = now.Add(2 * time.Minute)
	if _, err := manager.Confirm(offer.SessionID, offer.Code, "sha256:device"); !errors.Is(err, ErrPairingExpired) {
		t.Fatalf("expired session=%v", err)
	}

	offer, err = manager.Create("phone-2", PlatformAndroid)
	if err != nil {
		t.Fatal(err)
	}
	for attempt := 0; attempt < maxPairingAttempts; attempt++ {
		_, err = manager.Confirm(offer.SessionID, "invalid", "sha256:device")
	}
	if !errors.Is(err, ErrPairingAttempts) {
		t.Fatalf("attempt limit=%v", err)
	}
}

func TestPairingSessionReplacesOlderSessionForSameDevice(t *testing.T) {
	now := time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC)
	manager := NewPairingManager(func() time.Time { return now }, time.Minute)
	first, err := manager.Create("tvbox-1", PlatformAndroidTV)
	if err != nil {
		t.Fatal(err)
	}
	second, err := manager.Create("tvbox-1", PlatformAndroidTV)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Confirm(first.SessionID, first.Code, "sha256:device"); !errors.Is(err, ErrPairingCode) {
		t.Fatalf("old session remains active: %v", err)
	}
	if _, err := manager.Confirm(second.SessionID, second.Code, "sha256:device"); err != nil {
		t.Fatalf("new session=%v", err)
	}
}
