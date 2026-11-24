package controlplane

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/nocaptain-oss/archie-link/internal/aipay"
)

const (
	ActivationTaskPlan     = "activation-task"
	ActivationTaskResource = "/v1/pay/activation-intent"
)

var (
	ErrPaymentNotConfigured  = errors.New("SkillHub Pay is not configured")
	ErrPaymentIntentInvalid  = errors.New("payment activation intent is invalid")
	ErrPaymentIntentConflict = errors.New("request id is already bound to another intent")
	ErrPaymentIntentExpired  = errors.New("payment activation intent has expired")
	ErrPaymentProofInvalid   = errors.New("Payment-Proof is invalid")
	ErrPaymentMismatch       = errors.New("payment does not match the activation intent")
	ErrPaymentTradeReused    = errors.New("Alipay trade number was already used")
	ErrPaymentPendingConfirm = errors.New("payment is verified but fulfillment confirmation is still pending")
	ErrFulfillmentFailed     = errors.New("Alipay fulfillment confirmation failed")
	ErrRefundPending         = errors.New("Alipay refund is pending confirmation")
)

type PaymentGateway interface {
	Precreate(context.Context, aipay.PrecreateRequest) (string, error)
	Verify(context.Context, aipay.VerifyRequest) (aipay.VerifyResult, error)
	ConfirmFulfillment(context.Context, string) error
	SignSellerBill(aipay.SellerBillFields) (string, error)
	Refund(context.Context, aipay.RefundRequest) (aipay.RefundResult, error)
}

type PaymentConfig struct {
	Price                     string
	GoodsName                 string
	Currency                  string
	SellerID                  string
	SellerName                string
	SellerAppID               string
	ServiceID                 string
	ResourceID                string
	PayWindow                 time.Duration
	AllowSandboxMissingFields bool
}

type ActivationIntentRequest struct {
	RequestID       string `json:"request_id"`
	Plan            string `json:"plan"`
	InstallID       string `json:"install_id"`
	DevicePublicKey string `json:"device_public_key"`
}

type PaymentNeeded struct {
	Protocol PaymentProtocol `json:"protocol"`
	Method   PaymentMethod   `json:"method"`
}

type PaymentProtocol struct {
	OutTradeNo      string `json:"out_trade_no"`
	Amount          string `json:"amount"`
	Currency        string `json:"currency"`
	ResourceID      string `json:"resource_id"`
	PayBefore       string `json:"pay_before"`
	SellerSignature string `json:"seller_signature"`
	SellerSignType  string `json:"seller_sign_type"`
	SellerUniqueID  string `json:"seller_unique_id"`
}

