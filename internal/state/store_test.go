package state

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/nocaptain-oss/archie-link/internal/model"
)

func TestSnapshotUsesPrivatePermissions(t *testing.T) {
	store := Store{Dir: filepath.Join(t.TempDir(), "state")}
	snapshot := model.NetworkSnapshot{
		SchemaVersion:    model.SchemaVersion,
		CapturedAt:       time.Now().UTC(),
		DefaultInterface: "en0",
		Gateway:          "192.168.1.1",
		DNS:              []string{"192.168.1.1"},
	}
	if err := store.SaveSnapshot(snapshot); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(store.Dir, "snapshot.json"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode=%o", info.Mode().Perm())
	}

	loaded, err := store.LoadSnapshot()
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Gateway != snapshot.Gateway {
		t.Fatalf("snapshot=%#v", loaded)
	}
}

func TestRuntimeRoundTripAndClear(t *testing.T) {
	store := Store{Dir: filepath.Join(t.TempDir(), "state")}
	runtime := Runtime{
		SchemaVersion: model.SchemaVersion,
		Phase:         PhaseActive,
		Endpoint:      model.Endpoint{Address: "188.114.97.1", Port: 500},
		Ownership: &model.Ownership{
			Interface: "utun12",
			Endpoint:  model.Endpoint{Address: "188.114.97.1", Port: 500},
		},
	}
	if err := store.SaveRuntime(runtime); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(store.Dir, "runtime.json"))
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded["phase"] != "active" || decoded["ownership"] == nil {
		t.Fatalf("runtime=%s", raw)
	}
	loaded, err := store.LoadRuntime()
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Phase != PhaseActive || loaded.Ownership.Interface != "utun12" {
		t.Fatalf("runtime=%#v", loaded)
	}
	if err := store.ClearRuntime(); err != nil {
		t.Fatal(err)
	}
	if _, err := store.LoadRuntime(); !os.IsNotExist(err) {
		t.Fatalf("expected not found, got %v", err)
	}
}

func TestInstallIDIsStableAndPrivate(t *testing.T) {
	store := Store{Dir: filepath.Join(t.TempDir(), "state")}
	first, err := store.LoadOrCreateInstallID()
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.LoadOrCreateInstallID()
	if err != nil {
		t.Fatal(err)
	}
	if len(first) < 32 || first != second {
		t.Fatalf("first=%q second=%q", first, second)
	}
	info, err := os.Stat(filepath.Join(store.Dir, "install-id"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode=%o", info.Mode().Perm())
	}
}

func TestLeaseStateRoundTripAndPermissions(t *testing.T) {
	store := Store{Dir: filepath.Join(t.TempDir(), "state")}
	now := time.Now().UTC()
	state := LeaseState{
		SchemaVersion:       model.SchemaVersion,
		LicenseID:           "sub-1",
		OrderID:             "order-1",
		RevocationFloor:     4,
		LastSeenAt:          now,
		LastRenewedAt:       now,
		ClockReviewRequired: true,
	}
	if err := store.SaveLeaseState(state); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.LoadLeaseState()
	if err != nil {
		t.Fatal(err)
	}
	if loaded.RevocationFloor != 4 || loaded.LicenseID != "sub-1" || loaded.OrderID != "order-1" ||
		!loaded.ClockReviewRequired {
		t.Fatalf("loaded=%#v", loaded)
	}
	info, err := os.Stat(filepath.Join(store.Dir, "lease-state.json"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode=%o", info.Mode().Perm())
	}
}

func TestLeaseStateObserveNeverMovesBackwards(t *testing.T) {
	now := time.Now().UTC()
	state := LeaseState{LastSeenAt: now}
	if got := state.Observe(now.Add(-time.Hour)).LastSeenAt; !got.Equal(now) {
		t.Fatalf("last seen moved backwards to %s", got)
	}
	later := now.Add(time.Hour)
	if got := state.Observe(later).LastSeenAt; !got.Equal(later) {
		t.Fatalf("last seen=%s want %s", got, later)
	}
}

func TestLeaseStateRoundTripAndMonotonicFloor(t *testing.T) {
	store := Store{Dir: filepath.Join(t.TempDir(), "state")}
	state, err := store.LoadLeaseState()
	if err != nil {
		t.Fatal(err)
	}
	if state.RevocationFloor != 0 {
		t.Fatalf("unexpected initial floor=%d", state.RevocationFloor)
	}
	state.LicenseID = "sub-1"
	state.OrderID = "order-1"
	state.RevocationFloor = 3
	state.LastSeenAt = time.Now().UTC()
	state.LastRenewedAt = state.LastSeenAt
	if err := store.SaveLeaseState(state); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.LoadLeaseState()
	if err != nil {
		t.Fatal(err)
	}
	if loaded.LicenseID != "sub-1" || loaded.RevocationFloor != 3 {
		t.Fatalf("loaded=%#v", loaded)
	}
	info, err := os.Stat(filepath.Join(store.Dir, "lease-state.json"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("lease state mode=%o", info.Mode().Perm())
	}
}
