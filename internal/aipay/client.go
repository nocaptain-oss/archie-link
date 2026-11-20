// Package aipay is the minimal Alipay AI usage-based payment client used by
// the control plane. It intentionally exposes only precreate, payment proof
// verification and fulfillment confirmation.
package aipay

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/smartwalle/alipay/v3"
)

const (
	MethodPrecreate   = "alipay.aipay.agent.payment.precreate"
	MethodVerify      = "alipay.aipay.agent.payment.verify"
	MethodFulfillment = "alipay.aipay.agent.fulfillment.confirm"
	MethodRefund      = "alipay.trade.refund"
)

var (
	ErrNotConfigured = errors.New("Alipay AI payment is not configured")
	ErrGateway       = errors.New("Alipay gateway rejected the request")
)

type Config struct {
	AppID           string
	PrivateKey      string
	AlipayPublicKey string
	Gateway         string
	Production      bool
	HTTPClient      *http.Client
}

type Client struct {
	sdk        *alipay.Client
	privateKey *rsa.PrivateKey
}

type PrecreateRequest struct {
	OutTradeNo string
	ServiceID  string
	GoodsName  string
	Amount     string
	PayBefore  time.Time
	Currency   string
	Memo       string
}

type VerifyRequest struct {
	TradeNo       string
	PaymentProof  string
	ClientSession string
}

type VerifyResult struct {
	Code       string
	Msg        string
	SubCode    string
	SubMsg     string
	TradeNo    string
	OutTradeNo string
	Amount     string
	ResourceID string
	Active     bool
}

type SellerBillFields struct {
	Amount     string
	Currency   string
	GoodsName  string
	OutTradeNo string
	PayBefore  string
	ResourceID string
	SellerID   string
	ServiceID  string
}

type RefundRequest struct {
	TradeNo      string
	OutTradeNo   string
	OutRequestNo string
	Amount       string
	Reason       string
}

type RefundResult struct {
	Code       string
	Msg        string
	SubCode    string
	SubMsg     string
	TradeNo    string
	OutTradeNo string
	FundChange string
	RefundFee  string
}

func (r RefundResult) Confirmed() bool {
	return r.Code == "10000" && r.FundChange == "Y"
}

func New(config Config) (*Client, error) {
	if strings.TrimSpace(config.AppID) == "" ||
		strings.TrimSpace(config.PrivateKey) == "" ||
		strings.TrimSpace(config.AlipayPublicKey) == "" {
		return nil, ErrNotConfigured
	}
	privateKey, err := parsePrivateKey(config.PrivateKey)
	if err != nil {
		return nil, fmt.Errorf("parse Alipay private key: %w", err)
	}
	options := []alipay.OptionFunc{}
	if config.HTTPClient != nil {
		options = append(options, alipay.WithHTTPClient(config.HTTPClient))
	}
	if strings.TrimSpace(config.Gateway) != "" {
		if config.Production {
			options = append(options, alipay.WithProductionGateway(config.Gateway))
		} else {
			options = append(options, alipay.WithSandboxGateway(config.Gateway))
		}
	}
	sdk, err := alipay.New(config.AppID, config.PrivateKey, config.Production, options...)
	if err != nil {
		return nil, fmt.Errorf("initialize Alipay SDK: %w", err)
	}
	if err := sdk.LoadAliPayPublicKey(config.AlipayPublicKey); err != nil {
		return nil, fmt.Errorf("load Alipay public key: %w", err)
	}
	if config.HTTPClient == nil {
		sdk.Client = &http.Client{Timeout: 20 * time.Second}
	}
	return &Client{sdk: sdk, privateKey: privateKey}, nil
}

func (c *Client) Precreate(ctx context.Context, request PrecreateRequest) (string, error) {
	if c == nil || c.sdk == nil {
		return "", ErrNotConfigured
	}
	payload := alipay.NewPayload(MethodPrecreate).
		AddBizField("out_trade_no", request.OutTradeNo).
		AddBizField("service_id", request.ServiceID).
		AddBizField("goods_name", request.GoodsName).
		AddBizField("amount", request.Amount).
		AddBizField("pay_before", request.PayBefore.Format(time.RFC3339)).
		AddBizField("currency", request.Currency)
	if request.Memo != "" {
		payload.AddBizField("memo", request.Memo)
	}
	var response struct {
		Code        string `json:"code"`
		Msg         string `json:"msg"`
		SubCode     string `json:"sub_code"`
		SubMsg      string `json:"sub_msg"`
		AmtPrePayID string `json:"amt_pre_pay_id"`
	}
	if err := c.sdk.Request(ctx, payload, &response); err != nil {
		return "", err
	}
	if response.Code != "10000" || strings.TrimSpace(response.AmtPrePayID) == "" {
		return "", gatewayError(response.Code, response.Msg, response.SubCode, response.SubMsg)
	}
	return response.AmtPrePayID, nil
}

