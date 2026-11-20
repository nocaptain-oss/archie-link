package aipay

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"
	"time"
)

func TestClientPrecreateVerifyAndFulfill(t *testing.T) {
	appPriv, appPub := mustRSAKey(t)
	alipayPriv, alipayPub := mustRSAKey(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Errorf("parse form: %v", err)
			http.Error(w, "bad form", http.StatusBadRequest)
			return
		}
		if !verifyRequestSign(t, r, appPub) {
			http.Error(w, "bad request signature", http.StatusBadRequest)
			return
		}
		method := r.Form.Get("method")
		var inner any
		switch method {
		case MethodPrecreate:
			inner = map[string]any{"code": "10000", "msg": "Success", "amt_pre_pay_id": "prepay_1"}
		case MethodVerify:
			inner = map[string]any{
				"code": "10000", "msg": "Success", "trade_no": "trade_1",
				"out_trade_no": "ord_1", "amount": "19.90",
				"resource_id": ActivationContractResourceForTest, "active": true,
			}
		case MethodFulfillment:
			inner = map[string]any{"code": "10000", "msg": "Success", "trade_no": "trade_1"}
		case MethodRefund:
			inner = map[string]any{
				"code": "10000", "msg": "Success", "trade_no": "trade_1",
				"out_trade_no": "ord_1", "fund_change": "Y", "refund_fee": "19.90",
			}
		default:
			http.Error(w, "unknown method", http.StatusBadRequest)
			return
		}
		raw, _ := json.Marshal(inner)
		digest := sha256.Sum256(raw)
		signature, err := rsa.SignPKCS1v15(rand.Reader, alipayPriv, crypto.SHA256, digest[:])
		if err != nil {
			t.Errorf("sign response: %v", err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			strings.ReplaceAll(method, ".", "_") + "_response": json.RawMessage(raw),
			"sign": base64.StdEncoding.EncodeToString(signature),
		})
	}))
	defer server.Close()

	client, err := New(Config{
		AppID: "app-1", PrivateKey: encodePrivateKey(appPriv),
		AlipayPublicKey: encodePublicKey(alipayPub),
		Gateway:         server.URL, Production: false, HTTPClient: server.Client(),
	})
	if err != nil {
		t.Fatal(err)
	}
	prepayID, err := client.Precreate(context.Background(), PrecreateRequest{
		OutTradeNo: "ord_1", ServiceID: "api_mock_service_id", GoodsName: "安全激活",
		Amount: "19.90", PayBefore: time.Now().Add(30 * time.Minute), Currency: "CNY",
	})
	if err != nil || prepayID != "prepay_1" {
		t.Fatalf("prepay=%q err=%v", prepayID, err)
	}
	verified, err := client.Verify(context.Background(), VerifyRequest{
		TradeNo: "trade_1", PaymentProof: "proof-1", ClientSession: "session-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !verified.Active || verified.OutTradeNo != "ord_1" || verified.Amount != "19.90" {
		t.Fatalf("verify=%#v", verified)
	}
	if err := client.ConfirmFulfillment(context.Background(), "trade_1"); err != nil {
		t.Fatal(err)
	}
	refunded, err := client.Refund(context.Background(), RefundRequest{
		TradeNo: "trade_1", OutTradeNo: "ord_1", OutRequestNo: "rf_1",
		Amount: "19.90", Reason: "test refund",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !refunded.Confirmed() || refunded.RefundFee != "19.90" {
		t.Fatalf("refund=%#v", refunded)
	}
}

func TestSellerBillSignature(t *testing.T) {
	appPriv, appPub := mustRSAKey(t)
	_, alipayPub := mustRSAKey(t)
	client, err := New(Config{
		AppID: "app-1", PrivateKey: encodePrivateKey(appPriv),
		AlipayPublicKey: encodePublicKey(alipayPub),
	})
	if err != nil {
		t.Fatal(err)
	}
	signature, err := client.SignSellerBill(SellerBillFields{
		Amount: "19.90", Currency: "CNY", GoodsName: "安全激活", OutTradeNo: "ord_1",
		PayBefore: "2026-11-06T18:00:00+08:00", ResourceID: "/v1/pay/activation-intent",
		SellerID: "2088000000000000", ServiceID: "API123",
	})
	if err != nil {
		t.Fatal(err)
	}
	content := "amount=19.90&currency=CNY&goods_name=安全激活&out_trade_no=ord_1&pay_before=2026-11-06T18:00:00+08:00&resource_id=/v1/pay/activation-intent&seller_id=2088000000000000&service_id=API123"
	digest := sha256.Sum256([]byte(content))
	raw, err := base64.StdEncoding.DecodeString(signature)
	if err != nil {
		t.Fatal(err)
	}
	if err := rsa.VerifyPKCS1v15(appPub, crypto.SHA256, digest[:], raw); err != nil {
		t.Fatalf("seller signature invalid: %v", err)
	}
}

func TestClientRejectsUnsignedGatewayResponse(t *testing.T) {
	appPriv, _ := mustRSAKey(t)
	_, alipayPub := mustRSAKey(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"alipay_aipay_agent_payment_precreate_response":{"code":"10000","msg":"Success","amt_pre_pay_id":"prepay_1"},"sign":"aW52YWxpZA=="}`))
	}))
	defer server.Close()
	client, err := New(Config{
		AppID: "app-1", PrivateKey: encodePrivateKey(appPriv),
		AlipayPublicKey: encodePublicKey(alipayPub),
		Gateway:         server.URL, HTTPClient: server.Client(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Precreate(context.Background(), PrecreateRequest{
		OutTradeNo: "ord_1", ServiceID: "api_mock_service_id", GoodsName: "安全激活",
		Amount: "19.90", PayBefore: time.Now().Add(30 * time.Minute), Currency: "CNY",
	}); err == nil {
		t.Fatal("unsigned gateway response must be rejected")
	}
}

const ActivationContractResourceForTest = "/v1/pay/activation-intent"

func mustRSAKey(t *testing.T) (*rsa.PrivateKey, *rsa.PublicKey) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	return key, &key.PublicKey
}

func encodePrivateKey(key *rsa.PrivateKey) string {
	return base64.StdEncoding.EncodeToString(x509.MarshalPKCS1PrivateKey(key))
}

func encodePublicKey(key *rsa.PublicKey) string {
	raw, _ := x509.MarshalPKIXPublicKey(key)
	return base64.StdEncoding.EncodeToString(raw)
}

func verifyRequestSign(t *testing.T, r *http.Request, publicKey *rsa.PublicKey) bool {
	t.Helper()
	signature, err := base64.StdEncoding.DecodeString(r.Form.Get("sign"))
	if err != nil {
		t.Errorf("decode request signature: %v", err)
		return false
	}
	pairs := []string{}
	for key, values := range r.Form {
		if key == "sign" {
			continue
		}
		for _, value := range values {
			pairs = append(pairs, key+"="+value)
		}
	}
	sort.Strings(pairs)
	digest := sha256.Sum256([]byte(strings.Join(pairs, "&")))
	if err := rsa.VerifyPKCS1v15(publicKey, crypto.SHA256, digest[:], signature); err != nil {
		t.Errorf("request signature invalid: %v", err)
		return false
	}
	return true
}
