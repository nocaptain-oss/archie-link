// Package leaseclient 是客户端访问 ArchieLink 控制面的最小 HTTP 客户端：
// 申请一次性挑战、用设备私钥签名、换取签名租约。
package leaseclient

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/nocaptain-oss/archie-link/internal/deviceid"
)

const DefaultServer = "https://api.uzphoto.com"

// ErrPagePayUnavailable 表示控制面未挂载统一收款路由（buy 据此提示未开通）。
var ErrPagePayUnavailable = errors.New("page pay is not open on this control plane")

type Client struct {
	BaseURL   string
	HTTP      *http.Client
	Device    *deviceid.Manager
	InstallID string
	APIToken  string
}

// PageIntent 是统一收款（page pay）订单的收银台意图。
type PageIntent struct {
	OrderID  string `json:"order_id"`
	Plan     string `json:"plan"`
	Status   string `json:"status"`
	Amount   string `json:"amount"`
	Currency string `json:"currency"`
	Paid     bool   `json:"paid"`
	PayURL   string `json:"pay_url,omitempty"`
	// PayQRCode 是即刻收返回的二维码内容（purchase_qr_code），优先于 PayURL 渲染。
	PayQRCode string    `json:"pay_qr_code,omitempty"`
	PayBefore time.Time `json:"pay_before"`
}

// BeginPageIntent 向统一收款通道下单并返回收银台 URL（幂等：同 request_id 返回同一订单）。
func (c *Client) BeginPageIntent(ctx context.Context, requestID, plan string) (PageIntent, error) {
	if c.Device == nil {
		return PageIntent{}, errors.New("device identity is not configured")
	}
	if strings.TrimSpace(requestID) == "" {
		return PageIntent{}, errors.New("request id is required")
	}
	identity, err := c.Device.LoadOrCreate(ctx)
	if err != nil {
		return PageIntent{}, err
	}
	payload := ActivationIntentRequest{
		RequestID: requestID, Plan: plan, InstallID: c.InstallID, DevicePublicKey: identity.PublicKey,
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return PageIntent{}, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.baseURL()+"/v1/pay/page-intent", bytes.NewReader(raw))
	if err != nil {
		return PageIntent{}, err
	}
	request.Header.Set("Content-Type", "application/json")
	if token := strings.TrimSpace(c.APIToken); token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	response, err := c.httpClient().Do(request)
	if err != nil {
		return PageIntent{}, err
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return PageIntent{}, err
	}
	switch response.StatusCode {
	case http.StatusOK:
		var intent PageIntent
		if err := json.Unmarshal(body, &intent); err != nil {
			return PageIntent{}, fmt.Errorf("decode page intent: %w", err)
		}
		return intent, nil
	case http.StatusNotFound:
		return PageIntent{}, fmt.Errorf("control plane: %w (HTTP 404)", ErrPagePayUnavailable)
	default:
		return PageIntent{}, controlPlaneFailure(body, response.StatusCode)
	}
}

// PageStatus 查询统一收款订单状态（服务端用 TradeQuery 兜底确认入账）。
func (c *Client) PageStatus(ctx context.Context, orderID string) (PageIntent, error) {
	if strings.TrimSpace(orderID) == "" {
		return PageIntent{}, errors.New("order id is required")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet,
		c.baseURL()+"/v1/pay/"+orderID+"/status", nil)
	if err != nil {
		return PageIntent{}, err
	}
	if token := strings.TrimSpace(c.APIToken); token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	response, err := c.httpClient().Do(request)
	if err != nil {
		return PageIntent{}, err
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return PageIntent{}, err
	}
	switch response.StatusCode {
	case http.StatusOK:
		var intent PageIntent
		if err := json.Unmarshal(body, &intent); err != nil {
			return PageIntent{}, fmt.Errorf("decode page status: %w", err)
		}
		return intent, nil
	case http.StatusNotFound:
		return PageIntent{}, fmt.Errorf("control plane: %w (HTTP 404)", ErrPagePayUnavailable)
	default:
		return PageIntent{}, controlPlaneFailure(body, response.StatusCode)
	}
}

func controlPlaneFailure(body []byte, status int) error {
	var failure struct {
		Error string `json:"error"`
	}
	if json.Unmarshal(body, &failure) == nil && failure.Error != "" {
		return fmt.Errorf("control plane: %s (HTTP %d)", failure.Error, status)
	}
	return fmt.Errorf("control plane: HTTP %d", status)
}

type challengeReply struct {
	NonceID   string `json:"nonce_id"`
	Challenge string `json:"challenge"`
	ExpiresAt string `json:"expires_at"`
}

type leaseReply struct {
	License json.RawMessage `json:"license"`
}

type ActivationIntentRequest struct {
	RequestID       string `json:"request_id"`
	Plan            string `json:"plan"`
	InstallID       string `json:"install_id"`
	DevicePublicKey string `json:"device_public_key"`
}

type ActivationGrant struct {
	RequestID        string `json:"request_id"`
	OrderID          string `json:"order_id"`
	SubscriptionID   string `json:"subscription_id"`
	Plan             string `json:"plan"`
	ResourceID       string `json:"resource_id"`
	Amount           string `json:"amount"`
	Currency         string `json:"currency"`
	TradeNo          string `json:"trade_no,omitempty"`
	AlreadyFulfilled bool   `json:"already_fulfilled"`
}

