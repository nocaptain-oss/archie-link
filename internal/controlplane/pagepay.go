// PagePayService 是三产品统一收款（aipay「AI网页应用收款」，alipay.trade.page.pay）
// 的控制面：下单（幂等）、收银台 URL 签发、异步通知验签入账、TradeQuery 兜底确认。
// 金额来自 plans 表（服务端可信），客户端不可指定价格。
package controlplane

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/nocaptain-oss/archie-link/internal/aipay"
)

const PagePayResource = "/v1/pay/page-intent"

var (
	ErrPagePayNotConfigured = errors.New("page pay is not configured")
	ErrPagePayInvalid       = errors.New("page pay request is invalid")
	ErrPagePayConflict      = errors.New("request id is already bound to another order")
	ErrPagePayMismatch      = errors.New("payment does not match the order")
)

// PagePayGateway 由 aipay Client 实现；测试用 fake。
type PagePayGateway interface {
	PagePay(aipay.PagePayRequest) (string, error)
	VerifyNotify(context.Context, url.Values) (aipay.TradeNotification, error)
	TradeQuery(context.Context, string) (aipay.TradeQueryResult, error)
}

type PagePayConfig struct {
	// AppID 用于异步通知的 app_id 核对（防跨应用伪造）。
	AppID string
	// NotifyURL 是支付宝异步通知地址（POST 表单，验签）。
	NotifyURL string
	// ReturnURL 是支付完成后浏览器回跳地址。
	ReturnURL string
	// PayWindow 是订单支付窗口（默认 30 分钟）。
	PayWindow time.Duration
}

type PageIntentRequest struct {
	RequestID       string `json:"request_id"`
	Plan            string `json:"plan"`
	InstallID       string `json:"install_id"`
	DevicePublicKey string `json:"device_public_key"`
}

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

type PagePayService struct {
	mu      sync.Mutex
	service *Service
	gateway PagePayGateway
	config  PagePayConfig
}

func NewPagePayService(service *Service, gateway PagePayGateway, config PagePayConfig) (*PagePayService, error) {
	if service == nil || gateway == nil {
		return nil, ErrPagePayNotConfigured
	}
	if err := validatePagePayConfig(config); err != nil {
		return nil, err
	}
	if config.PayWindow <= 0 || config.PayWindow > 2*time.Hour {
		config.PayWindow = 30 * time.Minute
	}
	return &PagePayService{service: service, gateway: gateway, config: config}, nil
}

func validatePagePayConfig(config PagePayConfig) error {
	if len(config.AppID) == 0 || len(config.AppID) > 32 {
		return errors.New("Alipay app_id is required (at most 32 bytes) for page pay")
	}
	if config.NotifyURL == "" || config.ReturnURL == "" {
		return errors.New("Alipay notify_url and return_url are required for page pay")
	}
	return nil
}

