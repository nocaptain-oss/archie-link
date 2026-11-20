package devicebridge

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

var (
	ErrSessionMissing = errors.New("device pairing session is missing")
	ErrSessionExpired = errors.New("device pairing session expired")
)

type Session struct {
	SchemaVersion int       `json:"schema_version"`
	Serial        string    `json:"serial"`
	Platform      Platform  `json:"platform"`
	Token         string    `json:"token"`
	Fingerprint   string    `json:"fingerprint"`
	DeviceID      string    `json:"device_id"`
	ConfigKey     string    `json:"config_key"`
	ExpiresAt     time.Time `json:"expires_at"`
}

type SessionStore struct {
	Path string
}

func (s SessionStore) Save(session Session) error {
	if strings.TrimSpace(s.Path) == "" || session.SchemaVersion != SchemaVersion ||
		strings.TrimSpace(session.Serial) == "" || strings.TrimSpace(session.Token) == "" ||
		session.ExpiresAt.IsZero() {
		return ErrInvalidRequest
	}
	dir := filepath.Dir(s.Path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create device session directory: %w", err)
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return fmt.Errorf("secure device session directory: %w", err)
	}
	raw, err := json.Marshal(session)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".device-session-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(raw); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, s.Path)
}

func (s SessionStore) Load(serial string, now time.Time) (Session, error) {
	if strings.TrimSpace(s.Path) == "" {
		return Session{}, ErrSessionMissing
	}
	info, err := os.Stat(s.Path)
	if errors.Is(err, os.ErrNotExist) {
		return Session{}, ErrSessionMissing
	}
	if err != nil {
		return Session{}, err
	}
	if info.Mode().Perm()&0o077 != 0 {
		return Session{}, errors.New("device session file permissions are too broad")
	}
	raw, err := os.ReadFile(s.Path)
	if err != nil {
		return Session{}, err
	}
	var session Session
	if err := json.Unmarshal(raw, &session); err != nil {
		return Session{}, err
	}
	if session.SchemaVersion != SchemaVersion || session.Serial != strings.TrimSpace(serial) ||
		strings.TrimSpace(session.Token) == "" {
		return Session{}, ErrSessionMissing
	}
	if !now.Before(session.ExpiresAt) {
		_ = s.Delete()
		return Session{}, ErrSessionExpired
	}
	return session, nil
}

func (s SessionStore) Delete() error {
	if strings.TrimSpace(s.Path) == "" {
		return nil
	}
	err := os.Remove(s.Path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}
