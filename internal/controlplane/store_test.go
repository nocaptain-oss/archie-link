package controlplane

import (
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func newPersistentFixture(t *testing.T, statePath string) fixture {
	t.Helper()
	now := time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC)
	_, signing, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	secret := []byte("internal-notify-secret")
	service, err := NewServiceWithStore("test-key", signing, secret, func() time.Time { return now }, NewFileStore(statePath))
	if err != nil {
		t.Fatal(err)
	}
	order, err := service.CreateOrder("family-annual-12")
	if err != nil {
		t.Fatal(err)
	}
	return fixture{
		service:      service,
		order:        order,
		subscription: order.SubscriptionID,
		notifySecret: secret,
		now:          now,
	}
}

func TestServiceRestoresPersistedState(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "state.json")
	f := newPersistentFixture(t, statePath)
	device := newDevice(t, dir, "install-1")
	if err := f.pay(t, f.order.ID, "evt_1", f.now); err != nil {
		t.Fatal(err)
	}
	if _, err := f.activate(t, "install-1", device); err != nil {
		t.Fatal(err)
	}
	if err := f.service.Refund(f.order.ID); err != nil {
		t.Fatal(err)
	}

	restarted, err := NewServiceWithStore("test-key", f.service.signingKey, f.notifySecret,
		func() time.Time { return f.now.Add(time.Minute) }, NewFileStore(statePath))
	if err != nil {
		t.Fatal(err)
	}
	if restarted.orders[f.order.ID] == nil || restarted.orders[f.order.ID].Status != "refunded" {
		t.Fatalf("order state was not restored: %#v", restarted.orders)
	}
	epoch, err := restarted.CurrentEpoch(f.subscription)
	if err != nil {
		t.Fatal(err)
	}
	if epoch != 1 {
		t.Fatalf("revocation epoch was not restored: got %d", epoch)
	}
	devices, err := restarted.ListDevices(f.subscription)
	if err != nil {
		t.Fatal(err)
	}
	if len(devices) != 1 || devices[0].InstallID != "install-1" {
		t.Fatalf("device pool was not restored: %#v", devices)
	}
	if _, err := restarted.Renew(f.order.ID, "install-1", devices[0].PublicKey, "unused", ""); !errors.Is(err, ErrSubscriptionInactive) {
		t.Fatalf("refunded subscription renewed after restart: %v", err)
	}
}

func TestFileStoreFailsClosedOnCorruptState(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "state.json")
	if err := os.WriteFile(statePath, []byte("{broken"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, signing, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewServiceWithStore("test-key", signing, []byte("secret"), nil, NewFileStore(statePath)); err == nil {
		t.Fatal("corrupt state must fail closed")
	}
}

func TestFileStoreWritesPrivateAtomicState(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "nested", "state.json")
	store := NewFileStore(statePath)
	state := emptyState()
	state.Orders["ord_1"] = &Order{ID: "ord_1", Status: "created"}
	if err := store.Save(state); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("state file permission = %o, want 600", perm)
	}
	dirInfo, err := os.Stat(filepath.Dir(statePath))
	if err != nil {
		t.Fatal(err)
	}
	if perm := dirInfo.Mode().Perm(); perm != 0o700 {
		t.Fatalf("state dir permission = %o, want 700", perm)
	}
	loaded, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Orders["ord_1"] == nil || loaded.Orders["ord_1"].Status != "created" {
		t.Fatalf("state round trip failed: %#v", loaded.Orders)
	}
}

type failingStore struct{}

func (failingStore) Load() (State, error) { return emptyState(), nil }
func (failingStore) Save(State) error     { return errors.New("disk full") }

func TestServiceRollsBackWhenPersistFails(t *testing.T) {
	_, signing, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	service, err := NewServiceWithStore("test-key", signing, []byte("secret"), nil, failingStore{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.CreateOrder("perpetual"); err == nil {
		t.Fatal("persist failure must be returned")
	}
	if len(service.orders) != 0 || len(service.subscriptions) != 0 {
		t.Fatalf("failed persist left partial state: orders=%d subscriptions=%d", len(service.orders), len(service.subscriptions))
	}
}

func TestServiceConcurrentWrites(t *testing.T) {
	_, signing, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	service := NewService("test-key", signing, []byte("secret"), nil)
	var group sync.WaitGroup
	for i := 0; i < 100; i++ {
		group.Add(1)
		go func() {
			defer group.Done()
			if _, err := service.CreateOrder("perpetual"); err != nil {
				t.Errorf("CreateOrder: %v", err)
			}
		}()
	}
	group.Wait()
	if len(service.orders) != 100 {
		t.Fatalf("lost concurrent orders: got %d", len(service.orders))
	}
}
