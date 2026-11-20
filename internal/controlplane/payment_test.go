package controlplane

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nocaptain-oss/archie-link/internal/aipay"
	"github.com/nocaptain-oss/archie-link/internal/deviceid"
)

type fakePaymentGateway struct {
	precreateCalls int
	verifyCalls    int
	confirmCalls   int
	refundCalls    int
	confirmErr     error
	refundErr      error
	verifyResult   aipay.VerifyResult
	refundResult   aipay.RefundResult
}

func (f *fakePaymentGateway) Precreate(context.Context, aipay.PrecreateRequest) (string, error) {
	f.precreateCalls++
	return "prepay_1", nil
}

func (f *fakePaymentGateway) Verify(context.Context, aipay.VerifyRequest) (aipay.VerifyResult, error) {
	f.verifyCalls++
	return f.verifyResult, nil
}

func (f *fakePaymentGateway) ConfirmFulfillment(context.Context, string) error {
	f.confirmCalls++
	return f.confirmErr
}

func (f *fakePaymentGateway) SignSellerBill(aipay.SellerBillFields) (string, error) {
	return "seller-signature", nil
}

func (f *fakePaymentGateway) Refund(context.Context, aipay.RefundRequest) (aipay.RefundResult, error) {
	f.refundCalls++
	return f.refundResult, f.refundErr
}