// Begin 幂等下单并返回收银台 URL；同 request_id 重复请求返回同一订单。
func (p *PagePayService) Begin(ctx context.Context, request PageIntentRequest) (PageIntent, error) {
	_ = ctx
	if err := validatePageIntentRequest(request); err != nil {
		return PageIntent{}, err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	order, err := p.findOrCreateOrder(request)
	if err != nil {
		return PageIntent{}, err
	}
	return p.checkout(*order)
}

// Checkout 为已有订单重新签发收银台 URL（未支付且在窗口内）。
func (p *PagePayService) Checkout(orderID string) (PageIntent, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	order, err := p.orderByID(orderID)
	if err != nil {
		return PageIntent{}, err
	}
	return p.checkout(*order)
}

// Status 返回订单状态；未入账时用 alipay.trade.query 兜底确认（通知丢失场景）。
func (p *PagePayService) Status(ctx context.Context, orderID string) (PageIntent, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	order, err := p.orderByID(orderID)
	if err != nil {
		return PageIntent{}, err
	}
	if order.Status != "created" {
		return pageIntentFor(*order, "", ""), nil
	}
	result, err := p.gateway.TradeQuery(ctx, order.ID)
	if err != nil {
		if errors.Is(err, aipay.ErrTradeNotFound) {
			// 买家尚未支付，交易不存在属于正常待支付状态。
			return pageIntentFor(*order, "", ""), nil
		}
		return PageIntent{}, err
	}
	if err := p.applyTradeResult(*order, result.OutTradeNo, result.TradeNo, result.TradeStatus, result.TotalAmount); err != nil {
		return PageIntent{}, err
	}
	order, err = p.orderByID(orderID)
	if err != nil {
		return PageIntent{}, err
	}
	return pageIntentFor(*order, "", ""), nil
}

// Notify 处理支付宝异步通知：验签由 gateway 完成，这里核对 app_id、
// 订单号、金额与交易状态；重复通知幂等返回成功。
func (p *PagePayService) Notify(ctx context.Context, form url.Values) error {
	notification, err := p.gateway.VerifyNotify(ctx, form)
	if err != nil {
		return err
	}
	if notification.AppID != p.config.AppID {
		return ErrPagePayMismatch
	}
	if notification.NotifyType != "" && notification.NotifyType != "trade_status_sync" {
		return ErrPagePayMismatch
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	order, err := p.orderByID(notification.OutTradeNo)
	if err != nil {
		return err
	}
	if order.Status == "paid" {
		if order.ProviderTradeNo != "" && order.ProviderTradeNo != notification.TradeNo {
			return ErrPagePayMismatch
		}
		return nil
	}
	if order.Status != "created" {
		return ErrPagePayInvalid
	}
	if !tradePaidStatus(notification.TradeStatus) {
		// 非支付成功状态（如 WAIT_BUYER_PAY）确认收到但不入账。
		return nil
	}
	return p.applyTradeResult(*order, notification.OutTradeNo, notification.TradeNo, notification.TradeStatus, notification.TotalAmount)
}

// applyTradeResult 校验查询/通知结果与订单一致后入账；非支付成功状态不动作。
func (p *PagePayService) applyTradeResult(order Order, outTradeNo, tradeNo, tradeStatus, totalAmount string) error {
	if outTradeNo != "" && outTradeNo != order.ID {
		return ErrPagePayMismatch
	}
	if !tradePaidStatus(tradeStatus) {
		return nil
	}
	amount, err := normalizePlanAmount(totalAmount)
	if err != nil || amount != order.Amount {
		return ErrPagePayMismatch
	}
	return p.markPaid(order.ID, tradeNo)
}

func (p *PagePayService) markPaid(orderID, tradeNo string) error {
	p.service.mu.Lock()
	defer p.service.mu.Unlock()
	order := p.service.orders[orderID]
	if order == nil {
		return ErrOrderNotFound
	}
	if order.Status == "paid" {
		if order.ProviderTradeNo != "" && tradeNo != "" && order.ProviderTradeNo != tradeNo {
			return ErrPagePayMismatch
		}
		return nil
	}
	if order.Status != "created" {
		return ErrPagePayInvalid
	}
	if tradeNo != "" {
		for id, existing := range p.service.orders {
			if id != orderID && existing.ProviderTradeNo == tradeNo {
				return ErrPaymentTradeReused
			}
		}
	}
	backup := p.service.cloneStateLocked()
	order.Status = "paid"
	order.ProviderTradeNo = tradeNo
	if sub := p.service.subscriptions[order.SubscriptionID]; sub != nil {
		sub.Status = "active"
	}
	if err := p.service.persistLocked(); err != nil {
		p.service.restoreStateLocked(backup)
		return err
	}
	return nil
}

func (p *PagePayService) checkout(order Order) (PageIntent, error) {
	if order.Status == "paid" {
		return pageIntentFor(order, "", ""), nil
	}
	if order.Status != "created" {
		return PageIntent{}, ErrPagePayInvalid
	}
	if !p.service.now().Before(order.PayBefore) {
		return PageIntent{}, fmt.Errorf("%w: payment window closed", ErrPaymentIntentExpired)
	}
	plan, ok := plans[order.Plan]
	if !ok {
		return PageIntent{}, ErrPlanUnknown
	}
	payURL, err := p.gateway.PagePay(aipay.PagePayRequest{
		OutTradeNo:  order.ID,
		Subject:     plan.GoodsName,
		TotalAmount: order.Amount,
		NotifyURL:   p.config.NotifyURL,
		ReturnURL:   p.config.ReturnURL,
		TimeExpire:  order.PayBefore,
	})
	if err != nil {
		return PageIntent{}, err
	}
	return pageIntentFor(order, payURL, ""), nil
}

func (p *PagePayService) findOrCreateOrder(request PageIntentRequest) (*Order, error) {
	p.service.mu.Lock()
	defer p.service.mu.Unlock()
	for _, order := range p.service.orders {
		if order.RequestID == request.RequestID {
			if order.Plan != request.Plan || order.InstallID != request.InstallID ||
				order.DevicePublicKey != request.DevicePublicKey {
				return nil, ErrPagePayConflict
			}
			copied := *order
			return &copied, nil
		}
	}
	plan, ok := plans[request.Plan]
	if !ok {
		return nil, ErrPlanUnknown
	}
	amount, err := normalizePlanAmount(plan.Amount)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrPagePayInvalid, err)
	}
	backup := p.service.cloneStateLocked()
	now := p.service.now()
	order := &Order{
		ID: newID("ord_"), SubscriptionID: newID("sub_"), Plan: request.Plan,
		Status: "created", CreatedAt: now, RequestID: request.RequestID,
		InstallID: request.InstallID, DevicePublicKey: request.DevicePublicKey,
		ResourceID: PagePayResource, Amount: amount, Currency: "CNY",
		PayBefore: now.Add(p.config.PayWindow),
	}
	p.service.orders[order.ID] = order
	p.service.subscriptions[order.SubscriptionID] = &Subscription{
		ID: order.SubscriptionID, Plan: order.Plan, Status: "pending_payment",
		YearStart: order.CreatedAt, Devices: map[string]Device{}, ProcessedEvents: map[string]bool{},
	}
	if err := p.service.persistLocked(); err != nil {
		p.service.restoreStateLocked(backup)
		return nil, err
	}
	copied := *order
	return &copied, nil
}