func (c *Client) Verify(ctx context.Context, request VerifyRequest) (VerifyResult, error) {
	if c == nil || c.sdk == nil {
		return VerifyResult{}, ErrNotConfigured
	}
	payload := alipay.NewPayload(MethodVerify).
		AddBizField("trade_no", request.TradeNo).
		AddBizField("payment_proof", request.PaymentProof)
	if request.ClientSession != "" {
		payload.AddBizField("client_session", request.ClientSession)
	}
	var response struct {
		Code       string `json:"code"`
		Msg        string `json:"msg"`
		SubCode    string `json:"sub_code"`
		SubMsg     string `json:"sub_msg"`
		TradeNo    string `json:"trade_no"`
		OutTradeNo string `json:"out_trade_no"`
		Amount     string `json:"amount"`
		ResourceID string `json:"resource_id"`
		Active     bool   `json:"active"`
	}
	if err := c.sdk.Request(ctx, payload, &response); err != nil {
		return VerifyResult{}, err
	}
	if response.Code != "10000" {
		return VerifyResult{}, gatewayError(response.Code, response.Msg, response.SubCode, response.SubMsg)
	}
	return VerifyResult{
		Code: response.Code, Msg: response.Msg, SubCode: response.SubCode, SubMsg: response.SubMsg,
		TradeNo: response.TradeNo, OutTradeNo: response.OutTradeNo,
		Amount: response.Amount, ResourceID: response.ResourceID, Active: response.Active,
	}, nil
}

func (c *Client) ConfirmFulfillment(ctx context.Context, tradeNo string) error {
	if c == nil || c.sdk == nil {
		return ErrNotConfigured
	}
	payload := alipay.NewPayload(MethodFulfillment).AddBizField("trade_no", tradeNo)
	var response struct {
		Code    string `json:"code"`
		Msg     string `json:"msg"`
		SubCode string `json:"sub_code"`
		SubMsg  string `json:"sub_msg"`
	}
	if err := c.sdk.Request(ctx, payload, &response); err != nil {
		return err
	}
	if response.Code != "10000" {
		return gatewayError(response.Code, response.Msg, response.SubCode, response.SubMsg)
	}
	return nil
}

func (c *Client) Refund(ctx context.Context, request RefundRequest) (RefundResult, error) {
	if c == nil || c.sdk == nil {
		return RefundResult{}, ErrNotConfigured
	}
	if strings.TrimSpace(request.TradeNo) == "" ||
		strings.TrimSpace(request.OutRequestNo) == "" ||
		strings.TrimSpace(request.Amount) == "" {
		return RefundResult{}, errors.New("trade_no, out_request_no and amount are required")
	}
	response, err := c.sdk.TradeRefund(ctx, alipay.TradeRefund{
		TradeNo:      request.TradeNo,
		OutTradeNo:   request.OutTradeNo,
		RefundAmount: request.Amount,
		RefundReason: request.Reason,
		OutRequestNo: request.OutRequestNo,
		QueryOptions: []string{"refund_detail_item_list"},
	})
	if err != nil {
		return RefundResult{}, err
	}
	if response == nil {
		return RefundResult{}, fmt.Errorf("%w: empty refund response", ErrGateway)
	}
	code := string(response.Code)
	if code != "10000" {
		return RefundResult{}, gatewayError(code, response.Msg, response.SubCode, response.SubMsg)
	}
	return RefundResult{
		Code: code, Msg: response.Msg, SubCode: response.SubCode, SubMsg: response.SubMsg,
		TradeNo: response.TradeNo, OutTradeNo: response.OutTradeNo,
		FundChange: response.FundChange, RefundFee: response.RefundFee,
	}, nil
}

func (c *Client) SignSellerBill(fields SellerBillFields) (string, error) {
	if c == nil || c.privateKey == nil {
		return "", ErrNotConfigured
	}
	content := strings.Join([]string{
		"amount=" + fields.Amount,
		"currency=" + fields.Currency,
		"goods_name=" + fields.GoodsName,
		"out_trade_no=" + fields.OutTradeNo,
		"pay_before=" + fields.PayBefore,
		"resource_id=" + fields.ResourceID,
		"seller_id=" + fields.SellerID,
		"service_id=" + fields.ServiceID,
	}, "&")
	digest := sha256.Sum256([]byte(content))
	signature, err := rsa.SignPKCS1v15(rand.Reader, c.privateKey, crypto.SHA256, digest[:])
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(signature), nil
}

func gatewayError(code, msg, subCode, subMsg string) error {
	detail := strings.TrimSpace(strings.Join([]string{subCode, subMsg, msg}, ": "))
	return fmt.Errorf("%w: %s (%s)", ErrGateway, detail, code)
}

func parsePrivateKey(value string) (*rsa.PrivateKey, error) {
	value = strings.TrimSpace(value)
	if strings.Contains(value, "-----BEGIN") {
		lines := strings.Split(value, "\n")
		naked := make([]string, 0, len(lines))
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, "-----") {
				continue
			}
			naked = append(naked, line)
		}
		value = strings.Join(naked, "")
	}
	raw, err := base64.StdEncoding.DecodeString(value)
	if err != nil {
		return nil, err
	}
	if key, err := x509.ParsePKCS8PrivateKey(raw); err == nil {
		if rsaKey, ok := key.(*rsa.PrivateKey); ok {
			return rsaKey, nil
		}
	}
	key, err := x509.ParsePKCS1PrivateKey(raw)
	if err != nil {
		return nil, err
	}
	return key, nil
}