type PaymentIntentResult struct {
	PaymentRequired bool
	PaymentNeeded   string
	Grant           ActivationGrant
}

// Activate 用已支付订单换取绑定当前设备的租约。
func (c *Client) Activate(ctx context.Context, orderID string) ([]byte, error) {
	return c.exchange(ctx, "/v1/activate", orderID)
}

// Renew 为已注册设备续租。
func (c *Client) Renew(ctx context.Context, orderID string) ([]byte, error) {
	return c.exchange(ctx, "/v1/renew", orderID)
}

// Epoch 读取订阅当前的撤销 epoch，用于本地 revocation floor。
func (c *Client) Epoch(ctx context.Context, subscriptionID string) (int, error) {
	var reply struct {
		Epoch int `json:"revocation_epoch"`
	}
	if err := c.do(ctx, http.MethodGet, "/v1/subscriptions/"+subscriptionID+"/epoch", nil, &reply); err != nil {
		return 0, err
	}
	return reply.Epoch, nil
}

func (c *Client) BeginActivationIntent(ctx context.Context, requestID, plan string) (PaymentIntentResult, error) {
	if c.Device == nil {
		return PaymentIntentResult{}, errors.New("device identity is not configured")
	}
	if strings.TrimSpace(requestID) == "" {
		return PaymentIntentResult{}, errors.New("request id is required")
	}
	identity, err := c.Device.LoadOrCreate(ctx)
	if err != nil {
		return PaymentIntentResult{}, err
	}
	payload := ActivationIntentRequest{
		RequestID: requestID, Plan: plan, InstallID: c.InstallID, DevicePublicKey: identity.PublicKey,
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return PaymentIntentResult{}, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.baseURL()+"/v1/pay/activation-intent", bytes.NewReader(raw))
	if err != nil {
		return PaymentIntentResult{}, err
	}
	request.Header.Set("Content-Type", "application/json")
	if token := strings.TrimSpace(c.APIToken); token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	response, err := c.httpClient().Do(request)
	if err != nil {
		return PaymentIntentResult{}, err
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return PaymentIntentResult{}, err
	}
	switch response.StatusCode {
	case http.StatusPaymentRequired:
		needed := response.Header.Get("Payment-Needed")
		if needed == "" {
			return PaymentIntentResult{}, errors.New("control plane returned 402 without Payment-Needed")
		}
		return PaymentIntentResult{PaymentRequired: true, PaymentNeeded: needed}, nil
	case http.StatusOK:
		var envelope struct {
			Content ActivationGrant `json:"content"`
		}
		if err := json.Unmarshal(body, &envelope); err != nil {
			return PaymentIntentResult{}, fmt.Errorf("decode activation grant: %w", err)
		}
		return PaymentIntentResult{Grant: envelope.Content}, nil
	default:
		return PaymentIntentResult{}, controlPlaneFailure(body, response.StatusCode)
	}
}

func (c *Client) exchange(ctx context.Context, path, orderID string) ([]byte, error) {
	if c.Device == nil {
		return nil, errors.New("device identity is not configured")
	}
	if strings.TrimSpace(orderID) == "" {
		return nil, errors.New("order id is required")
	}
	identity, err := c.Device.LoadOrCreate(ctx)
	if err != nil {
		return nil, err
	}
	var challenge challengeReply
	if err := c.do(ctx, http.MethodPost, "/v1/challenge", map[string]string{
		"install_id": c.InstallID, "device_public_key": identity.PublicKey,
	}, &challenge); err != nil {
		return nil, err
	}
	value, err := base64.StdEncoding.DecodeString(challenge.Challenge)
	if err != nil {
		return nil, fmt.Errorf("decode challenge: %w", err)
	}
	signature, err := c.Device.Sign(ctx, value)
	if err != nil {
		return nil, err
	}
	var reply leaseReply
	if err := c.do(ctx, http.MethodPost, path, map[string]string{
		"order_id":          orderID,
		"install_id":        c.InstallID,
		"device_public_key": identity.PublicKey,
		"nonce_id":          challenge.NonceID,
		"signature":         base64.StdEncoding.EncodeToString(signature),
	}, &reply); err != nil {
		return nil, err
	}
	if len(reply.License) == 0 {
		return nil, errors.New("control plane returned an empty lease")
	}
	return reply.License, nil
}

func (c *Client) do(ctx context.Context, method, path string, payload any, out any) error {
	var body io.Reader
	if payload != nil {
		raw, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		body = bytes.NewReader(raw)
	}
	request, err := http.NewRequestWithContext(ctx, method, c.baseURL()+path, body)
	if err != nil {
		return err
	}
	if payload != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	if token := strings.TrimSpace(c.APIToken); token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	response, err := c.httpClient().Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return err
	}
	if response.StatusCode != http.StatusOK {
		return controlPlaneFailure(raw, response.StatusCode)
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("decode control plane response: %w", err)
	}
	return nil
}

func (c *Client) baseURL() string {
	base := strings.TrimSpace(c.BaseURL)
	if base == "" {
		base = DefaultServer
	}
	return strings.TrimRight(base, "/")
}

func (c *Client) httpClient() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return &http.Client{Timeout: 20 * time.Second}
}
