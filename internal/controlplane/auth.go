package controlplane

import (
	"crypto/subtle"
	"errors"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

var ErrAuthNotConfigured = errors.New("controlplane API and admin tokens are required")

type HandlerConfig struct {
	APIToken   string
	AdminToken string
	Limiter    *RateLimiter
	TrustProxy bool
	Payment    *PaymentService
}

// NewHandler builds the HTTP surface with fail-closed token authentication.
// Payment callbacks remain HMAC-authenticated by the service itself.
func NewHandler(service *Service, config HandlerConfig) (http.Handler, error) {
	if strings.TrimSpace(config.APIToken) == "" || strings.TrimSpace(config.AdminToken) == "" {
		return nil, ErrAuthNotConfigured
	}
	mux := http.NewServeMux()
	public := func(next http.HandlerFunc) http.Handler { return next }
	api := func(next http.HandlerFunc) http.Handler {
		return requireBearer(next, config.APIToken)
	}
	admin := func(next http.HandlerFunc) http.Handler {
		return requireBearer(next, config.AdminToken)
	}

	mux.Handle("GET /healthz", public(func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	}))
	mux.Handle("POST /v1/orders", api(handleCreateOrder(service)))
	mux.Handle("POST /v1/orders/{id}/payment", public(handlePayment(service)))
	mux.Handle("POST /v1/challenge", public(handleChallenge(service)))
	mux.Handle("POST /v1/activate", public(handleActivate(service)))
	mux.Handle("POST /v1/renew", public(handleRenew(service)))
	mux.Handle("POST /v1/orders/{id}/refund", admin(handleRefund(service, config.Payment)))
	mux.Handle("POST /v1/orders/{id}/chargeback", admin(handleChargeback(service)))
	mux.Handle("GET /v1/subscriptions/{id}/devices", admin(handleDevices(service)))
	mux.Handle("DELETE /v1/subscriptions/{id}/devices/{install_id}", admin(handleRemoveDevice(service)))
	mux.Handle("GET /v1/subscriptions/{id}/epoch", api(handleEpoch(service)))
	if config.Payment != nil {
		mux.Handle("POST /v1/pay/activation-intent", public(handleActivationIntent(config.Payment)))
	}

	var handler http.Handler = mux
	if config.Limiter != nil {
		handler = config.Limiter.Wrap(handler, config.TrustProxy)
	}
	return handler, nil
}

func requireBearer(next http.HandlerFunc, expected string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		provided := strings.TrimSpace(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
		if !secureEqual(provided, expected) {
			w.Header().Set("WWW-Authenticate", "Bearer")
			writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "unauthorized"})
			return
		}
		next(w, r)
	})
}

func secureEqual(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

type RateLimiter struct {
	mu      sync.Mutex
	rate    float64
	burst   float64
	buckets map[string]*rateBucket
	lastGC  time.Time
	now     func() time.Time
}

type rateBucket struct {
	tokens float64
	at     time.Time
}

func NewRateLimiter(perMinute, burst int) *RateLimiter {
	if perMinute <= 0 {
		perMinute = 60
	}
	if burst <= 0 {
		burst = 20
	}
	return &RateLimiter{
		rate:    float64(perMinute) / 60,
		burst:   float64(burst),
		buckets: map[string]*rateBucket{},
		now:     time.Now,
	}
}

func (l *RateLimiter) Allow(key string) bool {
	now := l.now()
	l.mu.Lock()
	defer l.mu.Unlock()
	bucket := l.buckets[key]
	if bucket == nil {
		bucket = &rateBucket{tokens: l.burst, at: now}
		l.buckets[key] = bucket
	}
	if elapsed := now.Sub(bucket.at).Seconds(); elapsed > 0 {
		bucket.tokens += elapsed * l.rate
		if bucket.tokens > l.burst {
			bucket.tokens = l.burst
		}
		bucket.at = now
	}
	if bucket.tokens < 1 {
		return false
	}
	bucket.tokens--
	if l.lastGC.IsZero() || now.Sub(l.lastGC) > time.Minute {
		for key, existing := range l.buckets {
			if now.Sub(existing.at) > 10*time.Minute {
				delete(l.buckets, key)
			}
		}
		l.lastGC = now
	}
	return true
}

func (l *RateLimiter) Wrap(next http.Handler, trustProxy bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/healthz" {
			next.ServeHTTP(w, r)
			return
		}
		if !l.Allow(clientKey(r, trustProxy)) {
			seconds := int(1 / l.rate)
			if seconds < 1 {
				seconds = 1
			}
			w.Header().Set("Retry-After", strconv.Itoa(seconds))
			writeJSON(w, http.StatusTooManyRequests, map[string]any{"error": "rate limit exceeded"})
			return
		}
		next.ServeHTTP(w, r)
	})
}

func clientKey(r *http.Request, trustProxy bool) string {
	if trustProxy {
		forwarded := strings.Split(r.Header.Get("X-Forwarded-For"), ",")[0]
		if forwarded = strings.TrimSpace(forwarded); forwarded != "" {
			return forwarded
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil {
		return host
	}
	return r.RemoteAddr
}
