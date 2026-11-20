package devicebridge

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSessionStoreIsPrivateAndExpires(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state", "device-session.json")
	store := SessionStore{Path: path}
	now := time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC)
	session := Session{
		SchemaVersion: SchemaVersion,
		Serial:        "tvbox-1",
		Platform:      PlatformAndroidTV,
		Token:         "transport-token",
		Fingerprint:   "sha256:device",
		DeviceID:      "device-id",
		ExpiresAt:     now.Add(time.Minute),
	}
	if err := store.Save(session); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("permissions=%o", info.Mode().Perm())
	}
	loaded, err := store.Load("tvbox-1", now)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Token != session.Token || loaded.DeviceID != session.DeviceID {
		t.Fatalf("loaded=%#v", loaded)
	}
	if _, err := store.Load("other-device", now); !errors.Is(err, ErrSessionMissing) {
		t.Fatalf("wrong serial=%v", err)
	}
	if _, err := store.Load("tvbox-1", now.Add(2*time.Minute)); !errors.Is(err, ErrSessionExpired) {
		t.Fatalf("expired=%v", err)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expired file still exists: %v", err)
	}
}
