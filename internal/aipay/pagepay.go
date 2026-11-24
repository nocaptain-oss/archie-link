// Page pay (alipay.trade.page.pay, product code FAST_INSTANT_TRADE_PAY) is the
// unified checkout for all three plans. Unlike Machine Pay precreate it has no
// per-service 50 CNY price cap, so it carries perpetual (199.00) and family
// annual (69.00) orders as well as activation-task (19.90).
package aipay

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/smartwalle/alipay/v3"
)

const (
	PagePayProductCode = "FAST_INSTANT_TRADE_PAY"
	// shanghaiZone is the timezone Alipay expects for absolute expiry fields.
	shanghaiZone = 8 * 60 * 60
)

var (
	ErrTradeNotFound = errors.New("Alipay trade does not exist")
)

// PagePayRequest is the input for alipay.trade.page.pay.
type PagePayRequest struct {
	OutTradeNo  string
	Subject     string
	TotalAmount string
	NotifyURL   string
	ReturnURL   string
	// TimeExpire is the absolute payment deadline; zero means no deadline.
	TimeExpire time.Time
}

// TradeNotification is the verified content of an async Alipay notification.
type TradeNotification struct {
	AppID       string
	NotifyType  string
	OutTradeNo  string
	TradeNo     string
	TradeStatus string
	TotalAmount string
	SellerID    string
}

// TradeQueryResult is the verified content of alipay.trade.query.
type TradeQueryResult struct {
	Code        string
	Msg         string
	SubCode     string
	SubMsg      string
	TradeNo     string
	OutTradeNo  string
	TradeStatus string
	TotalAmount string
}

// PagePay builds the checkout URL for alipay.trade.page.pay. The URL is signed
// locally; no network round trip happens here.
func (c *Client) PagePay(request PagePayRequest) (string, error) {
	if c == nil || c.sdk == nil {
		return "", ErrNotConfigured
	}
	if strings.TrimSpace(request.OutTradeNo) == "" ||
		strings.TrimSpace(request.Subject) == "" ||
		strings.TrimSpace(request.TotalAmount) == "" {
		return "", errors.New("out_trade_no, subject and total_amount are required")
	}
	param := alipay.TradePagePay{
		Trade: alipay.Trade{
			Subject:     request.Subject,
			OutTradeNo:  request.OutTradeNo,
			TotalAmount: request.TotalAmount,
			ProductCode: PagePayProductCode,
			NotifyURL:   request.NotifyURL,
			ReturnURL:   request.ReturnURL,
		},
	}
	if !request.TimeExpire.IsZero() {
		shanghai := time.FixedZone("Asia/Shanghai", shanghaiZone)
		param.TimeExpire = request.TimeExpire.In(shanghai).Format("2006-01-02 15:04:05")
	}
	checkout, err := c.sdk.TradePagePay(param)
	if err != nil {
		return "", err
	}
	return checkout.String(), nil
}

// VerifyNotify verifies the signature of an async Alipay notification
// (form-encoded POST) and returns its decoded content.
func (c *Client) VerifyNotify(ctx context.Context, form url.Values) (TradeNotification, error) {
	if c == nil || c.sdk == nil {
		return TradeNotification{}, ErrNotConfigured
	}
	notification, err := c.sdk.DecodeNotification(ctx, form)
	if err != nil {
		return TradeNotification{}, fmt.Errorf("verify notification signature: %w", err)
	}
	if notification == nil {
		return TradeNotification{}, errors.New("empty notification")
	}
	return TradeNotification{
		AppID:       notification.AppId,
		NotifyType:  notification.NotifyType,
		OutTradeNo:  notification.OutTradeNo,
		TradeNo:     notification.TradeNo,
		TradeStatus: string(notification.TradeStatus),
		TotalAmount: notification.TotalAmount,
		SellerID:    notification.SellerId,
	}, nil
}

// TradeQuery queries a trade by merchant order number (alipay.trade.query).
func (c *Client) TradeQuery(ctx context.Context, outTradeNo string) (TradeQueryResult, error) {
	if c == nil || c.sdk == nil {
		return TradeQueryResult{}, ErrNotConfigured
	}
	if strings.TrimSpace(outTradeNo) == "" {
		return TradeQueryResult{}, errors.New("out_trade_no is required")
	}
	response, err := c.sdk.TradeQuery(ctx, alipay.TradeQuery{OutTradeNo: outTradeNo})
	if err != nil {
		return TradeQueryResult{}, err
	}
	if response == nil {
		return TradeQueryResult{}, fmt.Errorf("%w: empty trade query response", ErrGateway)
	}
	code := string(response.Code)
	if code == "40004" && response.SubCode == "ACQ.TRADE_NOT_EXIST" {
		return TradeQueryResult{}, fmt.Errorf("%w: %s", ErrTradeNotFound, outTradeNo)
	}
	if code != "10000" {
		return TradeQueryResult{}, gatewayError(code, response.Msg, response.SubCode, response.SubMsg)
	}
	return TradeQueryResult{
		Code: code, Msg: response.Msg, SubCode: response.SubCode, SubMsg: response.SubMsg,
		TradeNo: response.TradeNo, OutTradeNo: response.OutTradeNo,
		TradeStatus: string(response.TradeStatus), TotalAmount: response.TotalAmount,
	}, nil
}
