// Package controlplane 实现 ArchieLink 商业控制面的最小闭环：
// 订单、支付事件（HMAC 防伪/防重放）、设备身份挑战、设备绑定租约签发与续租、
// 撤销/退款、家庭年度 12 台设备池。
//
// 边界（HANDOFF.md）：
//   - 内部 HMAC 通知保留给受控订单运维；面向买家的 SkillHub Pay 使用
//     /v1/pay/activation-intent 的 402 + Payment-Proof 流程。
//   - 云端只做控制面：不下发路由命令、不承载流量、不存储 WireGuard 私钥。
//   - down / rollback 在客户端永远不依赖本服务。
package controlplane

import (
	"crypto/ecdsa"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/nocaptain-oss/archie-link/internal/entitlement"
)

const (
	SkillID              = entitlement.SkillID
	FeatureApply         = "network_apply"
	FeatureExportPrivate = "export_private"
	LeaseTTL             = 7 * 24 * time.Hour
	GracePeriod          = 30 * 24 * time.Hour
	ChallengeTTL         = 5 * time.Minute
	NotifyMaxSkew        = 5 * time.Minute
)

var (
	ErrOrderNotFound         = errors.New("order not found")
	ErrSubscriptionNotFound  = errors.New("subscription not found")
	ErrOrderNotPaid          = errors.New("order is not paid")
	ErrSubscriptionInactive  = errors.New("subscription is not active")
	ErrEventRejected         = errors.New("payment event rejected")
	ErrEventDuplicate        = errors.New("payment event already processed")
	ErrChallengeInvalid      = errors.New("challenge is invalid or expired")
	ErrSignatureInvalid      = errors.New("device signature is invalid")
	ErrDeviceUnknown         = errors.New("device is not registered for this subscription")
	ErrDeviceQuota           = errors.New("device registration quota exceeded")
	ErrPlanUnknown           = errors.New("plan is unknown")
	ErrPlanRenewalNotAllowed = errors.New("plan does not allow automatic renewal")
	ErrPaymentIntentMismatch = errors.New("order is bound to another device")
)

// Plan 定义签发参数与设备池限制。
type Plan struct {
	ID             string
	Features       []string
	MaxDevices     int
	MaxNewPerYear  int
	MaxNewPer7Days int
	Amount         string
	GoodsName      string
	// GracePeriod 是该计划的离线宽限期；零值表示使用默认 30 天。
	// activation-task 为 7 天：7 天租约 + 7 天宽限 = 14 天执行窗口。
	GracePeriod time.Duration
	Renewable   bool
}

// GraceWindow 返回计划的离线宽限期（默认 30 天）。
func (p Plan) GraceWindow() time.Duration {
	if p.GracePeriod > 0 {
		return p.GracePeriod
	}
	return GracePeriod
}

var plans = map[string]Plan{
	"family-annual-12": {
		ID:             "family-annual-12",
		Features:       []string{FeatureApply, FeatureExportPrivate},
		MaxDevices:     12,
		MaxNewPerYear:  12,
		MaxNewPer7Days: 3,
		Amount:         "69.00",
		GoodsName:      "ArchieLink 家庭年度",
		Renewable:      true,
	},
	"perpetual": {
		ID:             "perpetual",
		Features:       []string{FeatureApply, FeatureExportPrivate},
		MaxDevices:     2,
		MaxNewPerYear:  2,
		MaxNewPer7Days: 2,
		Amount:         "199.00",
		GoodsName:      "ArchieLink 个人永久版",
		Renewable:      true,
	},
	"activation-task": {
		ID:             "activation-task",
		Features:       []string{FeatureApply},
		MaxDevices:     1,
		MaxNewPerYear:  1,
		MaxNewPer7Days: 1,
		Amount:         "19.90",
		GoodsName:      "ArchieLink 安全激活",
		GracePeriod:    7 * 24 * time.Hour,
		Renewable:      false,
	},
}

// PlanByName 返回计划定义。
func PlanByName(id string) (Plan, bool) {
	plan, ok := plans[id]
	return plan, ok
}

