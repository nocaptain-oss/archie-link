package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/hmac"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nocaptain-oss/archie-link/internal/controlplane"
	"github.com/nocaptain-oss/archie-link/internal/deviceid"
	"github.com/nocaptain-oss/archie-link/internal/entitlement"
)

// TestHTTPFullFlow 端到端冒烟：HTTP 层走完 订单→支付→挑战→激活→续租→退款→续租拒绝。
func TestHTTPFullFlow(t *testing.T) {
	_, signing, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	service := controlplane.NewService("smoke-key", signing, []byte("smoke-secret"), nil)
	handler, err := controlplane.NewHandler(service, controlplane.HandlerConfig{
		APIToken: "smoke-api", AdminToken: "smoke-admin",
	})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(handler)
	defer server.Close()

	postRaw := func(path string, payload any) (int, []byte, map[string]any) {
		raw, err := json.Marshal(payload)
		if err != nil {
			t.Fatal(err)
		}
		req, err := http.NewRequest(http.MethodPost, server.URL+path, bytes.NewReader(raw))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer smoke-api")
		if strings.HasSuffix(path, "/refund") || strings.HasSuffix(path, "/chargeback") ||
			strings.Contains(path, "/devices") {
			req.Header.Set("Authorization", "Bearer smoke-admin")
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatal(err)
		}
		var out map[string]any
		_ = json.Unmarshal(body, &out)
		return resp.StatusCode, body, out
	}
	post := func(path string, payload any) (int, map[string]any) {
		code, _, out := postRaw(path, payload)
		return code, out
	}

	orderCode, order := post("/v1/orders", map[string]any{"plan": "family-annual-12"})
	if orderCode != http.StatusOK {
		t.Fatalf("create order: %d %v", orderCode, order)
	}
	orderID := order["id"].(string)

	now := time.Now().UTC()
	message := fmt.Sprintf("evt_1|%s|paid|%d", orderID, now.Unix())
	mac := hmac.New(sha256.New, []byte("smoke-secret"))
	mac.Write([]byte(message))
	code, out := post("/v1/orders/"+orderID+"/payment", map[string]any{
		"event_id": "evt_1", "status": "paid", "timestamp": now.Format(time.RFC3339),
		"signature": hex.EncodeToString(mac.Sum(nil)),
	})
	if code != http.StatusOK {
		t.Fatalf("payment: %d %v", code, out)
	}

	device := deviceid.New(deviceid.FileStore{Path: filepath.Join(t.TempDir(), "device.key")}, "smoke-install")
	identity, err := device.LoadOrCreate(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	signChallenge := func() (nonceID, sigBase64 string) {
		code, out := post("/v1/challenge", map[string]any{
			"install_id": "smoke-install", "device_public_key": identity.PublicKey,
		})
		if code != http.StatusOK {
			t.Fatalf("challenge: %d %v", code, out)
		}
		value, err := base64.StdEncoding.DecodeString(out["challenge"].(string))
		if err != nil {
			t.Fatal(err)
		}
		signature, err := device.Sign(context.Background(), value)
		if err != nil {
			t.Fatal(err)
		}
		return out["nonce_id"].(string), base64.StdEncoding.EncodeToString(signature)
	}

	nonceID, signature := signChallenge()
	code, licenseBody, out := postRaw("/v1/activate", map[string]any{
		"order_id": orderID, "install_id": "smoke-install", "device_public_key": identity.PublicKey,
		"nonce_id": nonceID, "signature": signature,
	})
	if code != http.StatusOK {
		t.Fatalf("activate: %d %v", code, out)
	}
	var licenseEnvelope struct {
		License json.RawMessage `json:"license"`
	}
	if err := json.Unmarshal(licenseBody, &licenseEnvelope); err != nil {
		t.Fatal(err)
	}
	license := licenseEnvelope.License

	// 签发的 license 必须能被客户端信任根验证。
	root := entitlement.TrustRoot{Keys: []entitlement.TrustKey{{
		ID: "smoke-key", PublicKey: signing.Public().(ed25519.PublicKey),
	}}}
	provider := entitlement.LeaseFileProvider{
		Data: license, TrustRoot: root, Device: device,
	}
	ent, err := provider.Verify(context.Background(), entitlement.Request{
		SkillID: controlplane.SkillID, InstallID: "smoke-install", Feature: controlplane.FeatureApply,
	})
	if err != nil {
		t.Fatalf("client-side verification failed: %v", err)
	}
	if ent.LicenseID != order["subscription_id"] {
		t.Fatalf("license=%q subscription=%q", ent.LicenseID, order["subscription_id"])
	}

	nonceID, signature = signChallenge()
	code, out = post("/v1/renew", map[string]any{
		"order_id": orderID, "install_id": "smoke-install", "device_public_key": identity.PublicKey,
		"nonce_id": nonceID, "signature": signature,
	})
	if code != http.StatusOK {
		t.Fatalf("renew: %d %v", code, out)
	}

	code, out = post("/v1/orders/"+orderID+"/refund", map[string]any{})
	if code != http.StatusOK {
		t.Fatalf("refund: %d %v", code, out)
	}
	nonceID, signature = signChallenge()
	code, out = post("/v1/renew", map[string]any{
		"order_id": orderID, "install_id": "smoke-install", "device_public_key": identity.PublicKey,
		"nonce_id": nonceID, "signature": signature,
	})
	if code != http.StatusConflict {
		t.Fatalf("renew after refund must be 409, got %d %v", code, out)
	}
}

func TestHTTPPaymentRejectsBadSignature(t *testing.T) {
	_, signing, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	service := controlplane.NewService("smoke-key", signing, []byte("smoke-secret"), nil)
	handler, err := controlplane.NewHandler(service, controlplane.HandlerConfig{
		APIToken: "smoke-api", AdminToken: "smoke-admin",
	})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(handler)
	defer server.Close()

	req, err := http.NewRequest(http.MethodPost, server.URL+"/v1/orders",
		bytes.NewReader([]byte(`{"plan":"perpetual"}`)))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer smoke-api")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	var order map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&order)

	body := map[string]any{
		"event_id": "evt_x", "status": "paid",
		"timestamp": time.Now().UTC().Format(time.RFC3339),
		"signature": "0000",
	}
	raw, _ := json.Marshal(body)
	req, err = http.NewRequest(http.MethodPost, server.URL+"/v1/orders/"+order["id"].(string)+"/payment",
		bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("forged payment must be 403, got %d", resp.StatusCode)
	}
}

func TestNewPaymentServiceConfiguration(t *testing.T) {
	for _, name := range []string{
		"ARCHIE_LINK_PAY_APP_ID", "ARCHIE_LINK_PAY_PRIVATE_KEY",
		"ARCHIE_LINK_PAY_ALIPAY_PUBLIC_KEY", "ARCHIE_LINK_PAY_SELLER_ID",
		"ARCHIE_LINK_PAY_SERVICE_ID", "ARCHIE_LINK_PAY_GATEWAY",
		"ARCHIE_LINK_PAY_PRODUCTION", "ARCHIE_LINK_PAY_ACTIVATION_PRICE",
	} {
		t.Setenv(name, "")
	}
	service := controlplane.NewService("test", mustSigningKey(t), []byte("secret"), nil)
	if payment, err := newPaymentService(service); err != nil || payment != nil {
		t.Fatalf("empty payment config payment=%v err=%v", payment, err)
	}
	t.Setenv("ARCHIE_LINK_PAY_APP_ID", "app-1")
	if _, err := newPaymentService(service); err == nil {
		t.Fatal("partial payment config must fail closed")
	}

	appKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	alipayKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	alipayPublic, err := x509.MarshalPKIXPublicKey(&alipayKey.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("ARCHIE_LINK_PAY_PRIVATE_KEY", base64.StdEncoding.EncodeToString(x509.MarshalPKCS1PrivateKey(appKey)))
	t.Setenv("ARCHIE_LINK_PAY_ALIPAY_PUBLIC_KEY", base64.StdEncoding.EncodeToString(alipayPublic))
	t.Setenv("ARCHIE_LINK_PAY_SELLER_ID", "2088000000000000")
	t.Setenv("ARCHIE_LINK_PAY_SERVICE_ID", "api_mock_service_id")
	t.Setenv("ARCHIE_LINK_PAY_GATEWAY", "https://openapi-sandbox.dl.alipaydev.com/gateway.do")
	t.Setenv("ARCHIE_LINK_PAY_PRODUCTION", "false")
	t.Setenv("ARCHIE_LINK_PAY_ACTIVATION_PRICE", "19.90")
	if payment, err := newPaymentService(service); err != nil || payment == nil {
		t.Fatalf("valid payment config payment=%v err=%v", payment, err)
	}
}

func mustSigningKey(t *testing.T) ed25519.PrivateKey {
	t.Helper()
	_, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return key
}
