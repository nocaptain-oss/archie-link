package coexist

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"
)

type Snapshot struct {
	SchemaVersion    string           `json:"schema_version"`
	CreatedAt        time.Time        `json:"created_at"`
	Profile          Profile          `json:"profile"`
	Baselines        []EndpointResult `json:"baselines"`
	IncompleteLayers []Layer          `json:"incomplete_layers"`
}

func (s Snapshot) Validate() error {
	if s.SchemaVersion != "1" {
		return errors.New("unsupported coexistence snapshot schema")
	}
	if len(s.IncompleteLayers) > 0 || len(s.Profile.IncompleteLayers) > 0 {
		return errors.New("coexistence snapshot contains incomplete layers")
	}
	return nil
}

type SnapshotStore struct {
	Dir string
}

func (s SnapshotStore) Save(snapshot Snapshot) error {
	if err := os.MkdirAll(s.Dir, 0o700); err != nil {
		return err
	}
	if snapshot.CreatedAt.IsZero() {
		snapshot.CreatedAt = time.Now().UTC()
	}
	raw, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		return err
	}
	path := filepath.Join(s.Dir, "coexistence.json")
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}