type Device struct {
	InstallID    string    `json:"install_id"`
	PublicKey    string    `json:"device_public_key"`
	KeyID        string    `json:"key_id"`
	RegisteredAt time.Time `json:"registered_at"`
	// NewThisYear 标记本订阅年度内是否已消耗过设备注册名额。
	NewThisYear bool `json:"new_this_year"`
}

type Subscription struct {
	ID              string            `json:"id"`
	Plan            string            `json:"plan"`
	Status          string            `json:"status"` // pending_payment / active / refunded / revoked
	YearStart       time.Time         `json:"year_start"`
	RevocationEpoch int               `json:"revocation_epoch"`
	Devices         map[string]Device `json:"devices"`
	// RegistrationEvents 记录消耗注册名额的时刻，用于滚动 7 天限速。
	RegistrationEvents []time.Time     `json:"registration_events"`
	ProcessedEvents    map[string]bool `json:"processed_events"`
}

type Order struct {
	ID               string    `json:"id"`
	SubscriptionID   string    `json:"subscription_id"`
	Plan             string    `json:"plan"`
	Status           string    `json:"status"` // created / paid / refunded / revoked
	CreatedAt        time.Time `json:"created_at"`
	RequestID        string    `json:"request_id,omitempty"`
	InstallID        string    `json:"install_id,omitempty"`
	DevicePublicKey  string    `json:"device_public_key,omitempty"`
	ResourceID       string    `json:"resource_id,omitempty"`
	Amount           string    `json:"amount,omitempty"`
	Currency         string    `json:"currency,omitempty"`
	PayBefore        time.Time `json:"pay_before,omitempty"`
	ProviderPrePayID string    `json:"provider_prepay_id,omitempty"`
	ProviderTradeNo  string    `json:"provider_trade_no,omitempty"`
	// QuotaBaseline 记录即刻收次数包下单时的余额基线；余额超过基线视为新购到账。
	QuotaBaseline     int64  `json:"quota_baseline,omitempty"`
	ProofHash         string `json:"proof_hash,omitempty"`
	FulfillmentStatus string `json:"fulfillment_status,omitempty"`
}

// Challenge 是一次性激活/续租挑战；nonce 一次有效，绑定设备公钥。
type Challenge struct {
	ID        string
	InstallID string
	PublicKey string
	Value     []byte
	ExpiresAt time.Time
	used      bool
}

// Service 是控制面核心。持久化由注入的 Store 负责；nil 时为纯内存。
type Service struct {
	mu            sync.Mutex
	signingKey    ed25519Private
	signingKeyID  string
	notifySecret  []byte
	now           func() time.Time
	store         Store
	orders        map[string]*Order
	subscriptions map[string]*Subscription
	challenges    map[string]*Challenge
}

// NewService 创建纯内存控制面，供测试与本地开发使用。
func NewService(keyID string, signingKey ed25519Private, notifySecret []byte, now func() time.Time) *Service {
	service, _ := NewServiceWithStore(keyID, signingKey, notifySecret, now, nil)
	return service
}

// NewServiceWithStore 创建控制面并从 store 恢复状态。
// 持久化文件损坏时 fail closed，返回错误而不是静默清空。
func NewServiceWithStore(keyID string, signingKey ed25519Private, notifySecret []byte, now func() time.Time, store Store) (*Service, error) {
	if now == nil {
		now = time.Now
	}
	service := &Service{
		signingKey:    signingKey,
		signingKeyID:  keyID,
		notifySecret:  notifySecret,
		now:           now,
		store:         store,
		orders:        map[string]*Order{},
		subscriptions: map[string]*Subscription{},
		challenges:    map[string]*Challenge{},
	}
	if store == nil {
		return service, nil
	}
	state, err := store.Load()
	if err != nil {
		return nil, err
	}
	if state.Orders != nil {
		service.orders = state.Orders
	}
	if state.Subscriptions != nil {
		service.subscriptions = state.Subscriptions
	}
	return service, nil
}

