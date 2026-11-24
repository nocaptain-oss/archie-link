package controlplane

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/nocaptain-oss/archie-link/internal/aipay"
	"github.com/nocaptain-oss/archie-link/internal/deviceid"
)

// fakeNowPayGateway 模拟即刻收直连网关：consult 决策可切换，
// quota 余额可被外部推进（模拟买家购买次数包）。
type fakeNowPayGateway struct {
	mu              sync.Mutex
	quotaRemaining  int64
	allowBuyers     map[string]bool
	consultDecision string
	consultErr      error
	quotaErr        error
	createCalls     map[string]int
	verifyCalls     map[string]int
	lastVerify      aipay.NowPayQuotaVerifyRequest
}

func newFakeNowPayGateway(quotaRemaining int64) *fakeNowPayGateway {
	return &fakeNowPayGateway{
		quotaRemaining:  quotaRemaining,
		allowBuyers:     map[string]bool{},
		consultDecision: aipay.NowPayDecisionPurchaseRequired,
		createCalls:     map[string]int{},
		verifyCalls:     map[string]int{},
	}
}

func (f *fakeNowPayGateway) NowPayPurchaseConsult(_ context.Context, outProductID, externalBuyerID, callbackURL string) (aipay.NowPayConsultResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.consultErr != nil {
		return aipay.NowPayConsultResult{}, f.consultErr
	}
	if f.consultDecision == aipay.NowPayDecisionDeny {
		return aipay.NowPayConsultResult{Code: "10000", Decision: aipay.NowPayDecisionDeny, ReasonCode: "RISK_REJECT"}, nil
	}
	if f.allowBuyers[externalBuyerID] {
		return aipay.NowPayConsultResult{Code: "10000", Decision: aipay.NowPayDecisionAllow}, nil
	}
	return aipay.NowPayConsultResult{
		Code: "10000", Decision: aipay.NowPayDecisionPurchaseRequired,
		PurchaseURL:    "https://render.alipay.com/p/s/i/?mx=" + outProductID,
		PurchaseQRCode: "https://render.alipay.com/p/s/i/?mx=" + outProductID + "%26sc=" + callbackURL,
	}, nil
}

func (f *fakeNowPayGateway) NowPayPurchaseCreate(_ context.Context, request aipay.NowPayPurchaseCreateRequest) (aipay.NowPayPurchaseCreateResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.createCalls[request.OutRequestNo]++
	return aipay.NowPayPurchaseCreateResult{
		Code: "10000", PurchaseURL: "https://render.alipay.com/p/s/i/?mx=create",
		ExpireTime: "2026-11-24 12:30:00",
	}, nil
}

func (f *fakeNowPayGateway) NowPayQuotaQuery(_ context.Context, _, _ string) (aipay.NowPayQuotaQueryResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.quotaErr != nil {
		return aipay.NowPayQuotaQueryResult{}, f.quotaErr
	}
	return aipay.NowPayQuotaQueryResult{
		Code: "10000", QuotaUnit: "TIMES", Total: 100, Used: 100 - f.quotaRemaining,
		Remaining: f.quotaRemaining, AsOfTime: "2026-11-24 12:00:00",
	}, nil
}

func (f *fakeNowPayGateway) NowPayQuotaVerify(_ context.Context, request aipay.NowPayQuotaVerifyRequest) (aipay.NowPayConsumeResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.verifyCalls[request.OutRequestNo]++
	f.lastVerify = request
	if f.quotaRemaining < request.Amount {
		return aipay.NowPayConsumeResult{}, aipay.ErrNowPayQuotaInsufficient
	}
	f.quotaRemaining -= request.Amount
	return aipay.NowPayConsumeResult{
		Code: "10000", ConsumeStatus: aipay.NowPayConsumeSucceeded,
		Consumed: request.Amount, Remaining: f.quotaRemaining,
	}, nil
}

func (f *fakeNowPayGateway) addQuota(n int64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.quotaRemaining += n
}

type nowPayFixture struct {
	service *Service
	gateway *fakeNowPayGateway
	pay     *NowPayService
	clock   *mutableClock
}

type mutableClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *mutableClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *mutableClock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

