// NowPay direct mode (alipay.aipay.nowpaydirect.*) is the Vibe Pay「即刻收」
// integration powering the unified checkout for all three plans. The merchant
// configures products, billing modes and tiers in the Alipay「即刻收」mini
// program; the control plane only consults decisions, creates purchase
// entries, and verifies consumable quotas.
package aipay

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/smartwalle/alipay/v3"
)

const (
	MethodNowPayChargeQuery     = "alipay.aipay.nowpaydirect.charge.query"
	MethodNowPayPurchaseConsult = "alipay.aipay.nowpaydirect.purchase.consult"
	MethodNowPayPurchaseCreate  = "alipay.aipay.nowpaydirect.purchase.create"
	MethodNowPayQuotaQuery      = "alipay.aipay.nowpaydirect.quota.query"
	MethodNowPayQuotaVerify     = "alipay.aipay.nowpaydirect.quota.verify"
	MethodNowPayQuotaRefresh    = "alipay.aipay.nowpaydirect.quota.refresh"
)

// purchase.consult decisions.
const (
	NowPayDecisionAllow            = "ALLOW"
	NowPayDecisionPurchaseRequired = "PURCHASE_REQUIRED"
	NowPayDecisionDeny             = "DENY"
)

// quota.verify / quota.refresh consume statuses.
const (
	NowPayConsumeSucceeded  = "SUCCEEDED"
	NowPayConsumeFailed     = "FAILED"
	NowPayConsumeProcessing = "PROCESSING"
)

var ErrNowPayQuotaInsufficient = errors.New("Alipay now pay quota is insufficient")

// NowPayChargeQueryResult is the verified content of charge.query.
type NowPayChargeQueryResult struct {
	Code             string          `json:"code"`
	Msg              string          `json:"msg"`
	SubCode          string          `json:"sub_code"`
	SubMsg           string          `json:"sub_msg"`
	CapabilityStatus string          `json:"capability_status"`
	BillingMode      string          `json:"billing_mode"`
	QuotaUnit        string          `json:"quota_unit"`
	ManagementURL    string          `json:"management_url"`
	ChargingOptions  json.RawMessage `json:"charging_options"`
}

// NowPayConsultResult is the verified content of purchase.consult.
type NowPayConsultResult struct {
	Code           string `json:"code"`
	Msg            string `json:"msg"`
	SubCode        string `json:"sub_code"`
	SubMsg         string `json:"sub_msg"`
	Decision       string `json:"decision"`
	PurchaseURL    string `json:"purchase_url"`
	PurchaseQRCode string `json:"purchase_qr_code"`
	ValidFrom      string `json:"valid_from"`
	ValidUntil     string `json:"valid_until"`
	ReasonCode     string `json:"reason_code"`
}

// NowPayPurchaseCreateRequest is the input of purchase.create.
type NowPayPurchaseCreateRequest struct {
	OutRequestNo    string
	OutProductID    string
	ExternalBuyerID string
	SKUID           string
	CallbackURL     string
}

// NowPayPurchaseCreateResult is the verified content of purchase.create.
type NowPayPurchaseCreateResult struct {
	Code        string `json:"code"`
	Msg         string `json:"msg"`
	SubCode     string `json:"sub_code"`
	SubMsg      string `json:"sub_msg"`
	PurchaseURL string `json:"purchase_url"`
	ExpireTime  string `json:"expire_time"`
}

// NowPayQuotaQueryResult is the verified content of quota.query.
type NowPayQuotaQueryResult struct {
	Code      string `json:"code"`
	Msg       string `json:"msg"`
	SubCode   string `json:"sub_code"`
	SubMsg    string `json:"sub_msg"`
	QuotaUnit string `json:"quota_unit"`
	Total     int64  `json:"total"`
	Used      int64  `json:"used"`
	Remaining int64  `json:"remaining"`
	AsOfTime  string `json:"as_of_time"`
}

// NowPayQuotaVerifyRequest is the input of quota.verify.
type NowPayQuotaVerifyRequest struct {
	OutRequestNo    string
	OutProductID    string
	ExternalBuyerID string
	Amount          int64
	ConsumeReason   string
}

