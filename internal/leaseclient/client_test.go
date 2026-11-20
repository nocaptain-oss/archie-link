package leaseclient

import (
	"context"
	"crypto/ed25519"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/nocaptain-oss/archie-link/internal/controlplane"
	"github.com/nocaptain-oss/archie-link/internal/deviceid"
	"github.com/nocaptain-oss/archie-link/internal/entitlement"
)

func newTestServer(t *testing.T) (*controlplane.Service, *httptest.Server, []byte, time.Time, ed25519.PublicKey) {
	t.Helper()
	now := time.Now().UTC()
	publicKey, signing, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	secret := []byte("notify-secret")
	service := controlplane.NewService("test-key", signing, secret, func() time.Time { return now })
	handler, err := controlplane.NewHandler(service, controlplane.HandlerConfig{
		APIToken: "test-api", AdminToken: "test-admin",
	})
	if err != nil {
		t.Fatal(err)
	}
	return service, httptest.NewServer(handler), secret, now, publicKey
}

func payOrder(t *testing.T, service *controlplane.Service, secret []byte, orderID string, at time.Time) {
	t.Helper()
	message := fmt.Sprintf("%s|%s|paid|%d", "evt_1", orderID, at.Unix())
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(message))
	if err := service.NotifyPayment(orderID, "evt_1", "paid", at, hex.EncodeToString(mac.Sum(nil))); err != nil {
		t.Fatal(err)
	}
}

func TestActivateRenewAndEpochOverHTTP(t *testing.T) {
	service, server, secret, now, publicKey := newTestServer(t)
	defer server.Close()
	order, err := service.CreateOrder("family-annual-12")
	if err != nil {
		t.Fatal(err)
	}
	payOrder(t, service, secret, order.ID, now)

	device := deviceid.New(deviceid.FileStore{Path: filepath.Join(t.TempDir(), "device.key")}, "install-1")
	client := Client{BaseURL: server.URL, Device: device, InstallID: "install-1", APIToken: "test-api"}
	ctx := context.Background()

	data, err := client.Activate(ctx, order.ID)
	if err != nil {
		t.Fatal(err)
	}
	root := entitlement.TrustRoot{Keys: []entitlement.TrustKey{{
		ID: "test-key", PublicKey: publicKey,
	}}}
	provider := entitlement.LeaseFileProvider{
		Data: data, TrustRoot: root, Device: device,
		Now: func() time.Time { return now },
	}
	ent, err := provider.Verify(ctx, entitlement.Request{
		SkillID: controlplane.SkillID, InstallID: "install-1", Feature: controlplane.FeatureApply,
	})
	if err != nil {
		t.Fatalf("lease from HTTP control plane failed client verification: %v", err)
	}
	if ent.LicenseID != order.SubscriptionID {
		t.Fatalf("license=%q subscription=%q", ent.LicenseID, order.SubscriptionID)
	}

	renewed, err := client.Renew(ctx, order.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := entitlement.VerifyLease(renewed, root, entitlement.LeaseRequest{
		SkillID: controlplane.SkillID, InstallID: "install-1",
		DevicePublicKey: entInstallPublicKey(t, device), Feature: controlplane.FeatureApply,
	}, now); err != nil {
		t.Fatalf("renewed lease failed verification: %v", err)
	}

	epoch, err := client.Epoch(ctx, order.SubscriptionID)
	if err != nil {
		t.Fatal(err)
	}
	if epoch != 0 {
		t.Fatalf("initial epoch=%d", epoch)
	}

	if err := service.Refund(order.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := client.Renew(ctx, order.ID); err == nil {
		t.Fatal("renew after refund must fail")
	}
}

func TestActivateUnpaidOrderFails(t *testing.T) {
	service, server, _, _, _ := newTestServer(t)
	defer server.Close()
	order, err := service.CreateOrder("perpetual")
	if err != nil {
		t.Fatal(err)
	}
	device := deviceid.New(deviceid.FileStore{Path: filepath.Join(t.TempDir(), "device.key")}, "install-1")
	client := Client{BaseURL: server.URL, Device: device, InstallID: "install-1", APIToken: "test-api"}
	if _, err := client.Activate(context.Background(), order.ID); err == nil {
		t.Fatal("unpaid order must not issue a lease")
	}
}

func TestBeginActivationIntentReturns402Bill(t *testing.T) {
	device := deviceid.New(deviceid.FileStore{Path: filepath.Join(t.TempDir(), "device.key")}, "install-1")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request ActivationIntentRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if request.RequestID != "request-12345678" || request.Plan != controlplane.ActivationTaskPlan ||
			request.InstallID != "install-1" || request.DevicePublicKey == "" {
			t.Fatalf("request=%#v", request)
		}
		w.Header().Set("Payment-Needed", "encoded-bill")
		w.WriteHeader(http.StatusPaymentRequired)
		_, _ = w.Write([]byte(`{"code":"Payment-Needed"}`))
	}))
	defer server.Close()

	client := Client{BaseURL: server.URL, Device: device, InstallID: "install-1"}
	result, err := client.BeginActivationIntent(context.Background(), "request-12345678", controlplane.ActivationTaskPlan)
	if err != nil {
		t.Fatal(err)
	}
	if !result.PaymentRequired || result.PaymentNeeded != "encoded-bill" {
		t.Fatalf("result=%#v", result)
	}
}

func TestClientRequiresDeviceAndOrder(t *testing.T) {
	client := Client{}
	if _, err := client.Activate(context.Background(), "order"); err == nil {
		t.Fatal("missing device must fail")
	}
	client.Device = deviceid.New(deviceid.FileStore{Path: filepath.Join(t.TempDir(), "k")}, "i")
	if _, err := client.Activate(context.Background(), ""); err == nil {
		t.Fatal("missing order must fail")
	}
}

func entInstallPublicKey(t *testing.T, device *deviceid.Manager) string {
	t.Helper()
	identity, err := device.LoadOrCreate(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return identity.PublicKey
}
