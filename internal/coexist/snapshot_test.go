package coexist

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSnapshotUsesPrivateAtomicWrite(t *testing.T) {
	store := SnapshotStore{Dir: filepath.Join(t.TempDir(), "coexist")}
	if err := store.Save(Snapshot{SchemaVersion: "1"}); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(store.Dir, "coexistence.json")
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode=%o", info.Mode().Perm())
	}
	if _, err := os.Stat(path + ".tmp"); !os.IsNotExist(err) {
		t.Fatal("temporary snapshot file remains")
	}
	dirInfo, err := os.Stat(store.Dir)
	if err != nil {
		t.Fatal(err)
	}
	if dirInfo.Mode().Perm() != 0o700 {
		t.Fatalf("dir mode=%o", dirInfo.Mode().Perm())
	}
}

func TestSnapshotValidateRejectsIncompleteButSaveAllowsDiagnostics(t *testing.T) {
	snapshot := Snapshot{
		SchemaVersion:    "1",
		Profile:          Profile{OS: "darwin", IncompleteLayers: []Layer{LayerProxy}},
		IncompleteLayers: []Layer{LayerProxy},
	}
	if err := snapshot.Validate(); err == nil {
		t.Fatal("expected incomplete snapshot validation error")
	}
	store := SnapshotStore{Dir: filepath.Join(t.TempDir(), "coexist")}
	if err := store.Save(snapshot); err != nil {
		t.Fatalf("incomplete snapshot should be saveable for diagnostics: %v", err)
	}
}
