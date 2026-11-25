package cli

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	appstate "github.com/nocaptain-oss/archie-link/internal/state"
)

func writeRefreshFixture(t *testing.T, dir string) appstate.Store {
	t.Helper()
	store := appstate.Store{Dir: dir}
	// decodeLease 只需要信封 payload 内嵌 license_id，签名不参与刷新路径。
	leaseData := `{"key_id":"lease","payload":{"schema_version":"2","license_id":"sub-1"},"signature":"sig"}`
	if err := os.WriteFile(filepath.Join(dir, "lease.json"), []byte(leaseData), 0o600); err != nil {
		t.Fatal(err)
	}
	return store
}

func setEpochRefreshServer(t *testing.T, url string) {
	t.Helper()
	original := epochRefreshServer
	epochRefreshServer = url
	t.Cleanup(func() { epochRefreshServer = original })
}

func TestBestEffortEpochRefreshRatchetsFloor(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/subscriptions/sub-1/epoch" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"revocation_epoch":3}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()
	setEpochRefreshServer(t, server.URL)

	dir := t.TempDir()
	store := writeRefreshFixture(t, dir)
	if err := store.SaveLeaseState(appstate.LeaseState{RevocationFloor: 1}); err != nil {
		t.Fatal(err)
	}

	bestEffortEpochRefresh(context.Background(), &LifecycleDependencies{Store: store, StateDir: dir})

	state, err := store.LoadLeaseState()
	if err != nil {
		t.Fatal(err)
	}
	if state.RevocationFloor != 3 {
		t.Fatalf("floor=%d want 3", state.RevocationFloor)
	}
}

func TestBestEffortEpochRefreshToleratesMissingLeaseAndStaleEpoch(t *testing.T) {
	// 无租约文件：静默 no-op，不报错不落盘。
	empty := appstate.Store{Dir: t.TempDir()}
	bestEffortEpochRefresh(context.Background(), &LifecycleDependencies{Store: empty, StateDir: empty.Dir})

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"revocation_epoch":0}`))
	}))
	defer server.Close()
	setEpochRefreshServer(t, server.URL)

	dir := t.TempDir()
	store := writeRefreshFixture(t, dir)
	if err := store.SaveLeaseState(appstate.LeaseState{RevocationFloor: 5}); err != nil {
		t.Fatal(err)
	}

	bestEffortEpochRefresh(context.Background(), &LifecycleDependencies{Store: store, StateDir: dir})

	state, err := store.LoadLeaseState()
	if err != nil {
		t.Fatal(err)
	}
	if state.RevocationFloor != 5 {
		t.Fatalf("stale epoch must not lower floor: floor=%d want 5", state.RevocationFloor)
	}
}