// CreateOrder 建立订单与对应订阅（pending_payment）。
func (s *Service) CreateOrder(plan string) (Order, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := plans[plan]; !ok {
		return Order{}, ErrPlanUnknown
	}
	backup := s.cloneStateLocked()
	order := Order{
		ID:             newID("ord_"),
		SubscriptionID: newID("sub_"),
		Plan:           plan,
		Status:         "created",
		CreatedAt:      s.now(),
	}
	s.orders[order.ID] = &order
	s.subscriptions[order.SubscriptionID] = &Subscription{
		ID:              order.SubscriptionID,
		Plan:            plan,
		Status:          "pending_payment",
		YearStart:       order.CreatedAt,
		RevocationEpoch: 0,
		Devices:         map[string]Device{},
		ProcessedEvents: map[string]bool{},
	}
	if err := s.persistLocked(); err != nil {
		s.restoreStateLocked(backup)
		return Order{}, err
	}
	return order, nil
}

// NotifyPayment 处理内部支付通知：HMAC 防伪造、时间窗防过期、eventID 防重放。
// status 只接受 "paid"；其余状态走 Refund / Revoke 专用入口。
func (s *Service) NotifyPayment(orderID, eventID, status string, timestamp time.Time, signature string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	order, ok := s.orders[orderID]
	if !ok {
		return ErrOrderNotFound
	}
	sub := s.subscriptions[order.SubscriptionID]
	now := s.now()
	if timestamp.Before(now.Add(-NotifyMaxSkew)) || timestamp.After(now.Add(NotifyMaxSkew)) {
		return fmt.Errorf("%w: timestamp outside accept window", ErrEventRejected)
	}
	if status != "paid" {
		return fmt.Errorf("%w: unsupported status", ErrEventRejected)
	}
	message := fmt.Sprintf("%s|%s|%s|%d", eventID, orderID, status, timestamp.Unix())
	if !verifyHMAC(s.notifySecret, message, signature) {
		return fmt.Errorf("%w: bad signature", ErrEventRejected)
	}
	if sub.ProcessedEvents[eventID] {
		return ErrEventDuplicate
	}

	backup := s.cloneStateLocked()
	sub.ProcessedEvents[eventID] = true
	if order.Status != "paid" {
		order.Status = "paid"
		sub.Status = "active"
	}
	if err := s.persistLocked(); err != nil {
		s.restoreStateLocked(backup)
		return err
	}
	return nil
}

// IssueChallenge 生成一次性挑战。
func (s *Service) IssueChallenge(installID, devicePublicKey string) (Challenge, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if installID == "" || devicePublicKey == "" {
		return Challenge{}, ErrChallengeInvalid
	}
	if _, err := decodePublicKey(devicePublicKey); err != nil {
		return Challenge{}, ErrChallengeInvalid
	}
	nonce := make([]byte, 32)
	if err := fillRandom(nonce); err != nil {
		return Challenge{}, err
	}
	challenge := Challenge{
		ID:        newID("ch_"),
		InstallID: installID,
		PublicKey: devicePublicKey,
		Value:     nonce,
		ExpiresAt: s.now().Add(ChallengeTTL),
	}
	s.challenges[challenge.ID] = &challenge
	return challenge, nil
}

// Activate 在订单已支付且设备挑战验证通过后注册设备并签发租约。
// 同一 installID + 同一公钥（重装/升级）不消耗年度名额；
// 公钥变化视为设备密钥重置，消耗新名额并受滚动 7 天限速约束。
func (s *Service) Activate(orderID, installID, devicePublicKey, nonceID, signature string) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	backup := s.cloneStateLocked()
	lease, err := s.activateLocked(orderID, installID, devicePublicKey, nonceID, signature)
	if err != nil {
		s.restoreStateLocked(backup)
		return nil, err
	}
	if err := s.persistLocked(); err != nil {
		s.restoreStateLocked(backup)
		return nil, err
	}
	return lease, nil
}

