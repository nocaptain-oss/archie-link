package controlplane

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"
	"time"

	"github.com/nocaptain-oss/archie-link/internal/aipay"
	"github.com/nocaptain-oss/archie-link/internal/deviceid"
)

type fakePageGateway struct {
	mu           sync.Mutex
	lastPay      aipay.PagePayRequest
	queryResults map[string]aipay.TradeQueryResult
	notification aipay.TradeNotification
	notifyErr    error
}

func (f *fakePageGateway) PagePay(request aipay.PagePayRequest) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.lastPay = request
	return "https://openapi.alipay.com/gateway.do?biz_content=page", nil
}

func (f *fakePageGateway) VerifyNotify(_ context.Context, _ url.Values) (aipay.TradeNotification, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.notifyErr != nil {
		return aipay.TradeNotification{}, f.notifyErr
	}
	return f.notification, nil
}

func (f *fakePageGateway) TradeQuery(_ context.Context, outTradeNo string) (aipay.TradeQueryResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if result, ok := f.queryResults[outTradeNo]; ok {
		return result, nil
	}
	return aipay.TradeQueryResult{}, fmt.Errorf("%w: %s", aipay.ErrTradeNotFound, outTradeNo)
}

type pageFixture struct {
	service *Service
	gateway *fakePageGateway
	pay     *PagePayService
	now     time.Time
}

func newPageFixture(t *testing.T) pageFixture {
	t.Helper()
	now := time.Date(2026, 11, 23, 12, 0, 0, 0, time.UTC)
	_, signing, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	service := NewService("test-key", signing, []byte("secret"), func() time.Time { return now })
	gateway := &fakePageGateway{queryResults: map[string]aipay.TradeQueryResult{}}
	pay, err := NewPagePayService(service, gateway, PagePayConfig{
		AppID:     "2021007117679181",
		NotifyURL: "https://api.uzphoto.com/v1/pay/notify",
		ReturnURL: "https://api.uzphoto.com/pay/return",
		PayWindow: 30 * time.Minute,
	})
	if err != nil {
		t.Fatal(err)
	}
	return pageFixture{service: service, gateway: gateway, pay: pay, now: now}
}

func (f pageFixture) request(t *testing.T, plan string) PageIntentRequest {
	t.Helper()
	device := deviceid.New(deviceid.FileStore{Path: t.TempDir() + "/device.key"}, "install-1")
	return f.requestFromDevice(t, plan, device)
}