func TestPaymentActivationIntentIsIdempotentAndBoundToDevice(t *testing.T) {
	f := newPaymentFixture(t)
	firstBill, first := f.begin(t, f.request)
	if first.precreateCalls != 1 || firstBill.Protocol.OutTradeNo == "" {
		t.Fatalf("first intent: calls=%d bill=%#v", first.precreateCalls, firstBill)
	}
	secondBill, second := f.begin(t, f.request)
	if second.precreateCalls != 1 || secondBill.Protocol.OutTradeNo != firstBill.Protocol.OutTradeNo {
		t.Fatalf("idempotent intent mismatch: calls=%d first=%s second=%s",
			second.precreateCalls, firstBill.Protocol.OutTradeNo, secondBill.Protocol.OutTradeNo)
	}

	grant, err := f.payment.Complete(context.Background(), f.request, paymentProofHeader(t, "trade_1", "proof_1", "session_1"))
	if err != nil {
		t.Fatal(err)
	}
	if grant.OrderID != firstBill.Protocol.OutTradeNo || grant.TradeNo != "trade_1" || grant.AlreadyFulfilled {
		t.Fatalf("grant=%#v", grant)
	}
	if f.gateway.verifyCalls != 1 || f.gateway.confirmCalls != 1 {
		t.Fatalf("verify=%d confirm=%d", f.gateway.verifyCalls, f.gateway.confirmCalls)
	}
	if _, err := f.service.Activate(grant.OrderID, "install-2", f.otherIdentity.PublicKey, "", ""); !errors.Is(err, ErrPaymentIntentMismatch) {
		t.Fatalf("wrong device activation=%v", err)
	}

	replayed, err := f.payment.Complete(context.Background(), f.request, paymentProofHeader(t, "trade_1", "proof_1", "session_1"))
	if err != nil {
		t.Fatal(err)
	}
	if !replayed.AlreadyFulfilled || f.gateway.verifyCalls != 1 || f.gateway.confirmCalls != 1 {
		t.Fatalf("replay=%#v verify=%d confirm=%d", replayed, f.gateway.verifyCalls, f.gateway.confirmCalls)
	}

	challenge, err := f.service.IssueChallenge("install-1", f.identity.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	signature, err := f.device.Sign(context.Background(), challenge.Value)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.Activate(grant.OrderID, "install-1", f.identity.PublicKey,
		challenge.ID, base64.StdEncoding.EncodeToString(signature)); err != nil {
		t.Fatal(err)
	}
	challenge, err = f.service.IssueChallenge("install-1", f.identity.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	signature, err = f.device.Sign(context.Background(), challenge.Value)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.Renew(grant.OrderID, "install-1", f.identity.PublicKey,
		challenge.ID, base64.StdEncoding.EncodeToString(signature)); !errors.Is(err, ErrPlanRenewalNotAllowed) {
		t.Fatalf("activation task renew error=%v", err)
	}
	if err := f.service.Refund(grant.OrderID); err != nil {
		t.Fatal(err)
	}
	if _, _, err := f.payment.Begin(context.Background(), f.request); !errors.Is(err, ErrPaymentIntentInvalid) {
		t.Fatalf("refunded payment intent=%v", err)
	}
}

func TestPaymentRejectsReusedAlipayTrade(t *testing.T) {
	f := newPaymentFixture(t)
	firstBill, _ := f.begin(t, f.request)
	if _, err := f.payment.Complete(context.Background(), f.request,
		paymentProofHeader(t, "trade_1", "proof_1", "session_1")); err != nil {
		t.Fatal(err)
	}
	second := f.request
	second.RequestID = "request-87654321"
	second.InstallID = "install-2"
	second.DevicePublicKey = f.otherIdentity.PublicKey
	secondBill, _ := f.begin(t, second)
	if secondBill.Protocol.OutTradeNo == firstBill.Protocol.OutTradeNo {
		t.Fatal("second intent reused the first order")
	}
	f.gateway.verifyResult.OutTradeNo = secondBill.Protocol.OutTradeNo
	if _, err := f.payment.Complete(context.Background(), second,
		paymentProofHeader(t, "trade_1", "proof_2", "session_1")); !errors.Is(err, ErrPaymentTradeReused) {
		t.Fatalf("reused trade=%v", err)
	}
}

func TestPaymentCannotReviveRefundedIntent(t *testing.T) {
	f := newPaymentFixture(t)
	bill, _ := f.begin(t, f.request)
	if err := f.service.Refund(bill.Protocol.OutTradeNo); err != nil {
		t.Fatal(err)
	}
	if err := f.payment.markPendingConfirm(bill.Protocol.OutTradeNo, "trade_1", "hash"); !errors.Is(err, ErrPaymentIntentInvalid) {
		t.Fatalf("refunded intent revived: %v", err)
	}
}

func TestAmountLimitAndFormatting(t *testing.T) {
	cases := map[string]string{"0.01": "0.01", "19.9": "19.90", "050.00": "50.00"}
	for input, want := range cases {
		got, err := normalizeAmount(input)
		if err != nil || got != want {
			t.Fatalf("normalize(%q)=%q err=%v", input, got, err)
		}
	}
	for _, input := range []string{"0", "0.00", "50.01", "100.00", "1.001"} {
		if _, err := normalizeAmount(input); err == nil {
			t.Fatalf("amount %q must be rejected", input)
		}
	}
}

func TestPaymentConfirmationFailureRetriesWithoutReverify(t *testing.T) {
	f := newPaymentFixture(t)
	bill, _ := f.begin(t, f.request)
	f.gateway.confirmErr = errors.New("temporary")
	proof := paymentProofHeader(t, "trade_1", "proof_1", "session_1")
	if _, err := f.payment.Complete(context.Background(), f.request, proof); !errors.Is(err, ErrFulfillmentFailed) {
		t.Fatalf("confirm failure=%v", err)
	}
	if _, _, err := f.payment.Begin(context.Background(), f.request); !errors.Is(err, ErrPaymentPendingConfirm) {
		t.Fatalf("pending fulfillment begin=%v", err)
	}
	f.gateway.confirmErr = nil
	grant, err := f.payment.Complete(context.Background(), f.request, proof)
	if err != nil {
		t.Fatal(err)
	}
	if grant.OrderID != bill.Protocol.OutTradeNo || f.gateway.verifyCalls != 1 || f.gateway.confirmCalls != 2 {
		t.Fatalf("grant=%#v verify=%d confirm=%d", grant, f.gateway.verifyCalls, f.gateway.confirmCalls)
	}
}

func TestPaymentRefundConfirmsProviderBeforeRevocation(t *testing.T) {
	f := newPaymentFixture(t)
	bill, _ := f.begin(t, f.request)
	f.gateway.verifyResult.OutTradeNo = bill.Protocol.OutTradeNo
	grant, err := f.payment.Complete(context.Background(), f.request,
		paymentProofHeader(t, "trade_1", "proof_1", "session_1"))
	if err != nil {
		t.Fatal(err)
	}
	f.gateway.refundResult = aipay.RefundResult{
		Code: "10000", FundChange: "Y", TradeNo: "trade_1",
		OutTradeNo: bill.Protocol.OutTradeNo, RefundFee: "19.90",
	}
	if err := f.payment.Refund(context.Background(), grant.OrderID); err != nil {
		t.Fatal(err)
	}
	if f.gateway.refundCalls != 1 {
		t.Fatalf("refund calls=%d", f.gateway.refundCalls)
	}
	if f.service.orders[grant.OrderID].Status != "refunded" {
		t.Fatalf("order not refunded: %#v", f.service.orders[grant.OrderID])
	}
	if epoch, _ := f.service.CurrentEpoch(grant.SubscriptionID); epoch != 1 {
		t.Fatalf("epoch=%d", epoch)
	}
	if err := f.payment.Refund(context.Background(), grant.OrderID); err != nil {
		t.Fatal(err)
	}
	if f.gateway.refundCalls != 1 {
		t.Fatal("already refunded order must be idempotent")
	}
}

func TestPaymentRefundFailureOrPendingDoesNotRevokeEntitlement(t *testing.T) {
	f := newPaymentFixture(t)
	bill, _ := f.begin(t, f.request)
	f.gateway.verifyResult.OutTradeNo = bill.Protocol.OutTradeNo
	grant, err := f.payment.Complete(context.Background(), f.request,
		paymentProofHeader(t, "trade_1", "proof_1", "session_1"))
	if err != nil {
		t.Fatal(err)
	}
	f.gateway.refundErr = errors.New("gateway unavailable")
	if err := f.payment.Refund(context.Background(), grant.OrderID); err == nil {
		t.Fatal("provider failure must be returned")
	}
	if f.service.orders[grant.OrderID].Status != "paid" {
		t.Fatal("entitlement changed before confirmed refund")
	}
	f.gateway.refundErr = nil
	f.gateway.refundResult = aipay.RefundResult{Code: "10000", FundChange: "N"}
	if err := f.payment.Refund(context.Background(), grant.OrderID); !errors.Is(err, ErrRefundPending) {
		t.Fatalf("pending refund=%v", err)
	}
	if f.service.orders[grant.OrderID].Status != "paid" {
		t.Fatal("pending refund must not revoke entitlement")
	}
	if epoch, _ := f.service.CurrentEpoch(grant.SubscriptionID); epoch != 0 {
		t.Fatalf("pending refund advanced epoch to %d", epoch)
	}
}

func TestRefundHTTPUsesPaymentProvider(t *testing.T) {
	f := newPaymentFixture(t)
	bill, _ := f.begin(t, f.request)
	f.gateway.verifyResult.OutTradeNo = bill.Protocol.OutTradeNo
	grant, err := f.payment.Complete(context.Background(), f.request,
		paymentProofHeader(t, "trade_1", "proof_1", "session_1"))
	if err != nil {
		t.Fatal(err)
	}
	f.gateway.refundResult = aipay.RefundResult{
		Code: "10000", FundChange: "Y", TradeNo: "trade_1",
		OutTradeNo: bill.Protocol.OutTradeNo,
	}
	handler, err := NewHandler(f.service, HandlerConfig{
		APIToken: "api", AdminToken: "admin", Payment: f.payment,
	})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/v1/orders/"+grant.OrderID+"/refund", nil)
	request.Header.Set("Authorization", "Bearer admin")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if f.gateway.refundCalls != 1 || f.service.orders[grant.OrderID].Status != "refunded" {
		t.Fatalf("refund calls=%d status=%s", f.gateway.refundCalls, f.service.orders[grant.OrderID].Status)
	}
}

func TestActivationIntentHTTP402AndPaymentProof(t *testing.T) {
	f := newPaymentFixture(t)
	handler, err := NewHandler(f.service, HandlerConfig{
		APIToken: "api", AdminToken: "admin", Payment: f.payment,
	})
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(f.request)
	request := func(proof string) *http.Response {
		t.Helper()
		req := httptest.NewRequest(http.MethodPost, ActivationTaskResource, strings.NewReader(string(raw)))
		req.Header.Set("Content-Type", "application/json")
		if proof != "" {
			req.Header.Set("Payment-Proof", proof)
		}
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, req)
		return recorder.Result()
	}
	paymentRequired := request("")
	if paymentRequired.StatusCode != http.StatusPaymentRequired {
		body, _ := io.ReadAll(paymentRequired.Body)
		t.Fatalf("status=%d body=%s", paymentRequired.StatusCode, body)
	}
	if paymentRequired.Header.Get("Cache-Control") != "no-store" {
		t.Fatal("payment response must disable caching")
	}
	encodedBill := paymentRequired.Header.Get("Payment-Needed")
	decoded, err := base64.RawURLEncoding.DecodeString(encodedBill)
	if err != nil {
		t.Fatal(err)
	}
	var bill PaymentNeeded
	if err := json.Unmarshal(decoded, &bill); err != nil {
		t.Fatal(err)
	}
	if bill.Protocol.OutTradeNo == "" || bill.Protocol.Amount != "19.90" ||
		bill.Protocol.ResourceID != ActivationTaskResource {
		t.Fatalf("bill=%#v", bill)
	}
	f.gateway.verifyResult.OutTradeNo = bill.Protocol.OutTradeNo

	paid := request(paymentProofHeader(t, "trade_1", "proof_1", "session_1"))
	if paid.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(paid.Body)
		t.Fatalf("paid status=%d body=%s", paid.StatusCode, body)
	}
	if paid.Header.Get("Payment-Validation") == "" {
		t.Fatal("missing Payment-Validation")
	}
	var resource struct {
		ResourceID string `json:"resource_id"`
		Content    struct {
			OrderID string `json:"order_id"`
		} `json:"content"`
	}
	if err := json.NewDecoder(paid.Body).Decode(&resource); err != nil {
		t.Fatal(err)
	}
	if resource.ResourceID != ActivationTaskResource || resource.Content.OrderID != bill.Protocol.OutTradeNo {
		t.Fatalf("resource=%#v", resource)
	}
}