func (s *Service) activateLocked(orderID, installID, devicePublicKey, nonceID, signature string) ([]byte, error) {
	order, ok := s.orders[orderID]
	if !ok || order.Status != "paid" {
		return nil, ErrOrderNotPaid
	}
	if order.InstallID != "" &&
		(order.InstallID != installID || order.DevicePublicKey != devicePublicKey) {
		return nil, ErrPaymentIntentMismatch
	}
	sub := s.subscriptions[order.SubscriptionID]
	if sub.Status != "active" {
		return nil, ErrSubscriptionInactive
	}
	if err := s.verifyChallenge(sub, installID, devicePublicKey, nonceID, signature); err != nil {
		return nil, err
	}
	plan := plans[sub.Plan]
	now := s.now()

	device, known := sub.Devices[installID]
	reRegistration := known && device.PublicKey == devicePublicKey
	if !reRegistration {
		if err := s.checkQuota(sub, plan, now); err != nil {
			return nil, err
		}
	}

	if !known {
		device = Device{
			InstallID:    installID,
			PublicKey:    devicePublicKey,
			RegisteredAt: now,
		}
	}
	if !reRegistration {
		device.KeyID = keyFingerprint(devicePublicKey)
		device.RegisteredAt = now
		device.NewThisYear = true
		sub.RegistrationEvents = append(sub.RegistrationEvents, now)
	}
	device.PublicKey = devicePublicKey
	sub.Devices[installID] = device

	lease, err := s.signLease(sub, plan, installID, devicePublicKey, now)
	if err != nil {
		return nil, err
	}
	return lease, nil
}

// Renew 为已注册设备续租；订阅退款/撤销后停止续租。
func (s *Service) Renew(orderID, installID, devicePublicKey, nonceID, signature string) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	order, ok := s.orders[orderID]
	if !ok {
		return nil, ErrOrderNotFound
	}
	sub := s.subscriptions[order.SubscriptionID]
	if sub.Status != "active" {
		return nil, ErrSubscriptionInactive
	}
	plan, ok := plans[sub.Plan]
	if !ok || !plan.Renewable {
		return nil, ErrPlanRenewalNotAllowed
	}
	device, known := sub.Devices[installID]
	if !known || device.PublicKey != devicePublicKey {
		return nil, ErrDeviceUnknown
	}
	if err := s.verifyChallenge(sub, installID, devicePublicKey, nonceID, signature); err != nil {
		return nil, err
	}
	return s.signLease(sub, plan, installID, devicePublicKey, s.now())
}

// Refund 将订阅标记为退款并推进撤销 epoch：已发租约在到期后失效，续租立即停止。
func (s *Service) Refund(orderID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	order, ok := s.orders[orderID]
	if !ok {
		return ErrOrderNotFound
	}
	if order.Status == "refunded" || order.Status == "revoked" {
		return nil
	}
	if order.Status != "paid" && order.Status != "created" {
		return ErrOrderNotPaid
	}
	backup := s.cloneStateLocked()
	sub := s.subscriptions[order.SubscriptionID]
	order.Status = "refunded"
	sub.Status = "refunded"
	sub.RevocationEpoch++
	if err := s.persistLocked(); err != nil {
		s.restoreStateLocked(backup)
		return err
	}
	return nil
}

// Chargeback 语义同退款，状态记录区分来源。
func (s *Service) Chargeback(orderID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	order, ok := s.orders[orderID]
	if !ok {
		return ErrOrderNotFound
	}
	if order.Status == "revoked" {
		return nil
	}
	if order.Status != "paid" && order.Status != "refunded" && order.Status != "created" {
		return ErrOrderNotPaid
	}
	backup := s.cloneStateLocked()
	sub := s.subscriptions[order.SubscriptionID]
	order.Status = "revoked"
	sub.Status = "revoked"
	sub.RevocationEpoch++
	if err := s.persistLocked(); err != nil {
		s.restoreStateLocked(backup)
		return err
	}
	return nil
}

