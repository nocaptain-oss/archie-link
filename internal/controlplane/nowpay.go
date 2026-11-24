// NowPayService 是三产品统一收款（Vibe Pay「即刻收」直连模式，
// alipay.aipay.nowpaydirect.*）的控制面：下单（幂等）、购买入口签发、
// consult / quota 轮询确认到账并落账。价格由「即刻收」管理小程序配置，
// 控制面只绑定 plan → out_product_id；客户端不可指定价格。
// 路由与响应形状沿用 page-intent（PageIntent），存量 CLI 零改动。
package controlplane

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/nocaptain-oss/archie-link/internal/aipay"
)

const NowPayResource = "/v1/pay/nowpay-intent"

var (
	ErrNowPayNotConfigured  = errors.New("now pay is not configured")
	ErrNowPayInvalid        = errors.New("now pay request is invalid")
	ErrNowPayConflict       = errors.New("request id is already bound to another order")
	ErrNowPayProductNotSold = errors.New("plan has no now pay product")
	ErrNowPayDenied         = errors.New("Alipay denied the purchase consultation")
)

// NowPayGateway 由 aipay Client 实现；测试用 fake。
type NowPayGateway interface {
	NowPayPurchaseConsult(ctx context.Context, outProductID, externalBuyerID, callbackURL string) (aipay.NowPayConsultResult, error)
	NowPayPurchaseCreate(ctx context.Context, request aipay.NowPayPurchaseCreateRequest) (aipay.NowPayPurchaseCreateResult, error)
	NowPayQuotaQuery(ctx context.Context, outProductID, externalBuyerID string) (aipay.NowPayQuotaQueryResult, error)
	NowPayQuotaVerify(ctx context.Context, request aipay.NowPayQuotaVerifyRequest) (aipay.NowPayConsumeResult, error)
}

type NowPayConfig struct {
	// Products 绑定 plan → out_product_id（即刻收商品 ID）。
	Products map[string]string
	// CallbackURL 是购买页返回跳转地址（可选）。
	CallbackURL string
	// PayWindow 是订单支付窗口（默认 30 分钟，即刻收购买链接默认有效期一致）。
	PayWindow time.Duration
}

type NowPayService struct {
	mu      sync.Mutex
	service *Service
	gateway NowPayGateway
	config  NowPayConfig
}

func NewNowPayService(service *Service, gateway NowPayGateway, config NowPayConfig) (*NowPayService, error) {
	if service == nil || gateway == nil {
		return nil, ErrNowPayNotConfigured
	}
	if len(config.Products) == 0 {
		return nil, fmt.Errorf("%w: no product is bound", ErrNowPayNotConfigured)
	}
	for plan, productID := range config.Products {
		if _, ok := plans[plan]; !ok {
			return nil, fmt.Errorf("%w: unknown plan %q", ErrNowPayInvalid, plan)
		}
		if strings.TrimSpace(productID) == "" {
			return nil, fmt.Errorf("%w: plan %q has an empty product id", ErrNowPayNotConfigured, plan)
		}
	}
	if config.PayWindow <= 0 || config.PayWindow > 2*time.Hour {
		config.PayWindow = 30 * time.Minute
	}
	return &NowPayService{service: service, gateway: gateway, config: config}, nil
}

// productID 返回 plan 绑定的即刻收商品 ID。
func (n *NowPayService) productID(plan string) (string, error) {
	productID := strings.TrimSpace(n.config.Products[plan])
	if productID == "" {
		return "", fmt.Errorf("%w: %s", ErrNowPayProductNotSold, plan)
	}
	return productID, nil
}

