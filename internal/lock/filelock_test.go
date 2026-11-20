package lock

import (
	"path/filepath"
	"testing"
)

func TestSecondAcquireFails(t *testing.T) {
	path := filepath.Join(t.TempDir(), "archielink.lock")
	first, err := Acquire(path)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()

	if _, err := Acquire(path); err == nil {
		t.Fatal("expected lock conflict")
	}
}

func TestCanReacquireAfterClose(t *testing.T) {
	path := filepath.Join(t.TempDir(), "archielink.lock")
	first, err := Acquire(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	second, err := Acquire(path)
	if err != nil {
		t.Fatal(err)
	}
	_ = second.Close()
}