type PaymentMethod struct {
	SellerName        string `json:"seller_name"`
	SellerID          string `json:"seller_id"`
	SellerAppID       string `json:"seller_app_id"`
	GoodsName         string `json:"goods_name"`
	SellerUniqueIDKey string `json:"seller_unique_id_key"`
	ServiceID         string `json:"service_id"`
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

type PaymentService struct {
	mu      sync.Mutex
	service *Service
	gateway PaymentGateway
	config  PaymentConfig
}

func NewPaymentService(service *Service, gateway PaymentGateway, config PaymentConfig) (*PaymentService, error) {
	if service == nil || gateway == nil {
		return nil, ErrPaymentNotConfigured
	}
	price, err := normalizeAmount(config.Price)
	if err != nil || price == "0.00" {
		return nil, fmt.Errorf("invalid activation price: %q", config.Price)
	}
	if config.Currency == "" {
		config.Currency = "CNY"
	}
	if config.Currency != "CNY" {
		return nil, errors.New("Alipay AI payment only supports CNY")
	}
	if config.GoodsName == "" {
		config.GoodsName = "ArchieLink 安全激活"
	}
	if config.SellerName == "" {
		config.SellerName = "汪连 ArchieLink"
	}
	if config.ServiceID == "" {
		return nil, errors.New("Alipay service_id is required")
	}
	if len(config.ServiceID) > 32 || len(config.SellerAppID) > 32 || len(config.SellerID) > 32 {
		return nil, errors.New("Alipay app_id, seller_id and service_id must be at most 32 bytes")
	}
	if config.SellerID == "" || config.SellerAppID == "" {
		return nil, errors.New("Alipay seller_id and seller_app_id are required")
	}
	if config.ResourceID == "" {
		config.ResourceID = ActivationTaskResource
	}
	if len(config.ResourceID) > 32 || utf8.RuneCountInString(config.GoodsName) > 32 {
		return nil, errors.New("Alipay resource_id or goods_name exceeds field limits")
	}
	if config.PayWindow <= 0 || config.PayWindow > 12*time.Hour {
		config.PayWindow = 30 * time.Minute
	}
	config.Price = price
	return &PaymentService{service: service, gateway: gateway, config: config}, nil
}

func (p *PaymentService) Begin(ctx context.Context, request ActivationIntentRequest) (ActivationGrant, PaymentNeeded, error) {
	if err := validateIntentRequest(request); err != nil {
		return ActivationGrant{}, PaymentNeeded{}, err
	}
	p.mu.Lock()
	defer p.mu.Unlock()

	order, err := p.findOrCreateIntent(request)
	if err != nil {
		return ActivationGrant{}, PaymentNeeded{}, err
	}
	switch order.Status {
	case "paid":
		if order.FulfillmentStatus == "fulfilled" {
			return grantForOrder(order, true), PaymentNeeded{}, nil
		}
		return ActivationGrant{}, PaymentNeeded{}, ErrPaymentPendingConfirm
	case "created":
	default:
		return ActivationGrant{}, PaymentNeeded{}, ErrPaymentIntentInvalid
	}
	if order.PayBefore.IsZero() || !p.service.now().Before(order.PayBefore) {
		return ActivationGrant{}, PaymentNeeded{}, ErrPaymentIntentExpired
	}
	if order.ProviderPrePayID == "" {
		prePayID, err := p.gateway.Precreate(ctx, aipay.PrecreateRequest{
			OutTradeNo: order.ID,
			ServiceID:  p.config.ServiceID,
			GoodsName:  p.config.GoodsName,
			Amount:     order.Amount,
			PayBefore:  order.PayBefore,
			Currency:   order.Currency,
		})
		if err != nil {
			return ActivationGrant{}, PaymentNeeded{}, err
		}
		if err := p.storePrepay(order.ID, prePayID); err != nil {
			return ActivationGrant{}, PaymentNeeded{}, err
		}
		order.ProviderPrePayID = prePayID
	}
	bill, err := p.paymentNeeded(*order)
	if err != nil {
		return ActivationGrant{}, PaymentNeeded{}, err
	}
	return ActivationGrant{}, bill, nil
}

func (p *PaymentService) Complete(ctx context.Context, request ActivationIntentRequest, proofHeader string) (ActivationGrant, error) {
	if err := validateIntentRequest(request); err != nil {
		return ActivationGrant{}, err
	}
	proof, err := parsePaymentProof(proofHeader)
	if err != nil {
		return ActivationGrant{}, err
	}
	hash := hashPaymentProof(proof)

	p.mu.Lock()
	defer p.mu.Unlock()

	order, err := p.intentByRequestID(request.RequestID)
	if err != nil {
		return ActivationGrant{}, err
	}
	if order.Plan != request.Plan || order.InstallID != request.InstallID ||
		order.DevicePublicKey != request.DevicePublicKey {
		return ActivationGrant{}, ErrPaymentIntentConflict
	}
	if order.ProviderTradeNo != "" && order.ProviderTradeNo != proof.TradeNo {
		return ActivationGrant{}, ErrPaymentTradeReused
	}
	if order.Status != "created" && order.Status != "paid" {
		return ActivationGrant{}, ErrPaymentIntentInvalid
	}
	if order.ProofHash != "" && order.ProofHash != hash {
		return ActivationGrant{}, ErrPaymentProofInvalid
	}
	if order.FulfillmentStatus == "fulfilled" {
		return grantForOrder(order, true), nil
	}
	if order.FulfillmentStatus != "pending_confirm" {
		result, err := p.gateway.Verify(ctx, aipay.VerifyRequest{
			TradeNo: proof.TradeNo, PaymentProof: proof.PaymentProof, ClientSession: proof.ClientSession,
		})
		if err != nil {
			return ActivationGrant{}, err
		}
		if err := p.validateVerification(*order, proof, result); err != nil {
			return ActivationGrant{}, err
		}
		if err := p.markPendingConfirm(order.ID, result.TradeNo, hash); err != nil {
			return ActivationGrant{}, err
		}
		order, err = p.intentByRequestID(request.RequestID)
		if err != nil {
			return ActivationGrant{}, err
		}
	}
	if err := p.gateway.ConfirmFulfillment(ctx, order.ProviderTradeNo); err != nil {
		return ActivationGrant{}, fmt.Errorf("%w: %v", ErrFulfillmentFailed, err)
	}
	if err := p.markFulfilled(order.ID); err != nil {
		return ActivationGrant{}, err
	}
	order, err = p.intentByRequestID(request.RequestID)
	if err != nil {
		return ActivationGrant{}, err
	}
	return grantForOrder(order, false), nil
}

// Refund calls the provider first. Internal entitlement is revoked only after
// Alipay confirms that funds changed, so provider failures cannot lock users out.
func (p *PaymentService) Refund(ctx context.Context, orderID string) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.service.mu.Lock()
	current := p.service.orders[orderID]
	if current == nil {
		p.service.mu.Unlock()
		return ErrOrderNotFound
	}
	order := *current
	p.service.mu.Unlock()

	switch order.Status {
	case "refunded", "revoked":
		return nil
	case "paid":
		// Continue below.
	default:
		return ErrPaymentIntentInvalid
	}
	if order.ProviderTradeNo == "" {
		return ErrPaymentIntentInvalid
	}
	result, err := p.gateway.Refund(ctx, aipay.RefundRequest{
		TradeNo:      order.ProviderTradeNo,
		OutTradeNo:   order.ID,
		OutRequestNo: refundRequestNo(order.ID),
		Amount:       order.Amount,
		Reason:       "ArchieLink activation refund",
	})
	if err != nil {
		return err
	}
	if !result.Confirmed() {
		return ErrRefundPending
	}
	if result.TradeNo != "" && result.TradeNo != order.ProviderTradeNo {
		return ErrPaymentMismatch
	}
	if result.OutTradeNo != "" && result.OutTradeNo != order.ID {
		return ErrPaymentMismatch
	}
	return p.service.Refund(orderID)
}