// ListDevices 返回注册设备（按注册时间排序）。
func (s *Service) ListDevices(subscriptionID string) ([]Device, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sub, ok := s.subscriptions[subscriptionID]
	if !ok {
		return nil, ErrSubscriptionNotFound
	}
	devices := make([]Device, 0, len(sub.Devices))
	for _, device := range sub.Devices {
		devices = append(devices, device)
	}
	sort.Slice(devices, func(i, j int) bool { return devices[i].RegisteredAt.Before(devices[j].RegisteredAt) })
	return devices, nil
}

// RemoveDevice 移除设备的"同时激活"占用；年度注册名额不恢复。
func (s *Service) RemoveDevice(subscriptionID, installID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	sub, ok := s.subscriptions[subscriptionID]
	if !ok {
		return ErrSubscriptionNotFound
	}
	if _, known := sub.Devices[installID]; !known {
		return ErrDeviceUnknown
	}
	backup := s.cloneStateLocked()
	delete(sub.Devices, installID)
	if err := s.persistLocked(); err != nil {
		s.restoreStateLocked(backup)
		return err
	}
	return nil
}

// CurrentEpoch 返回订阅当前撤销 epoch，供客户端设置 revocation floor。
func (s *Service) CurrentEpoch(subscriptionID string) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sub, ok := s.subscriptions[subscriptionID]
	if !ok {
		return 0, ErrSubscriptionNotFound
	}
	return sub.RevocationEpoch, nil
}

func (s *Service) verifyChallenge(sub *Subscription, installID, devicePublicKey, nonceID, signature string) error {
	challenge, ok := s.challenges[nonceID]
	if !ok || challenge.used {
		return ErrChallengeInvalid
	}
	now := s.now()
	if now.After(challenge.ExpiresAt) {
		return ErrChallengeInvalid
	}
	if challenge.InstallID != installID || challenge.PublicKey != devicePublicKey {
		return ErrChallengeInvalid
	}
	publicKey, err := decodePublicKey(devicePublicKey)
	if err != nil {
		return ErrSignatureInvalid
	}
	digest := sha256.Sum256(challenge.Value)
	signatureBytes, err := base64.StdEncoding.DecodeString(signature)
	if err != nil || !ecdsa.VerifyASN1(publicKey, digest[:], signatureBytes) {
		return ErrSignatureInvalid
	}
	challenge.used = true
	delete(s.challenges, nonceID)
	return nil
}

func (s *Service) checkQuota(sub *Subscription, plan Plan, now time.Time) error {
	if len(sub.Devices) >= plan.MaxDevices {
		return ErrDeviceQuota
	}
	usedThisYear := 0
	for _, at := range sub.RegistrationEvents {
		if !at.Before(sub.YearStart) {
			usedThisYear++
		}
	}
	if usedThisYear >= plan.MaxNewPerYear {
		return fmt.Errorf("%w: annual registration quota exhausted", ErrDeviceQuota)
	}
	windowStart := now.Add(-7 * 24 * time.Hour)
	recent := 0
	for _, at := range sub.RegistrationEvents {
		if at.After(windowStart) {
			recent++
		}
	}
	if recent >= plan.MaxNewPer7Days {
		return fmt.Errorf("%w: too many new devices within 7 days", ErrDeviceQuota)
	}
	return nil
}

func verifyHMAC(secret []byte, message, signature string) bool {
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(message))
	expected := hex.EncodeToString(mac.Sum(nil))
	return subtle.ConstantTimeCompare([]byte(expected), []byte(strings.ToLower(signature))) == 1
}

func decodePublicKey(value string) (*ecdsa.PublicKey, error) {
	raw, err := base64.StdEncoding.DecodeString(value)
	if err != nil {
		return nil, err
	}
	parsed, err := x509.ParsePKIXPublicKey(raw)
	if err != nil {
		return nil, err
	}
	key, ok := parsed.(*ecdsa.PublicKey)
	if !ok {
		return nil, errors.New("not an ECDSA public key")
	}
	return key, nil
}

func keyFingerprint(publicKeyBase64 string) string {
	digest := sha256.Sum256([]byte(publicKeyBase64))
	return hex.EncodeToString(digest[:8])
}