// Begin 幂等下单并返回购买入口；同 request_id 重复请求返回同一订单。
func (n *NowPayService) Begin(ctx context.Context, request PageIntentRequest) (PageIntent, error) {
	if err := validatePageIntentRequest(request); err != nil {
		return PageIntent{}, err
	}
	if _, err := n.productID(request.Plan); err != nil {
		return PageIntent{}, err
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	order, err := n.findOrCreateOrder(ctx, request)
	if err != nil {
		return PageIntent{}, err
	}
	return n.checkout(ctx, *order)
}

// Checkout 为已有订单重新签发购买入口（未支付且在窗口内）。
func (n *NowPayService) Checkout(ctx context.Context, orderID string) (PageIntent, error) {
	n.mu.Lock()
	defer n.mu.Unlock()
	order, err := n.orderByID(orderID)
	if err != nil {
		return PageIntent{}, err
	}
	return n.checkout(ctx, *order)
}

// Status 返回订单状态；created 状态下按收费模式向即刻收确认到账。
func (n *NowPayService) Status(ctx context.Context, orderID string) (PageIntent, error) {
	n.mu.Lock()
	defer n.mu.Unlock()
	order, err := n.orderByID(orderID)
	if err != nil {
		return PageIntent{}, err
	}
	if order.Status != "created" {
		return pageIntentFor(*order, "", ""), nil
	}
	if err := n.confirmPaid(ctx, *order); err != nil {
		return PageIntent{}, err
	}
	order, err = n.orderByID(orderID)
	if err != nil {
		return PageIntent{}, err
	}
	return pageIntentFor(*order, "", ""), nil
}

// confirmPaid 按收费模式确认到账：次数包余额超基线后核销 1 次，
// 买断/时长包 consult 决策 ALLOW 即落账；未到账不报错。
func (n *NowPayService) confirmPaid(ctx context.Context, order Order) error {
	productID, err := n.productID(order.Plan)
	if err != nil {
		return err
	}
	if order.Plan == "activation-task" {
		quota, err := n.gateway.NowPayQuotaQuery(ctx, productID, order.InstallID)
		if err != nil {
			return err
		}
		if quota.Remaining <= order.QuotaBaseline {
			return nil
		}
		consume, err := n.gateway.NowPayQuotaVerify(ctx, aipay.NowPayQuotaVerifyRequest{
			OutRequestNo:    "consume-" + order.ID,
			OutProductID:    productID,
			ExternalBuyerID: order.InstallID,
			Amount:          1,
			ConsumeReason:   "ArchieLink 安全激活",
		})
		if err != nil {
			if errors.Is(err, aipay.ErrNowPayQuotaInsufficient) {
				// 基线比较与核销之间余额被其他消费占用；留待下一轮轮询。
				return nil
			}
			return err
		}
		if consume.ConsumeStatus != aipay.NowPayConsumeSucceeded {
			// FAILED / PROCESSING：PROCESSING 用原请求号重试，均不落账。
			return nil
		}
		return n.markPaid(order.ID)
	}
	consult, err := n.gateway.NowPayPurchaseConsult(ctx, productID, order.InstallID, n.config.CallbackURL)
	if err != nil {
		return err
	}
	if consult.Decision == aipay.NowPayDecisionAllow {
		return n.markPaid(order.ID)
	}
	return nil
}

// checkout 为订单签发购买入口：次数包 purchase.create，买断/时长包 consult。
func (n *NowPayService) checkout(ctx context.Context, order Order) (PageIntent, error) {
	if order.Status == "paid" {
		return pageIntentFor(order, "", ""), nil
	}
	if order.Status != "created" {
		return PageIntent{}, ErrNowPayInvalid
	}
	if !n.service.now().Before(order.PayBefore) {
		return PageIntent{}, fmt.Errorf("%w: payment window closed", ErrPaymentIntentExpired)
	}
	productID, err := n.productID(order.Plan)
	if err != nil {
		return PageIntent{}, err
	}
	if order.Plan == "activation-task" {
		created, err := n.gateway.NowPayPurchaseCreate(ctx, aipay.NowPayPurchaseCreateRequest{
			OutRequestNo:    order.ID,
			OutProductID:    productID,
			ExternalBuyerID: order.InstallID,
			CallbackURL:     n.config.CallbackURL,
		})
		if err != nil {
			return PageIntent{}, err
		}
		return pageIntentFor(order, created.PurchaseURL, ""), nil
	}
	consult, err := n.gateway.NowPayPurchaseConsult(ctx, productID, order.InstallID, n.config.CallbackURL)
	if err != nil {
		return PageIntent{}, err
	}
	switch consult.Decision {
	case aipay.NowPayDecisionAllow:
		// 已持有权益（幂等重购 / 恢复购买）：直接落账，不再生成购买入口。
		if err := n.markPaid(order.ID); err != nil {
			return PageIntent{}, err
		}
		updated, err := n.orderByID(order.ID)
		if err != nil {
			return PageIntent{}, err
		}
		return pageIntentFor(*updated, "", ""), nil
	case aipay.NowPayDecisionPurchaseRequired:
		return pageIntentFor(order, consult.PurchaseURL, consult.PurchaseQRCode), nil
	default:
		return PageIntent{}, fmt.Errorf("%w: %s", ErrNowPayDenied, strings.TrimSpace(consult.ReasonCode))
	}
}

func (n *NowPayService) findOrCreateOrder(ctx context.Context, request PageIntentRequest) (*Order, error) {
	n.service.mu.Lock()
	defer n.service.mu.Unlock()
	for _, order := range n.service.orders {
		if order.RequestID == request.RequestID {
			if order.Plan != request.Plan || order.InstallID != request.InstallID ||
				order.DevicePublicKey != request.DevicePublicKey {
				return nil, ErrNowPayConflict
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
		return nil, fmt.Errorf("%w: %v", ErrNowPayInvalid, err)
	}
	// 次数包下单前记录余额基线；查询失败不创建订单（fail closed）。
	var baseline int64
	productID, err := n.productID(request.Plan)
	if err != nil {
		return nil, err
	}
	if request.Plan == "activation-task" {
		quota, err := n.gateway.NowPayQuotaQuery(ctx, productID, request.InstallID)
		if err != nil {
			return nil, err
		}
		baseline = quota.Remaining
	}
	backup := n.service.cloneStateLocked()
	now := n.service.now()
	order := &Order{
		ID: newID("ord_"), SubscriptionID: newID("sub_"), Plan: request.Plan,
		Status: "created", CreatedAt: now, RequestID: request.RequestID,
		InstallID: request.InstallID, DevicePublicKey: request.DevicePublicKey,
		ResourceID: NowPayResource, Amount: amount, Currency: "CNY",
		PayBefore: now.Add(n.config.PayWindow), QuotaBaseline: baseline,
	}
	n.service.orders[order.ID] = order
	n.service.subscriptions[order.SubscriptionID] = &Subscription{
		ID: order.SubscriptionID, Plan: order.Plan, Status: "pending_payment",
		YearStart: order.CreatedAt, Devices: map[string]Device{}, ProcessedEvents: map[string]bool{},
	}
	if err := n.service.persistLocked(); err != nil {
		n.service.restoreStateLocked(backup)
		return nil, err
	}
	copied := *order
	return &copied, nil
}

func (n *NowPayService) markPaid(orderID string) error {
	n.service.mu.Lock()
	defer n.service.mu.Unlock()
	order := n.service.orders[orderID]
	if order == nil {
		return ErrOrderNotFound
	}
	if order.Status == "paid" {
		return nil
	}
	if order.Status != "created" {
		return ErrNowPayInvalid
	}
	backup := n.service.cloneStateLocked()
	order.Status = "paid"
	if sub := n.service.subscriptions[order.SubscriptionID]; sub != nil {
		sub.Status = "active"
	}
	if err := n.service.persistLocked(); err != nil {
		n.service.restoreStateLocked(backup)
		return err
	}
	return nil
}

func (n *NowPayService) orderByID(orderID string) (*Order, error) {
	n.service.mu.Lock()
	defer n.service.mu.Unlock()
	order := n.service.orders[orderID]
	if order == nil {
		return nil, ErrOrderNotFound
	}
	copied := *order
	return &copied, nil
}