func TestPaymentRejectsAmountMismatchAndWrongIntentBinding(t *testing.T) {
	f := newPaymentFixture(t)
	f.begin(t, f.request)
	f.gateway.verifyResult.Amount = "18.90"
	if _, err := f.payment.Complete(context.Background(), f.request,
		paymentProofHeader(t, "trade_1", "proof_1", "session_1")); !errors.Is(err, ErrPaymentMismatch) {
		t.Fatalf("amount mismatch=%v", err)
	}

	other := f.request
	other.DevicePublicKey = f.otherIdentity.PublicKey
	if _, _, err := f.payment.Begin(context.Background(), other); !errors.Is(err, ErrPaymentIntentConflict) {
		t.Fatalf("binding conflict=%v", err)
	}
}

type paymentFixture struct {
	service       *Service
	payment       *PaymentService
	gateway       *fakePaymentGateway
	device        *deviceid.Manager
	identity      deviceid.Identity
	otherIdentity deviceid.Identity
	request       ActivationIntentRequest
}

func newPaymentFixture(t *testing.T) paymentFixture {
	t.Helper()
	now := time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC)
	_, signing, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	service := NewService("test-key", signing, []byte("secret"), func() time.Time { return now })
	device := deviceid.New(deviceid.FileStore{Path: filepath.Join(t.TempDir(), "device.key")}, "install-1")
	identity, err := device.LoadOrCreate(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	other := deviceid.New(deviceid.FileStore{Path: filepath.Join(t.TempDir(), "other.key")}, "install-2")
	otherIdentity, err := other.LoadOrCreate(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	gateway := &fakePaymentGateway{verifyResult: aipay.VerifyResult{
		Code: "10000", TradeNo: "trade_1", OutTradeNo: "", Amount: "19.90",
		ResourceID: ActivationTaskResource, Active: true,
	}}
	request := ActivationIntentRequest{
		RequestID: "request-12345678", Plan: ActivationTaskPlan,
		InstallID: "install-1", DevicePublicKey: identity.PublicKey,
	}
	payment, err := NewPaymentService(service, gateway, PaymentConfig{
		Price: "19.90", GoodsName: "ArchieLink 安全激活", Currency: "CNY",
		SellerID: "2088000000000000", SellerName: "汪连 ArchieLink",
		SellerAppID: "app-1", ServiceID: "api_mock_service_id",
	})
	if err != nil {
		t.Fatal(err)
	}
	return paymentFixture{
		service: service, payment: payment, gateway: gateway,
		device: device, identity: identity, otherIdentity: otherIdentity, request: request,
	}
}

func (f paymentFixture) begin(t *testing.T, request ActivationIntentRequest) (PaymentNeeded, *fakePaymentGateway) {
	t.Helper()
	_, bill, err := f.payment.Begin(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if f.gateway.verifyResult.OutTradeNo == "" {
		f.gateway.verifyResult.OutTradeNo = bill.Protocol.OutTradeNo
	}
	return bill, f.gateway
}

func paymentProofHeader(t *testing.T, tradeNo, proof, session string) string {
	t.Helper()
	raw, err := json.Marshal(map[string]any{
		"protocol": map[string]string{"payment_proof": proof, "trade_no": tradeNo},
		"method":   map[string]string{"client_session": session},
	})
	if err != nil {
		t.Fatal(err)
	}
	return base64.RawURLEncoding.EncodeToString(raw)
}