func (p *PagePayService) orderByID(orderID string) (*Order, error) {
	p.service.mu.Lock()
	defer p.service.mu.Unlock()
	order := p.service.orders[orderID]
	if order == nil {
		return nil, ErrOrderNotFound
	}
	copied := *order
	return &copied, nil
}

func validatePageIntentRequest(request PageIntentRequest) error {
	if !requestIDPattern.MatchString(request.RequestID) ||
		strings.TrimSpace(request.InstallID) == "" ||
		strings.TrimSpace(request.DevicePublicKey) == "" {
		return ErrPagePayInvalid
	}
	if _, ok := plans[request.Plan]; !ok {
		return ErrPlanUnknown
	}
	if _, err := decodePublicKey(request.DevicePublicKey); err != nil {
		return ErrPagePayInvalid
	}
	return nil
}

func pageIntentFor(order Order, payURL, payQRCode string) PageIntent {
	return PageIntent{
		OrderID: order.ID, Plan: order.Plan, Status: order.Status,
		Amount: order.Amount, Currency: order.Currency,
		Paid: order.Status == "paid", PayURL: payURL, PayQRCode: payQRCode,
		PayBefore: order.PayBefore,
	}
}

func tradePaidStatus(status string) bool {
	return status == "TRADE_SUCCESS" || status == "TRADE_FINISHED"
}