// NowPayConsumeResult is the verified content of quota.verify / quota.refresh.
type NowPayConsumeResult struct {
	Code          string `json:"code"`
	Msg           string `json:"msg"`
	SubCode       string `json:"sub_code"`
	SubMsg        string `json:"sub_msg"`
	ConsumeStatus string `json:"consume_status"`
	Consumed      int64  `json:"consumed"`
	Remaining     int64  `json:"remaining"`
}

// NowPayChargeQuery queries the product's billing capability and tiers.
func (c *Client) NowPayChargeQuery(ctx context.Context, outProductID string) (NowPayChargeQueryResult, error) {
	if c == nil || c.sdk == nil {
		return NowPayChargeQueryResult{}, ErrNotConfigured
	}
	if strings.TrimSpace(outProductID) == "" {
		return NowPayChargeQueryResult{}, errors.New("out_product_id is required")
	}
	payload := alipay.NewPayload(MethodNowPayChargeQuery).
		AddBizField("out_product_id", outProductID)
	var response NowPayChargeQueryResult
	if err := c.sdk.Request(ctx, payload, &response); err != nil {
		return NowPayChargeQueryResult{}, err
	}
	if response.Code != "10000" {
		return response, gatewayError(response.Code, response.Msg, response.SubCode, response.SubMsg)
	}
	return response, nil
}

// NowPayPurchaseConsult queries the buyout / duration access decision.
func (c *Client) NowPayPurchaseConsult(ctx context.Context, outProductID, externalBuyerID, callbackURL string) (NowPayConsultResult, error) {
	if c == nil || c.sdk == nil {
		return NowPayConsultResult{}, ErrNotConfigured
	}
	if strings.TrimSpace(outProductID) == "" || strings.TrimSpace(externalBuyerID) == "" {
		return NowPayConsultResult{}, errors.New("out_product_id and external_buyer_id are required")
	}
	payload := alipay.NewPayload(MethodNowPayPurchaseConsult).
		AddBizField("out_product_id", outProductID).
		AddBizField("external_buyer_id", externalBuyerID)
	if strings.TrimSpace(callbackURL) != "" {
		payload.AddBizField("callback_url", callbackURL)
	}
	var response NowPayConsultResult
	if err := c.sdk.Request(ctx, payload, &response); err != nil {
		return NowPayConsultResult{}, err
	}
	if response.Code != "10000" {
		return response, gatewayError(response.Code, response.Msg, response.SubCode, response.SubMsg)
	}
	return response, nil
}

// NowPayPurchaseCreate creates a purchase entry with request-level idempotency.
func (c *Client) NowPayPurchaseCreate(ctx context.Context, request NowPayPurchaseCreateRequest) (NowPayPurchaseCreateResult, error) {
	if c == nil || c.sdk == nil {
		return NowPayPurchaseCreateResult{}, ErrNotConfigured
	}
	if strings.TrimSpace(request.OutRequestNo) == "" ||
		strings.TrimSpace(request.OutProductID) == "" ||
		strings.TrimSpace(request.ExternalBuyerID) == "" {
		return NowPayPurchaseCreateResult{}, errors.New("out_request_no, out_product_id and external_buyer_id are required")
	}
	payload := alipay.NewPayload(MethodNowPayPurchaseCreate).
		AddBizField("out_request_no", request.OutRequestNo).
		AddBizField("out_product_id", request.OutProductID).
		AddBizField("external_buyer_id", request.ExternalBuyerID)
	if strings.TrimSpace(request.SKUID) != "" {
		payload.AddBizField("sku_id", request.SKUID)
	}
	if strings.TrimSpace(request.CallbackURL) != "" {
		payload.AddBizField("callback_url", request.CallbackURL)
	}
	var response NowPayPurchaseCreateResult
	if err := c.sdk.Request(ctx, payload, &response); err != nil {
		return NowPayPurchaseCreateResult{}, err
	}
	if response.Code != "10000" {
		return response, gatewayError(response.Code, response.Msg, response.SubCode, response.SubMsg)
	}
	return response, nil
}

