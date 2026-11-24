package cli

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nocaptain-oss/archie-link/internal/aipay"
	"github.com/nocaptain-oss/archie-link/internal/controlplane"
	"github.com/nocaptain-oss/archie-link/internal/deviceid"
	"github.com/nocaptain-oss/archie-link/internal/leaseclient"
	appstate "github.com/nocaptain-oss/archie-link/internal/state"
)

// buyTestGateway 是统一收款网关的测试替身：PagePay 永远签发收银台 URL，
// TradeQuery 只对预置的订单返回已支付。
type buyTestGateway struct {
	mu      sync.Mutex
	queries map[string]aipay.TradeQueryResult
}

func (g *buyTestGateway) PagePay(request aipay.PagePayRequest) (string, error) {
	return "https://cashier.example.test/pay?out_trade_no=" + url.QueryEscape(request.OutTradeNo), nil
}

func (g *buyTestGateway) VerifyNotify(_ context.Context, _ url.Values) (aipay.TradeNotification, error) {
	return aipay.TradeNotification{}, errors.New("notify is not used in buy tests")
}

func (g *buyTestGateway) TradeQuery(_ context.Context, outTradeNo string) (aipay.TradeQueryResult, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if result, ok := g.queries[outTradeNo]; ok {
		return result, nil
	}
	return aipay.TradeQueryResult{}, fmt.Errorf("%w: %s", aipay.ErrTradeNotFound, outTradeNo)
}

func (g *buyTestGateway) markPaid(orderID, planAmount string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.queries[orderID] = aipay.TradeQueryResult{
		OutTradeNo: orderID, TradeNo: "trade-" + orderID,
		TradeStatus: "TRADE_SUCCESS", TotalAmount: planAmount,
	}
}

type buyFixture struct {
	server  *httptest.Server
	service *controlplane.Service
	pagePay *controlplane.PagePayService
	gateway *buyTestGateway
}

func newBuyFixture(t *testing.T, pagePay bool) buyFixture {
	t.Helper()
	now := time.Date(2026, 11, 23, 12, 0, 0, 0, time.UTC)
	_, signing, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	service := controlplane.NewService("test-key", signing, []byte("secret"), func() time.Time { return now })
	config := controlplane.HandlerConfig{APIToken: "api-token", AdminToken: "admin-token"}
	var gateway *buyTestGateway
	if pagePay {
		gateway = &buyTestGateway{queries: map[string]aipay.TradeQueryResult{}}
		pagePayService, err := controlplane.NewPagePayService(service, gateway, controlplane.PagePayConfig{
			AppID:     "2021007117679181",
			NotifyURL: "https://api.uzphoto.com/v1/pay/notify",
			ReturnURL: "https://api.uzphoto.com/pay/return",
			PayWindow: 30 * time.Minute,
		})
		if err != nil {
			t.Fatal(err)
		}
		config.PagePay = pagePayService
		fixture := buyFixture{service: service, pagePay: pagePayService, gateway: gateway}
		fixture.server = newBuyServer(t, service, config)
		return fixture
	}
	fixture := buyFixture{service: service}
	fixture.server = newBuyServer(t, service, config)
	return fixture
}

func newBuyServer(t *testing.T, service *controlplane.Service, config controlplane.HandlerConfig) *httptest.Server {
	t.Helper()
	handler, err := controlplane.NewHandler(service, config)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return server
}

func (f buyFixture) client(t *testing.T, installID string) (*leaseclient.Client, *deviceid.Manager) {
	t.Helper()
	device := deviceid.New(deviceid.FileStore{Path: filepath.Join(t.TempDir(), "device.key")}, installID)
	client := &leaseclient.Client{BaseURL: f.server.URL, Device: device, InstallID: installID}
	return client, device
}