func (f pageFixture) requestFromDevice(t *testing.T, plan string, device *deviceid.Manager) PageIntentRequest {
	t.Helper()
	identity, err := device.LoadOrCreate(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return PageIntentRequest{
		RequestID:       "req-" + plan,
		Plan:            plan,
		InstallID:       "install-1",
		DevicePublicKey: identity.PublicKey,
	}
}

func TestPagePayBeginUsesServerSidePlanAmount(t *testing.T) {
	f := newPageFixture(t)
	for _, plan := range []struct {
		id     string
		amount string
	}{
		{"activation-task", "19.90"},
		{"perpetual", "199.00"},
		{"family-annual-12", "69.00"},
	} {
		request := f.request(t, plan.id)
		intent, err := f.pay.Begin(context.Background(), request)
		if err != nil {
			t.Fatalf("%s: %v", plan.id, err)
		}
		if intent.Amount != plan.amount || intent.Currency != "CNY" {
			t.Fatalf("%s: amount = %q %q, want %q CNY", plan.id, intent.Amount, intent.Currency, plan.amount)
		}
		if intent.Paid || intent.PayURL == "" || intent.Status != "created" {
			t.Fatalf("%s: unexpected intent %#v", plan.id, intent)
		}
		if !intent.PayBefore.Equal(f.now.Add(30 * time.Minute)) {
			t.Fatalf("%s: pay_before = %v", plan.id, intent.PayBefore)
		}
		if f.gateway.lastPay.TotalAmount != plan.amount ||
			f.gateway.lastPay.OutTradeNo != intent.OrderID ||
			f.gateway.lastPay.NotifyURL == "" || f.gateway.lastPay.ReturnURL == "" {
			t.Fatalf("%s: gateway request = %#v", plan.id, f.gateway.lastPay)
		}
	}
}

func TestPagePayBeginIsIdempotentAndConflictsOnReuse(t *testing.T) {
	f := newPageFixture(t)
	request := f.request(t, "perpetual")
	first, err := f.pay.Begin(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	second, err := f.pay.Begin(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if first.OrderID != second.OrderID {
		t.Fatalf("same request_id must reuse the order: %q vs %q", first.OrderID, second.OrderID)
	}
	if len(f.service.orders) != 1 {
		t.Fatalf("expected exactly 1 order, got %d", len(f.service.orders))
	}
	conflicting := request
	conflicting.Plan = "family-annual-12"
	if _, err := f.pay.Begin(context.Background(), conflicting); !errors.Is(err, ErrPagePayConflict) {
		t.Fatalf("plan change on same request_id must conflict, got %v", err)
	}
}

func TestPagePayBeginRejectsUnknownPlanAndBadIdentity(t *testing.T) {
	f := newPageFixture(t)
	request := f.request(t, "perpetual")
	request.Plan = "gold-edition"
	if _, err := f.pay.Begin(context.Background(), request); !errors.Is(err, ErrPlanUnknown) {
		t.Fatalf("unknown plan must be rejected, got %v", err)
	}
	request = f.request(t, "perpetual")
	request.DevicePublicKey = "not-a-key"
	if _, err := f.pay.Begin(context.Background(), request); !errors.Is(err, ErrPagePayInvalid) {
		t.Fatalf("bad device key must be rejected, got %v", err)
	}
	request = f.request(t, "perpetual")
	request.RequestID = "short"
	if _, err := f.pay.Begin(context.Background(), request); !errors.Is(err, ErrPagePayInvalid) {
		t.Fatalf("short request id must be rejected, got %v", err)
	}
}

func paidNotification(orderID, amount string) aipay.TradeNotification {
	return aipay.TradeNotification{
		AppID:       "2021007117679181",
		NotifyType:  "trade_status_sync",
		OutTradeNo:  orderID,
		TradeNo:     "2026112322001",
		TradeStatus: "TRADE_SUCCESS",
		TotalAmount: amount,
	}
}

func TestPagePayNotifyMarksPaidAndIsIdempotent(t *testing.T) {
	f := newPageFixture(t)
	request := f.request(t, "family-annual-12")
	intent, err := f.pay.Begin(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	f.gateway.notification = paidNotification(intent.OrderID, "69.00")
	if err := f.pay.Notify(context.Background(), url.Values{}); err != nil {
		t.Fatal(err)
	}
	order := f.service.orders[intent.OrderID]
	if order.Status != "paid" || order.ProviderTradeNo != "2026112322001" {
		t.Fatalf("order must be paid with trade no, got %#v", order)
	}
	if f.service.subscriptions[order.SubscriptionID].Status != "active" {
		t.Fatal("subscription must be active after payment")
	}
	// 重复通知幂等成功。
	if err := f.pay.Notify(context.Background(), url.Values{}); err != nil {
		t.Fatalf("duplicate notify must succeed: %v", err)
	}
	// 相同订单不同交易号必须拒绝。
	f.gateway.notification = paidNotification(intent.OrderID, "69.00")
	f.gateway.notification.TradeNo = "2026112322002"
	if err := f.pay.Notify(context.Background(), url.Values{}); !errors.Is(err, ErrPagePayMismatch) {
		t.Fatalf("conflicting trade no must be rejected, got %v", err)
	}
}

func TestPagePayNotifyRejectsForgedOrigins(t *testing.T) {
	f := newPageFixture(t)
	request := f.request(t, "activation-task")
	intent, err := f.pay.Begin(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	// 错误 app_id。
	f.gateway.notification = paidNotification(intent.OrderID, "19.90")
	f.gateway.notification.AppID = "2099999999999999"
	if err := f.pay.Notify(context.Background(), url.Values{}); !errors.Is(err, ErrPagePayMismatch) {
		t.Fatalf("wrong app_id must be rejected, got %v", err)
	}
	// 金额不符。
	f.gateway.notification = paidNotification(intent.OrderID, "9.90")
	if err := f.pay.Notify(context.Background(), url.Values{}); !errors.Is(err, ErrPagePayMismatch) {
		t.Fatalf("amount mismatch must be rejected, got %v", err)
	}
	// 未知订单。
	f.gateway.notification = paidNotification("ord_unknown", "19.90")
	if err := f.pay.Notify(context.Background(), url.Values{}); !errors.Is(err, ErrOrderNotFound) {
		t.Fatalf("unknown order must be rejected, got %v", err)
	}
	// 非支付成功状态确认但不入账。
	f.gateway.notification = paidNotification(intent.OrderID, "19.90")
	f.gateway.notification.TradeStatus = "WAIT_BUYER_PAY"
	if err := f.pay.Notify(context.Background(), url.Values{}); err != nil {
		t.Fatal(err)
	}
	if f.service.orders[intent.OrderID].Status != "created" {
		t.Fatal("WAIT_BUYER_PAY must not mark the order paid")
	}
}

func TestPagePayStatusFallsBackToTradeQuery(t *testing.T) {
	f := newPageFixture(t)
	request := f.request(t, "perpetual")
	intent, err := f.pay.Begin(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	// 交易不存在：保持待支付。
	status, err := f.pay.Status(context.Background(), intent.OrderID)
	if err != nil {
		t.Fatal(err)
	}
	if status.Paid || status.Status != "created" {
		t.Fatalf("expected pending status, got %#v", status)
	}
	// 通知丢失，查询兜底入账。
	f.gateway.queryResults[intent.OrderID] = aipay.TradeQueryResult{
		Code: "10000", TradeNo: "2026112322003", OutTradeNo: intent.OrderID,
		TradeStatus: "TRADE_SUCCESS", TotalAmount: "199.00",
	}
	status, err = f.pay.Status(context.Background(), intent.OrderID)
	if err != nil {
		t.Fatal(err)
	}
	if !status.Paid || status.Status != "paid" {
		t.Fatalf("query fallback must mark paid, got %#v", status)
	}
	// 查询金额不符必须拒绝。
	other := f.request(t, "perpetual")
	other.RequestID = "req-perpetual-2"
	otherIntent, err := f.pay.Begin(context.Background(), other)
	if err != nil {
		t.Fatal(err)
	}
	f.gateway.queryResults[otherIntent.OrderID] = aipay.TradeQueryResult{
		Code: "10000", TradeNo: "2026112322004", OutTradeNo: otherIntent.OrderID,
		TradeStatus: "TRADE_SUCCESS", TotalAmount: "199.99",
	}
	if _, err := f.pay.Status(context.Background(), otherIntent.OrderID); !errors.Is(err, ErrPagePayMismatch) {
		t.Fatalf("amount mismatch must be rejected, got %v", err)
	}
}

func TestPagePayPaidOrderFlowsIntoLeaseActivation(t *testing.T) {
	f := newPageFixture(t)
	device := deviceid.New(deviceid.FileStore{Path: t.TempDir() + "/device.key"}, "install-1")
	request := f.requestFromDevice(t, "perpetual", device)
	intent, err := f.pay.Begin(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	f.gateway.notification = paidNotification(intent.OrderID, "199.00")
	if err := f.pay.Notify(context.Background(), url.Values{}); err != nil {
		t.Fatal(err)
	}
	// 支付完成后走标准激活：挑战 → 签名 → 租约（同一设备身份）。
	identity, err := device.LoadOrCreate(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	challenge, err := f.service.IssueChallenge("install-1", identity.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	signature, err := device.Sign(context.Background(), challenge.Value)
	if err != nil {
		t.Fatal(err)
	}
	leaseBytes, err := f.service.Activate(intent.OrderID, "install-1", identity.PublicKey, challenge.ID, base64.StdEncoding.EncodeToString(signature))
	if err != nil {
		t.Fatal(err)
	}
	lease := decodeLease(t, leaseBytes)
	if !containsFeature(lease.Features, FeatureExportPrivate) {
		t.Fatalf("perpetual lease must carry export_private: %v", lease.Features)
	}
}

func TestPagePayHTTPRoutes(t *testing.T) {
	f := newPageFixture(t)
	handler, err := NewHandler(f.service, HandlerConfig{
		APIToken: "api-token", AdminToken: "admin-token", PagePay: f.pay,
	})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(handler)
	defer server.Close()
	request := f.request(t, "activation-task")

	// 下单。
	body, _ := json.Marshal(request)
	response, err := http.Post(server.URL+"/v1/pay/page-intent", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	var intent PageIntent
	if err := json.NewDecoder(response.Body).Decode(&intent); err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusOK || intent.PayURL == "" {
		t.Fatalf("page-intent = %d %#v", response.StatusCode, intent)
	}

	// 异步通知（支付宝表单 POST）→ success。
	f.gateway.notification = paidNotification(intent.OrderID, "19.90")
	form := url.Values{"out_trade_no": {intent.OrderID}}
	response, err = http.PostForm(server.URL+"/v1/pay/notify", form)
	if err != nil {
		t.Fatal(err)
	}
	ack := make([]byte, 32)
	n, _ := response.Body.Read(ack)
	response.Body.Close()
	if response.StatusCode != http.StatusOK || string(ack[:n]) != "success" {
		t.Fatalf("notify ack = %d %q", response.StatusCode, ack[:n])
	}

	// 状态查询。
	response, err = http.Get(server.URL + "/v1/pay/" + intent.OrderID + "/status")
	if err != nil {
		t.Fatal(err)
	}
	var status PageIntent
	if err := json.NewDecoder(response.Body).Decode(&status); err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if !status.Paid || status.Status != "paid" {
		t.Fatalf("status = %#v", status)
	}

	// 未知订单 404。
	response, err = http.Get(server.URL + "/v1/pay/ord_missing/status")
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusNotFound {
		t.Fatalf("missing order = %d, want 404", response.StatusCode)
	}
}
