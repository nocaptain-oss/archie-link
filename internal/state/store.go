package state

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/nocaptain-oss/archie-link/internal/model"
)

type Runtime struct {
	SchemaVersion string           `json:"schema_version"`
	Phase         Phase            `json:"phase"`
	Endpoint      model.Endpoint   `json:"endpoint"`
	Ownership     *model.Ownership `json:"ownership,omitempty"`
	LastError     string           `json:"last_error,omitempty"`
	UpdatedAt     time.Time        `json:"updated_at"`
}

type LeaseState struct {
	SchemaVersion       string    `json:"schema_version"`
	LicenseID           string    `json:"license_id"`
	OrderID             string    `json:"order_id"`
	RevocationFloor     int       `json:"revocation_floor"`
	LastSeenAt          time.Time `json:"last_seen_at"`
	LastRenewedAt       time.Time `json:"last_renewed_at"`
	ClockReviewRequired bool      `json:"clock_review_required,omitempty"`
}

func (s LeaseState) Observe(now time.Time) LeaseState {
	if now.After(s.LastSeenAt) {
		s.LastSeenAt = now
	}
	return s
}

type Store struct {
	Dir          string
	OwnerUID     int
	OwnerGID     int
	SetOwnership bool
}

func (s Store) SaveSnapshot(snapshot model.NetworkSnapshot) error {
	if snapshot.SchemaVersion == "" {
		snapshot.SchemaVersion = model.SchemaVersion
	}
	if snapshot.CapturedAt.IsZero() {
		snapshot.CapturedAt = time.Now().UTC()
	}
	raw, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		return err
	}
	return s.writeAtomic("snapshot.json", raw)
}

func (s Store) LoadSnapshot() (model.NetworkSnapshot, error) {
	var snapshot model.NetworkSnapshot
	err := s.readJSON("snapshot.json", &snapshot)
	return snapshot, err
}

func (s Store) SaveRuntime(runtime Runtime) error {
	if runtime.SchemaVersion == "" {
		runtime.SchemaVersion = model.SchemaVersion
	}
	runtime.UpdatedAt = time.Now().UTC()
	raw, err := json.MarshalIndent(runtime, "", "  ")
	if err != nil {
		return err
	}
	return s.writeAtomic("runtime.json", raw)
}

func (s Store) LoadRuntime() (Runtime, error) {
	var runtime Runtime
	err := s.readJSON("runtime.json", &runtime)
	return runtime, err
}

func (s Store) ClearRuntime() error {
	err := os.Remove(filepath.Join(s.Dir, "runtime.json"))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

func (s Store) ClearSnapshot() error {
	err := os.Remove(filepath.Join(s.Dir, "snapshot.json"))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

func (s Store) LoadOrCreateInstallID() (string, error) {
	if err := s.ensureDir(); err != nil {
		return "", err
	}
	path := filepath.Join(s.Dir, "install-id")
	if raw, err := os.ReadFile(path); err == nil {
		return strings.TrimSpace(string(raw)), nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	random := make([]byte, 16)
	if _, err := rand.Read(random); err != nil {
		return "", err
	}
	value := hex.EncodeToString(random)
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if errors.Is(err, os.ErrExist) {
		raw, readErr := os.ReadFile(path)
		return strings.TrimSpace(string(raw)), readErr
	}
	if err != nil {
		return "", err
	}
	if _, err := file.WriteString(value + "\n"); err != nil {
		_ = file.Close()
		return "", err
	}
	if err := file.Close(); err != nil {
		return "", err
	}
	if err := os.Chmod(path, 0o600); err != nil {
		return "", err
	}
	if err := s.chown(path); err != nil {
		return "", err
	}
	return value, nil
}

func (s Store) LoadLeaseState() (LeaseState, error) {
	var state LeaseState
	err := s.readJSON("lease-state.json", &state)
	if errors.Is(err, os.ErrNotExist) {
		return LeaseState{SchemaVersion: model.SchemaVersion}, nil
	}
	if err != nil {
		return LeaseState{}, err
	}
	if state.SchemaVersion != model.SchemaVersion {
		return LeaseState{}, fmt.Errorf("unsupported lease state schema %q", state.SchemaVersion)
	}
	return state, nil
}

func (s Store) SaveLeaseState(state LeaseState) error {
	if state.SchemaVersion == "" {
		state.SchemaVersion = model.SchemaVersion
	}
	if state.SchemaVersion != model.SchemaVersion {
		return fmt.Errorf("unsupported lease state schema %q", state.SchemaVersion)
	}
	raw, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	return s.writeAtomic("lease-state.json", raw)
}

func (s Store) readJSON(name string, value any) error {
	raw, err := os.ReadFile(filepath.Join(s.Dir, name))
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, value)
}

func (s Store) writeAtomic(name string, data []byte) error {
	if err := s.ensureDir(); err != nil {
		return err
	}
	path := filepath.Join(s.Dir, name)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	if err := os.Chmod(tmp, 0o600); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if err := s.chown(tmp); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

func (s Store) ensureDir() error {
	if err := os.MkdirAll(s.Dir, 0o700); err != nil {
		return err
	}
	if err := os.Chmod(s.Dir, 0o700); err != nil {
		return err
	}
	return s.chown(s.Dir)
}

func (s Store) chown(path string) error {
	if !s.SetOwnership {
		return nil
	}
	return os.Chown(path, s.OwnerUID, s.OwnerGID)
}
