package controlplane

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func testHandler(t *testing.T) http.Handler {
	t.Helper()
	_, signing, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	handler, err := NewHandler(NewService("test", signing, []byte("secret"), nil), HandlerConfig{
		APIToken: "api-token", AdminToken: "admin-token",
	})
	if err != nil {
		t.Fatal(err)
	}
	return handler
}

func TestHandlerExposesEpochWithoutToken(t *testing.T) {
	handler := testHandler(t)
	req := httptest.NewRequest(http.MethodGet, "/v1/subscriptions/missing/epoch", nil)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)
	// epoch 端点公开：无 token 须穿透到路由逻辑（缺失订阅返回 404 而非 401）。
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("epoch without token must reach handler (404 for missing subscription), got %d", recorder.Code)
	}
}

func TestHandlerRequiresScopedBearerTokens(t *testing.T) {
	handler := testHandler(t)
	request := func(path, token string) int {
		t.Helper()
		req := httptest.NewRequest(http.MethodPost, path, bytes.NewBufferString(`{"plan":"perpetual"}`))
		req.Header.Set("Content-Type", "application/json")
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, req)
		return recorder.Code
	}
	if code := request("/v1/orders", ""); code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated order code=%d", code)
	}
	if code := request("/v1/orders", "admin-token"); code != http.StatusUnauthorized {
		t.Fatalf("admin token must not authorize user API: %d", code)
	}
	if code := request("/v1/orders", "api-token"); code != http.StatusOK {
		t.Fatalf("api token code=%d", code)
	}
}

func TestHandlerRequiresAdminTokenForRefund(t *testing.T) {
	handler := testHandler(t)
	request := func(token string) int {
		req := httptest.NewRequest(http.MethodPost, "/v1/orders/missing/refund", nil)
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, req)
		return recorder.Code
	}
	if code := request(""); code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated refund=%d", code)
	}
	if code := request("api-token"); code != http.StatusUnauthorized {
		t.Fatalf("api token authorized refund=%d", code)
	}
	if code := request("admin-token"); code != http.StatusNotFound {
		t.Fatalf("admin token should reach refund route: %d", code)
	}
}

func TestHandlerFailsClosedWithoutTokens(t *testing.T) {
	_, signing, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewHandler(NewService("test", signing, []byte("secret"), nil), HandlerConfig{}); !errors.Is(err, ErrAuthNotConfigured) {
		t.Fatalf("expected auth config error, got %v", err)
	}
}

func TestRateLimiterBurstAndRefill(t *testing.T) {
	now := time.Unix(1000, 0)
	limiter := NewRateLimiter(1, 1) // 1/minute, burst 1
	limiter.now = func() time.Time { return now }
	if !limiter.Allow("ip") {
		t.Fatal("first request should pass")
	}
	if limiter.Allow("ip") {
		t.Fatal("second request should be limited")
	}
	now = now.Add(time.Minute)
	if !limiter.Allow("ip") {
		t.Fatal("bucket should refill")
	}
}