func newNowPayFixture(t *testing.T, quotaRemaining int64) nowPayFixture {
	t.Helper()
	clock := &mutableClock{now: time.Date(2026, 11, 24, 12, 0, 0, 0, time.UTC)}
	_, signing, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	service := NewService("test-key", signing, []byte("secret"), clock.Now)
	gateway := newFakeNowPayGateway(quotaRemaining)
	pay, err := NewNowPayService(service, gateway, NowPayConfig{
		Products: map[string]string{
			"activation-task":  "DIRECT_d556e72d19e53c9eb99708e60538b631",
			"perpetual":        "DIRECT_6baab706772b3e75b85a5024ed799e7e",
			"family-annual-12": "DIRECT_91e4ba51675334238c6a296b13293cd4",
		},
		CallbackURL: "https://nocaptain-oss.github.io/archie-link/",
		PayWindow:   30 * time.Minute,
	})
	if err != nil {
		t.Fatal(err)
	}
	return nowPayFixture{service: service, gateway: gateway, pay: pay, clock: clock}
}

func (f nowPayFixture) request(t *testing.T, requestID, plan, installID string) PageIntentRequest {
	t.Helper()
	device := deviceid.New(deviceid.FileStore{Path: t.TempDir() + "/device.key"}, installID)
	identity, err := device.LoadOrCreate(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return PageIntentRequest{
		RequestID: requestID, Plan: plan, InstallID: installID,
		DevicePublicKey: identity.PublicKey,
	}
}

func TestNowPayBeginIsIdempotentPerRequestID(t *testing.T) {
	f := newNowPayFixture(t, 0)
	request := f.request(t, "buy-perp-1", "perpetual", "install-1")
	first, err := f.pay.Begin(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if first.Paid || first.Status != "created" {
		t.Fatalf("status = %q paid = %t", first.Status, first.Paid)
	}
	if first.Amount != "199.00" || first.Currency != "CNY" {
		t.Fatalf("amount = %q %q", first.Amount, first.Currency)
	}
	if first.PayURL == "" || first.PayQRCode == "" {
		t.Fatalf("consult must return purchase url and qr code, got %q / %q", first.PayURL, first.PayQRCode)
	}
	second, err := f.pay.Begin(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if second.OrderID != first.OrderID {
		t.Fatalf("request id replay returned different order: %q vs %q", second.OrderID, first.OrderID)
	}
}

func TestNowPayBeginConflictOnRequestIDReuse(t *testing.T) {
	f := newNowPayFixture(t, 0)
	first := f.request(t, "buy-conflict-1", "perpetual", "install-1")
	if _, err := f.pay.Begin(context.Background(), first); err != nil {
		t.Fatal(err)
	}
	conflicting := f.request(t, "buy-conflict-1", "family-annual-12", "install-1")
	if _, err := f.pay.Begin(context.Background(), conflicting); !errors.Is(err, ErrNowPayConflict) {
		t.Fatalf("err = %v, want ErrNowPayConflict", err)
	}
}

func TestNowPayActivationRecordsBaselineAndConsumesOnArrival(t *testing.T) {
	f := newNowPayFixture(t, 0)
	request := f.request(t, "buy-act-1", "activation-task", "install-act")
	intent, err := f.pay.Begin(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if intent.Paid {
		t.Fatal("activation order must start unpaid when quota balance is zero")
	}
	order := f.service.orders[intent.OrderID]
	if order == nil || order.QuotaBaseline != 0 {
		t.Fatalf("quota baseline = %+v", order)
	}
	// 买家扫码购买次数包：余额 +1。
	f.gateway.addQuota(1)
	checked, err := f.pay.Status(context.Background(), intent.OrderID)
	if err != nil {
		t.Fatal(err)
	}
	if !checked.Paid {
		t.Fatalf("status = %q after quota arrival, want paid", checked.Status)
	}
	if calls := f.gateway.verifyCalls["consume-"+intent.OrderID]; calls != 1 {
		t.Fatalf("quota verify calls = %d, want 1", calls)
	}
	if f.gateway.lastVerify.Amount != 1 {
		t.Fatalf("consume amount = %d, want 1", f.gateway.lastVerify.Amount)
	}
	// 幂等：paid 后再次 Status 不再核销。
	if _, err := f.pay.Status(context.Background(), intent.OrderID); err != nil {
		t.Fatal(err)
	}
	if calls := f.gateway.verifyCalls["consume-"+intent.OrderID]; calls != 1 {
		t.Fatalf("quota verify calls after replay = %d, want 1", calls)
	}
	if sub := f.service.subscriptions[order.SubscriptionID]; sub == nil || sub.Status != "active" {
		t.Fatalf("subscription status = %+v, want active", sub)
	}
}

func TestNowPayActivationDoesNotConsumeBeforeArrival(t *testing.T) {
	f := newNowPayFixture(t, 0)
	// 已有 2 次余额（老用户复购）：基线为 2，未新增前不算新购。
	f.gateway.addQuota(2)
	request := f.request(t, "buy-act-2", "activation-task", "install-act2")
	intent, err := f.pay.Begin(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	order := f.service.orders[intent.OrderID]
	if order == nil || order.QuotaBaseline != 2 {
		t.Fatalf("quota baseline = %+v, want 2", order)
	}
	checked, err := f.pay.Status(context.Background(), intent.OrderID)
	if err != nil {
		t.Fatal(err)
	}
	if checked.Paid {
		t.Fatal("baseline quota must not mark the order paid")
	}
	if calls := f.gateway.verifyCalls["consume-"+intent.OrderID]; calls != 0 {
		t.Fatalf("quota verify calls = %d, want 0", calls)
	}
}

func TestNowPayActivationQuotaQueryFailureFailsClosed(t *testing.T) {
	f := newNowPayFixture(t, 0)
	f.gateway.quotaErr = errors.New("gateway down")
	request := f.request(t, "buy-act-3", "activation-task", "install-act3")
	if _, err := f.pay.Begin(context.Background(), request); err == nil {
		t.Fatal("begin must fail when baseline quota query fails")
	}
	if len(f.service.orders) != 0 {
		t.Fatalf("no order may be created on baseline failure, got %d", len(f.service.orders))
	}
}

func TestNowPayPerpetualConsultAllowMarksPaid(t *testing.T) {
	f := newNowPayFixture(t, 0)
	f.gateway.allowBuyers["install-owned"] = true
	request := f.request(t, "buy-perp-owned", "perpetual", "install-owned")
	intent, err := f.pay.Begin(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if !intent.Paid {
		t.Fatalf("consult ALLOW must mark order paid, status = %q", intent.Status)
	}
	if intent.PayURL != "" || intent.PayQRCode != "" {
		t.Fatalf("no purchase entry expected on ALLOW, got %q / %q", intent.PayURL, intent.PayQRCode)
	}
}

func TestNowPayCheckoutExpiresWithWindow(t *testing.T) {
	f := newNowPayFixture(t, 0)
	request := f.request(t, "buy-fam-1", "family-annual-12", "install-fam")
	intent, err := f.pay.Begin(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	f.clock.advance(31 * time.Minute)
	if _, err := f.pay.Checkout(context.Background(), intent.OrderID); !errors.Is(err, ErrPaymentIntentExpired) {
		t.Fatalf("err = %v, want ErrPaymentIntentExpired", err)
	}
}

func TestNowPayUnboundPlanIsRejected(t *testing.T) {
	f := newNowPayFixture(t, 0)
	pay, err := NewNowPayService(f.service, f.gateway, NowPayConfig{
		Products: map[string]string{"perpetual": "DIRECT_6baab706772b3e75b85a5024ed799e7e"},
	})
	if err != nil {
		t.Fatal(err)
	}
	request := f.request(t, "buy-fam-2", "family-annual-12", "install-fam2")
	if _, err := pay.Begin(context.Background(), request); !errors.Is(err, ErrNowPayProductNotSold) {
		t.Fatalf("err = %v, want ErrNowPayProductNotSold", err)
	}
}

func TestNowPayDeniedConsultFailsBegin(t *testing.T) {
	f := newNowPayFixture(t, 0)
	f.gateway.mu.Lock()
	f.gateway.consultDecision = aipay.NowPayDecisionDeny
	f.gateway.mu.Unlock()
	request := f.request(t, "buy-denied", "perpetual", "install-denied")
	if _, err := f.pay.Begin(context.Background(), request); !errors.Is(err, ErrNowPayDenied) {
		t.Fatalf("err = %v, want ErrNowPayDenied", err)
	}
}

func TestNowPayNewServiceValidatesProducts(t *testing.T) {
	f := newNowPayFixture(t, 0)
	if _, err := NewNowPayService(f.service, f.gateway, NowPayConfig{}); !errors.Is(err, ErrNowPayNotConfigured) {
		t.Fatalf("err = %v, want ErrNowPayNotConfigured", err)
	}
	if _, err := NewNowPayService(f.service, f.gateway, NowPayConfig{
		Products: map[string]string{"unknown-plan": "DIRECT_x"},
	}); !errors.Is(err, ErrNowPayInvalid) {
		t.Fatalf("err = %v, want ErrNowPayInvalid", err)
	}
}