func TestBuyFlowEndToEnd(t *testing.T) {
	f := newBuyFixture(t, true)
	client, device := f.client(t, "install-1")
	identity, err := device.LoadOrCreate(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	const requestID = "buy-e2e-0001"
	// 预下单拿到 order id，并把 TradeQuery 兜底配置为已支付（模拟买家完成支付）。
	intent, err := f.pagePay.Begin(context.Background(), controlplane.PageIntentRequest{
		RequestID: requestID, Plan: "perpetual",
		InstallID: "install-1", DevicePublicKey: identity.PublicKey,
	})
	if err != nil {
		t.Fatal(err)
	}
	f.gateway.markPaid(intent.OrderID, "199.00")

	stateDir := t.TempDir()
	leasePath := filepath.Join(stateDir, "lease.json")
	lifecycle := &LifecycleDependencies{
		Store: appstate.Store{Dir: stateDir}, InstallID: "install-1",
	}
	var stdout, stderr bytes.Buffer
	options := buyOptions{
		plan: "perpetual", requestID: requestID,
		timeout: 5 * time.Second, pollEvery: 20 * time.Millisecond,
	}
	code := runBuyFlow(context.Background(), *client, options, leasePath, lifecycle, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit=%d stderr=%s stdout=%s", code, stderr.String(), stdout.String())
	}
	output := stdout.String()
	if !strings.Contains(output, intent.OrderID) || !strings.Contains(output, "cashier.example.test") {
		t.Fatalf("missing checkout output: %s", output)
	}
	if !strings.Contains(output, "payment confirmed") || !strings.Contains(output, "lease written") {
		t.Fatalf("missing activation output: %s", output)
	}
	if _, err := loadLeaseFile(t, leasePath); err != nil {
		t.Fatalf("lease file: %v", err)
	}
}

func TestBuyFlowJSONEvents(t *testing.T) {
	f := newBuyFixture(t, true)
	client, device := f.client(t, "install-1")
	identity, err := device.LoadOrCreate(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	const requestID = "buy-json-0001"
	intent, err := f.pagePay.Begin(context.Background(), controlplane.PageIntentRequest{
		RequestID: requestID, Plan: "family-annual-12",
		InstallID: "install-1", DevicePublicKey: identity.PublicKey,
	})
	if err != nil {
		t.Fatal(err)
	}
	f.gateway.markPaid(intent.OrderID, "69.00")

	stateDir := t.TempDir()
	leasePath := filepath.Join(stateDir, "lease.json")
	lifecycle := &LifecycleDependencies{
		Store: appstate.Store{Dir: stateDir}, InstallID: "install-1",
	}
	var stdout, stderr bytes.Buffer
	options := buyOptions{
		plan: "family-annual-12", requestID: requestID, jsonOutput: true,
		timeout: 5 * time.Second, pollEvery: 20 * time.Millisecond,
	}
	code := runBuyFlow(context.Background(), *client, options, leasePath, lifecycle, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit=%d stderr=%s stdout=%s", code, stderr.String(), stdout.String())
	}
	lines := strings.Split(strings.TrimSpace(stdout.String()), "\n")
	if len(lines) != 3 {
		t.Fatalf("want 3 json events, got %d: %q", len(lines), stdout.String())
	}
	var intentEvent, paidEvent, leaseEvent map[string]any
	for i, line := range lines {
		decoded := make(map[string]any)
		if err := json.Unmarshal([]byte(line), &decoded); err != nil {
			t.Fatalf("line %d is not json: %q", i, line)
		}
		switch i {
		case 0:
			intentEvent = decoded
		case 1:
			paidEvent = decoded
		case 2:
			leaseEvent = decoded
		}
	}
	if intentEvent["event"] != "intent" || intentEvent["pay_url"] == "" ||
		intentEvent["order_id"] != intent.OrderID || intentEvent["amount"] != "69.00" {
		t.Fatalf("intent event = %#v", intentEvent)
	}
	if paidEvent["event"] != "paid" || paidEvent["order_id"] != intent.OrderID {
		t.Fatalf("paid event = %#v", paidEvent)
	}
	if leaseEvent["license_id"] == "" || leaseEvent["plan"] != "family-annual-12" {
		t.Fatalf("lease event = %#v", leaseEvent)
	}
	if _, err := loadLeaseFile(t, leasePath); err != nil {
		t.Fatalf("lease file: %v", err)
	}
}

func TestBuyFlowTimesOutWhenUnpaid(t *testing.T) {
	f := newBuyFixture(t, true)
	client, _ := f.client(t, "install-1")
	stateDir := t.TempDir()
	leasePath := filepath.Join(stateDir, "lease.json")
	lifecycle := &LifecycleDependencies{
		Store: appstate.Store{Dir: stateDir}, InstallID: "install-1",
	}
	var stdout, stderr bytes.Buffer
	options := buyOptions{
		plan: "perpetual", requestID: "buy-timeout-0001",
		timeout: 250 * time.Millisecond, pollEvery: 30 * time.Millisecond,
	}
	code := runBuyFlow(context.Background(), *client, options, leasePath, lifecycle, &stdout, &stderr)
	if code != 4 {
		t.Fatalf("exit=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stderr.String(), "payment_timeout") ||
		!strings.Contains(stderr.String(), "--request-id buy-timeout-0001") {
		t.Fatalf("stderr=%s", stderr.String())
	}
	if !strings.Contains(stdout.String(), "cashier.example.test") {
		t.Fatalf("pay url missing from stdout: %s", stdout.String())
	}
	if _, err := loadLeaseFile(t, leasePath); err == nil {
		t.Fatal("lease file must not exist after timeout")
	}
}

func TestBuyFlowReportsPagePayUnavailable(t *testing.T) {
	f := newBuyFixture(t, false)
	client, _ := f.client(t, "install-1")
	var stdout, stderr bytes.Buffer
	options := buyOptions{
		plan: "perpetual", requestID: "buy-nochannel-001",
		timeout: time.Second, pollEvery: 20 * time.Millisecond,
	}
	code := runBuyFlow(context.Background(), *client, options,
		filepath.Join(t.TempDir(), "lease.json"), &LifecycleDependencies{InstallID: "install-1"}, &stdout, &stderr)
	if code != 4 {
		t.Fatalf("exit=%d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "page_pay_unavailable") ||
		!strings.Contains(stderr.String(), "该服务端未开通统一收款") {
		t.Fatalf("stderr=%s", stderr.String())
	}
}

func TestBuyRejectsBadPlanFromServer(t *testing.T) {
	f := newBuyFixture(t, true)
	client, _ := f.client(t, "install-1")
	var stdout, stderr bytes.Buffer
	options := buyOptions{
		plan: "no-such-plan", requestID: "buy-badplan-001",
		timeout: time.Second, pollEvery: 20 * time.Millisecond,
	}
	code := runBuyFlow(context.Background(), *client, options,
		filepath.Join(t.TempDir(), "lease.json"), &LifecycleDependencies{InstallID: "install-1"}, &stdout, &stderr)
	if code != 4 || !strings.Contains(stderr.String(), "buy_failed") {
		t.Fatalf("exit=%d stderr=%s", code, stderr.String())
	}
}

func TestBuyUsageAndPlatformGuards(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := RunWithDeps([]string{"buy"}, &stdout, &stderr, Dependencies{}); code != 2 ||
		!strings.Contains(stderr.String(), "usage: archielink buy") {
		t.Fatalf("exit=%d stderr=%s", code, stderr.String())
	}
	stdout.Reset()
	stderr.Reset()
	if code := RunWithDeps([]string{"buy", "perpetual"}, &stdout, &stderr, Dependencies{
		Lifecycle: &LifecycleDependencies{},
	}); code != 3 || !strings.Contains(stderr.String(), "unsupported_platform") {
		t.Fatalf("exit=%d stderr=%s", code, stderr.String())
	}
}

func TestParseBuyOptions(t *testing.T) {
	var stderr bytes.Buffer
	options, ok := parseBuyOptions([]string{
		"perpetual", "--server", "http://127.0.0.1:8790",
		"--request-id", "buy-parse-0001", "--timeout", "15m", "--json",
	}, &stderr)
	if !ok {
		t.Fatalf("parse failed: %s", stderr.String())
	}
	if options.plan != "perpetual" || options.server != "http://127.0.0.1:8790" ||
		options.requestID != "buy-parse-0001" || options.timeout != 15*time.Minute || !options.jsonOutput {
		t.Fatalf("options=%#v", options)
	}
	if _, ok := parseBuyOptions([]string{"perpetual", "--timeout", "abc"}, &stderr); ok {
		t.Fatal("invalid duration must fail")
	}
	if _, ok := parseBuyOptions([]string{"perpetual", "--timeout", "0s"}, &stderr); ok {
		t.Fatal("zero timeout must fail")
	}
	if _, ok := parseBuyOptions([]string{"perpetual", "--bogus"}, &stderr); ok {
		t.Fatal("unknown option must fail")
	}
	if _, ok := parseBuyOptions([]string{"perpetual", "--server"}, &stderr); ok {
		t.Fatal("missing option value must fail")
	}
}

func loadLeaseFile(t *testing.T, path string) (map[string]any, error) {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	envelope := make(map[string]any)
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return nil, err
	}
	return envelope, nil
}