// NowPayQuotaQuery queries the consumable quota balance.
func (c *Client) NowPayQuotaQuery(ctx context.Context, outProductID, externalBuyerID string) (NowPayQuotaQueryResult, error) {
	if c == nil || c.sdk == nil {
		return NowPayQuotaQueryResult{}, ErrNotConfigured
	}
	if strings.TrimSpace(outProductID) == "" || strings.TrimSpace(externalBuyerID) == "" {
		return NowPayQuotaQueryResult{}, errors.New("out_product_id and external_buyer_id are required")
	}
	payload := alipay.NewPayload(MethodNowPayQuotaQuery).
		AddBizField("out_product_id", outProductID).
		AddBizField("external_buyer_id", externalBuyerID)
	var response NowPayQuotaQueryResult
	if err := c.sdk.Request(ctx, payload, &response); err != nil {
		return NowPayQuotaQueryResult{}, err
	}
	if response.Code != "10000" {
		return response, gatewayError(response.Code, response.Msg, response.SubCode, response.SubMsg)
	}
	return response, nil
}

// NowPayQuotaVerify consumes quota at the service delivery point. The request
// is idempotent per out_request_no; retry with the same parameters or query
// with NowPayQuotaRefresh when the result is unknown.
func (c *Client) NowPayQuotaVerify(ctx context.Context, request NowPayQuotaVerifyRequest) (NowPayConsumeResult, error) {
	if c == nil || c.sdk == nil {
		return NowPayConsumeResult{}, ErrNotConfigured
	}
	if strings.TrimSpace(request.OutRequestNo) == "" ||
		strings.TrimSpace(request.OutProductID) == "" ||
		strings.TrimSpace(request.ExternalBuyerID) == "" ||
		request.Amount <= 0 {
		return NowPayConsumeResult{}, errors.New("out_request_no, out_product_id, external_buyer_id and positive amount are required")
	}
	payload := alipay.NewPayload(MethodNowPayQuotaVerify).
		AddBizField("out_request_no", request.OutRequestNo).
		AddBizField("out_product_id", request.OutProductID).
		AddBizField("external_buyer_id", request.ExternalBuyerID).
		AddBizField("amount", request.Amount)
	if strings.TrimSpace(request.ConsumeReason) != "" {
		payload.AddBizField("consume_reason", request.ConsumeReason)
	}
	var response NowPayConsumeResult
	if err := c.sdk.Request(ctx, payload, &response); err != nil {
		return NowPayConsumeResult{}, err
	}
	if response.Code != "10000" {
		if response.SubCode == "NOW_PAY_QUOTA_INSUFFICIENT" {
			return response, fmt.Errorf("%w: %s", ErrNowPayQuotaInsufficient, strings.TrimSpace(response.SubMsg))
		}
		return response, gatewayError(response.Code, response.Msg, response.SubCode, response.SubMsg)
	}
	return response, nil
}

// NowPayQuotaRefresh queries the result of a previous quota.verify using the
// original request number; it never creates a new consumption.
func (c *Client) NowPayQuotaRefresh(ctx context.Context, outRequestNo, outProductID, externalBuyerID string) (NowPayConsumeResult, error) {
	if c == nil || c.sdk == nil {
		return NowPayConsumeResult{}, ErrNotConfigured
	}
	if strings.TrimSpace(outRequestNo) == "" ||
		strings.TrimSpace(outProductID) == "" ||
		strings.TrimSpace(externalBuyerID) == "" {
		return NowPayConsumeResult{}, errors.New("out_request_no, out_product_id and external_buyer_id are required")
	}
	payload := alipay.NewPayload(MethodNowPayQuotaRefresh).
		AddBizField("out_request_no", outRequestNo).
		AddBizField("out_product_id", outProductID).
		AddBizField("external_buyer_id", externalBuyerID)
	var response NowPayConsumeResult
	if err := c.sdk.Request(ctx, payload, &response); err != nil {
		return NowPayConsumeResult{}, err
	}
	if response.Code != "10000" {
		return response, gatewayError(response.Code, response.Msg, response.SubCode, response.SubMsg)
	}
	return response, nil
}
