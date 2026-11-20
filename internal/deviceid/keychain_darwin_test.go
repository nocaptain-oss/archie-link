//go:build darwin

package deviceid

import (
	"context"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestKeychainStoreRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "archielink-test.keychain-db")
	if err := exec.Command("security", "create-keychain", "-p", "test", path).Run(); err != nil {
		t.Skipf("cannot create temporary keychain: %v", err)
	}
	t.Cleanup(func() { _ = exec.Command("security", "delete-keychain", path).Run() })
	if err := exec.Command("security", "unlock-keychain", "-p", "test", path).Run(); err != nil {
		t.Fatal(err)
	}
	store := KeychainStore{Service: "com.archielink.device.test", KeychainPath: path}
	if err := store.Save(context.Background(), "install-test", "secret-value"); err != nil {
		t.Fatal(err)
	}
	got, err := store.Load(context.Background(), "install-test")
	if err != nil {
		t.Fatal(err)
	}
	if got != "secret-value" {
		t.Fatalf("secret=%q", got)
	}
}