func (p *PaymentService) findOrCreateIntent(request ActivationIntentRequest) (*Order, error) {
	p.service.mu.Lock()
	defer p.service.mu.Unlock()
	for _, order := range p.service.orders {
		if order.RequestID == request.RequestID {
			if order.Plan != request.Plan || order.InstallID != request.InstallID ||
				order.DevicePublicKey != request.DevicePublicKey {
				return nil, ErrPaymentIntentConflict
			}
			copied := *order
			return &copied, nil
		}
	}
	backup := p.service.cloneStateLocked()
	now := p.service.now()
	order := &Order{
		ID: newID("ord_"), SubscriptionID: newID("sub_"), Plan: request.Plan,
		Status: "created", CreatedAt: now, RequestID: request.RequestID,
		InstallID: request.InstallID, DevicePublicKey: request.DevicePublicKey,
		ResourceID: p.config.ResourceID, Amount: p.config.Price, Currency: p.config.Currency,
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

func (p *PaymentService) storePrepay(orderID, prePayID string) error {
	p.service.mu.Lock()
	defer p.service.mu.Unlock()
	order := p.service.orders[orderID]
	if order == nil {
		return ErrOrderNotFound
	}
	backup := p.service.cloneStateLocked()
	order.ProviderPrePayID = prePayID
	if err := p.service.persistLocked(); err != nil {
		p.service.restoreStateLocked(backup)
		return err
	}
	return nil
}

func (p *PaymentService) paymentNeeded(order Order) (PaymentNeeded, error) {
	payBefore := order.PayBefore.Format(time.RFC3339)
	signature, err := p.gateway.SignSellerBill(aipay.SellerBillFields{
		Amount: order.Amount, Currency: order.Currency, GoodsName: p.config.GoodsName,
		OutTradeNo: order.ID, PayBefore: payBefore, ResourceID: order.ResourceID,
		SellerID: p.config.SellerID, ServiceID: p.config.ServiceID,
	})
	if err != nil {
		return PaymentNeeded{}, err
	}
	return PaymentNeeded{
		Protocol: PaymentProtocol{
			OutTradeNo: order.ID, Amount: order.Amount, Currency: order.Currency,
			ResourceID: order.ResourceID, PayBefore: payBefore,
			SellerSignature: signature, SellerSignType: "RSA2", SellerUniqueID: p.config.SellerID,
		},
		Method: PaymentMethod{
			SellerName: p.config.SellerName, SellerID: p.config.SellerID,
			SellerAppID: p.config.SellerAppID, GoodsName: p.config.GoodsName,
			SellerUniqueIDKey: "seller_id", ServiceID: p.config.ServiceID,
		},
	}, nil
}

func (p *PaymentService) validateVerification(order Order, proof paymentProof, result aipay.VerifyResult) error {
	if !result.Active || result.TradeNo != proof.TradeNo ||
		result.OutTradeNo != order.ID {
		return ErrPaymentMismatch
	}
	verifiedAmount := result.Amount
	if verifiedAmount == "" && p.config.AllowSandboxMissingFields {
		verifiedAmount = order.Amount
	}
	verifiedResource := result.ResourceID
	if verifiedResource == "" && p.config.AllowSandboxMissingFields {
		verifiedResource = order.ResourceID
	}
	amount, err := normalizeAmount(verifiedAmount)
	if err != nil || amount != order.Amount {
		return ErrPaymentMismatch
	}
	if verifiedResource != order.ResourceID {
		return ErrPaymentMismatch
	}
	if p.service.now().After(order.PayBefore) && order.FulfillmentStatus == "" {
		return ErrPaymentIntentExpired
	}
	return nil
}

func (p *PaymentService) markPendingConfirm(orderID, tradeNo, proofHash string) error {
	p.service.mu.Lock()
	defer p.service.mu.Unlock()
	order := p.service.orders[orderID]
	if order == nil {
		return ErrOrderNotFound
	}
	if order.Status != "created" {
		return ErrPaymentIntentInvalid
	}
	for id, existing := range p.service.orders {
		if id != orderID && existing.ProviderTradeNo == tradeNo {
			return ErrPaymentTradeReused
		}
	}
	backup := p.service.cloneStateLocked()
	order.Status = "paid"
	order.ProviderTradeNo = tradeNo
	order.ProofHash = proofHash
	order.FulfillmentStatus = "pending_confirm"
	p.service.subscriptions[order.SubscriptionID].Status = "active"
	if err := p.service.persistLocked(); err != nil {
		p.service.restoreStateLocked(backup)
		return err
	}
	return nil
}

func (p *PaymentService) markFulfilled(orderID string) error {
	p.service.mu.Lock()
	defer p.service.mu.Unlock()
	order := p.service.orders[orderID]
	if order == nil {
		return ErrOrderNotFound
	}
	backup := p.service.cloneStateLocked()
	order.FulfillmentStatus = "fulfilled"
	if err := p.service.persistLocked(); err != nil {
		p.service.restoreStateLocked(backup)
		return err
	}
	return nil
}

func (p *PaymentService) intentByRequestID(requestID string) (*Order, error) {
	p.service.mu.Lock()
	defer p.service.mu.Unlock()
	for _, order := range p.service.orders {
		if order.RequestID == requestID {
			copied := *order
			return &copied, nil
		}
	}
	return nil, ErrPaymentIntentInvalid
}

func grantForOrder(order *Order, already bool) ActivationGrant {
	return ActivationGrant{
		RequestID: order.RequestID, OrderID: order.ID, SubscriptionID: order.SubscriptionID,
		Plan: order.Plan, ResourceID: order.ResourceID, Amount: order.Amount,
		Currency: order.Currency, TradeNo: order.ProviderTradeNo, AlreadyFulfilled: already,
	}
}

type paymentProof struct {
	PaymentProof  string
	TradeNo       string
	ClientSession string
}

func parsePaymentProof(value string) (paymentProof, error) {
	value = strings.TrimSpace(value)
	raw, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		raw, err = base64.URLEncoding.DecodeString(value)
	}
	if err != nil {
		raw, err = base64.StdEncoding.DecodeString(value)
	}
	if err != nil {
		return paymentProof{}, ErrPaymentProofInvalid
	}
	var envelope struct {
		Protocol struct {
			PaymentProof string `json:"payment_proof"`
			TradeNo      string `json:"trade_no"`
		} `json:"protocol"`
		Method struct {
			ClientSession string `json:"client_session"`
		} `json:"method"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil ||
		envelope.Protocol.PaymentProof == "" || envelope.Protocol.TradeNo == "" {
		return paymentProof{}, ErrPaymentProofInvalid
	}
	return paymentProof{
		PaymentProof:  envelope.Protocol.PaymentProof,
		TradeNo:       envelope.Protocol.TradeNo,
		ClientSession: envelope.Method.ClientSession,
	}, nil
}

func hashPaymentProof(proof paymentProof) string {
	digest := sha256.Sum256([]byte(proof.TradeNo + "\n" + proof.PaymentProof + "\n" + proof.ClientSession))
	return hex.EncodeToString(digest[:])
}

func refundRequestNo(orderID string) string {
	digest := sha256.Sum256([]byte("refund\n" + orderID))
	return "rf_" + hex.EncodeToString(digest[:16])
}

var requestIDPattern = regexp.MustCompile(`^[A-Za-z0-9._:-]{8,128}$`)

func validateIntentRequest(request ActivationIntentRequest) error {
	if !requestIDPattern.MatchString(request.RequestID) ||
		request.Plan != ActivationTaskPlan ||
		strings.TrimSpace(request.InstallID) == "" ||
		strings.TrimSpace(request.DevicePublicKey) == "" {
		return ErrPaymentIntentInvalid
	}
	if _, err := decodePublicKey(request.DevicePublicKey); err != nil {
		return ErrPaymentIntentInvalid
	}
	return nil
}

var amountPattern = regexp.MustCompile(`^([0-9]+)(?:\.([0-9]{1,2}))?$`)

// parseCNYCents parses a CNY amount into integer cents.
func parseCNYCents(value string) (int64, error) {
	match := amountPattern.FindStringSubmatch(strings.TrimSpace(value))
	if match == nil {
		return 0, errors.New("invalid CNY amount")
	}
	whole, err := strconv.ParseInt(match[1], 10, 64)
	if err != nil {
		return 0, errors.New("invalid CNY amount")
	}
	fraction := match[2] + strings.Repeat("0", 2-len(match[2]))
	fractionCents, err := strconv.ParseInt(fraction, 10, 64)
	if err != nil {
		return 0, errors.New("invalid CNY amount")
	}
	return whole*100 + fractionCents, nil
}

// normalizeAmount keeps the Machine Pay 402 path constrained to the 50 CNY
// service-registration price cap; it must stay untouched for compatibility.
func normalizeAmount(value string) (string, error) {
	cents, err := parseCNYCents(value)
	if err != nil {
		return "", err
	}
	if cents <= 0 {
		return "", errors.New("amount must be positive")
	}
	if cents > 5000 {
		return "", errors.New("amount exceeds Alipay 50 CNY limit")
	}
	return fmt.Sprintf("%d.%02d", cents/100, cents%100), nil
}

// normalizePlanAmount normalizes plan prices for the unified page-pay checkout
// (alipay.trade.page.pay accepts [0.01, 1亿]) without the Machine Pay cap.
func normalizePlanAmount(value string) (string, error) {
	cents, err := parseCNYCents(value)
	if err != nil {
		return "", err
	}
	if cents <= 0 {
		return "", errors.New("amount must be positive")
	}
	if cents > 10000000000 {
		return "", errors.New("amount exceeds Alipay page pay limit")
	}
	return fmt.Sprintf("%d.%02d", cents/100, cents%100), nil
}

func encodePaymentNeeded(value PaymentNeeded) (string, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func encodePaymentValidation(grant ActivationGrant) (string, error) {
	raw, err := json.Marshal(map[string]any{
		"trade_no": grant.TradeNo, "out_trade_no": grant.OrderID,
		"validated": true, "resource_id": grant.ResourceID,
	})
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}
