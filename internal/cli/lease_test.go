package cli

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/nocaptain-oss/archie-link/internal/controlplane"
	"github.com/nocaptain-oss/archie-link/internal/deviceid"
	"github.com/nocaptain-oss/archie-link/internal/entitlement"
	appstate "github.com/nocaptain-oss/archie-link/internal/state"
)

func issuedLease(t *testing.T) ([]byte, ed25519.PublicKey, time.Time, *deviceid.Manager) {
	t.Helper()
	now := time.Now().UTC()
	publicKey, signing, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	secret := []byte("notify-secret")
	service := controlplane.NewService("test-key", signing, secret, func() time.Time { return now })
	order, err := service.CreateOrder("perpetual")
	if err != nil {
		t.Fatal(err)
	}
	message := fmt.Sprintf("%s|%s|paid|%d", "evt_1", order.ID, now.Unix())
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(message))
	if err := service.NotifyPayment(order.ID, "evt_1", "paid", now, hex.EncodeToString(mac.Sum(nil))); err != nil {
		t.Fatal(err)
	}
	device := deviceid.New(deviceid.FileStore{Path: filepath.Join(t.TempDir(), "device.key")}, "install-1")
	identity, err := device.LoadOrCreate(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	challenge, err := service.IssueChallenge("install-1", identity.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	signature, err := device.Sign(context.Background(), challenge.Value)
	if err != nil {
		t.Fatal(err)
	}
	data, err := service.Activate(order.ID, "install-1", identity.PublicKey, challenge.ID,
		base64.StdEncoding.EncodeToString(signature))
	if err != nil {
		t.Fatal(err)
	}
	return data, publicKey, now, device
}

func TestVerifyLeaseDataRejectsCopiedLease(t *testing.T) {
	data, publicKey, now, owner := issuedLease(t)
	root := entitlement.TrustRoot{Keys: []entitlement.TrustKey{{ID: "test-key", PublicKey: publicKey}}}

	ownerIdentity, err := owner.LoadOrCreate(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := verifyLeaseData(data, root, ownerIdentity, now, 0); err != nil {
		t.Fatalf("lease must verify on the issuing device: %v", err)
	}

	other := deviceid.New(deviceid.FileStore{Path: filepath.Join(t.TempDir(), "other.key")}, "install-2")
	otherIdentity, err := other.LoadOrCreate(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := verifyLeaseData(data, root, otherIdentity, now, 0); err == nil {
		t.Fatal("lease must not verify on another device")
	}
}

func TestWriteLeaseFileUsesPrivateMode(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state", "lease.json")
	if err := writeLeaseFile(path, []byte(`{"ok":true}`)); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if mode := info.Mode().Perm(); mode != 0o600 {
		t.Fatalf("lease file mode=%o", mode)
	}
}

func TestParseLeaseOptions(t *testing.T) {
	var stderr bytes.Buffer
	options, ok := parseLeaseOptions([]string{
		"--order", "order-1", "--server", "http://127.0.0.1:8790",
		"--file", "/tmp/lease.json", "--plan", "activation-task",
		"--request-id", "request-12345678", "--json",
	}, &stderr)
	if !ok {
		t.Fatalf("parse failed: %s", stderr.String())
	}
	if options.order != "order-1" || options.server != "http://127.0.0.1:8790" ||
		options.file != "/tmp/lease.json" || options.plan != "activation-task" ||
		options.requestID != "request-12345678" || !options.jsonOutput {
		t.Fatalf("unexpected options %#v", options)
	}
	if _, ok := parseLeaseOptions([]string{"--order"}, &stderr); ok {
		t.Fatal("missing option value must fail")
	}
	if _, ok := parseLeaseOptions([]string{"--bogus"}, &stderr); ok {
		t.Fatal("unknown option must fail")
	}
}

func TestRecordLeaseStateOnlineReviewUsesIssuedTime(t *testing.T) {
	store := appstate.Store{Dir: filepath.Join(t.TempDir(), "state")}
	localNow := time.Now().UTC()
	issuedAt := localNow.Add(25 * time.Hour)
	initial := appstate.LeaseState{
		LastSeenAt:          issuedAt,
		ClockReviewRequired: true,
	}
	if err := store.SaveLeaseState(initial); err != nil {
		t.Fatal(err)
	}
	if err := recordLeaseState(store, "order-1", entitlement.Lease{
		LicenseID: "lic-1", IssuedAt: issuedAt,
	}, localNow); err != nil {
		t.Fatal(err)
	}
	state, err := store.LoadLeaseState()
	if err != nil {
		t.Fatal(err)
	}
	if state.ClockReviewRequired || !state.LastSeenAt.Equal(issuedAt) {
		t.Fatalf("clock review was not resolved monotonically: %#v", state)
	}
}
