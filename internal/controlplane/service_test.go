package controlplane

import (
	"context"
	"crypto/ed25519"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/nocaptain-oss/archie-link/internal/deviceid"
	"github.com/nocaptain-oss/archie-link/internal/entitlement"
)

type fixture struct {
	service      *Service
	order        Order
	subscription string
	notifySecret []byte
	now          time.Time
}

func newFixture(t *testing.T, plan string) fixture {
	t.Helper()
	now := time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC)
	_, signing, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	secret := []byte("internal-notify-secret")
	service := NewService("test-key", signing, secret, func() time.Time { return now })
	order, err := service.CreateOrder(plan)
	if err != nil {
		t.Fatal(err)
	}
	return fixture{
		service:      service,
		order:        order,
		subscription: order.SubscriptionID,
		notifySecret: secret,
		now:          now,
	}
}

func (f *fixture) pay(t *testing.T, orderID, eventID string, at time.Time) error {
	t.Helper()
	message := fmt.Sprintf("%s|%s|%s|%d", eventID, orderID, "paid", at.Unix())
	mac := hmacSHA256(f.notifySecret, message)
	return f.service.NotifyPayment(orderID, eventID, "paid", at, mac)
}

func (f *fixture) activate(t *testing.T, installID string, device *deviceid.Manager) ([]byte, error) {
	t.Helper()
	identity, err := device.LoadOrCreate(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	challenge, err := f.service.IssueChallenge(installID, identity.PublicKey)
	if err != nil {
		return nil, err
	}
	signature, err := device.Sign(context.Background(), challenge.Value)
	if err != nil {
		t.Fatal(err)
	}
	return f.service.Activate(f.order.ID, installID, identity.PublicKey, challenge.ID, base64.StdEncoding.EncodeToString(signature))
}

func newDevice(t *testing.T, dir string, installID string) *deviceid.Manager {
	t.Helper()
	return deviceid.New(deviceid.FileStore{Path: dir + "/" + installID + ".key"}, installID)
}

func TestPaymentEventIsIdempotent(t *testing.T) {
	f := newFixture(t, "family-annual-12")
	if err := f.pay(t, f.order.ID, "evt_1", f.now); err != nil {
		t.Fatal(err)
	}
	if err := f.pay(t, f.order.ID, "evt_1", f.now); err == nil {
		t.Fatal("duplicate event must be reported")
	}
	if f.service.orders[f.order.ID].Status != "paid" {
		t.Fatal("order must be paid exactly once")
	}
}

func TestForgedStaleReplayedPaymentRejected(t *testing.T) {
	f := newFixture(t, "family-annual-12")
	device := newDevice(t, t.TempDir(), "install-1")

	if err := f.service.NotifyPayment(f.order.ID, "evt_forged", "paid", f.now, "deadbeef"); err == nil {
		t.Fatal("forged signature must be rejected")
	}
	if err := f.pay(t, f.order.ID, "evt_stale", f.now.Add(-10*time.Minute)); err == nil {
		t.Fatal("stale notification must be rejected")
	}
	if err := f.pay(t, f.order.ID, "evt_ok", f.now); err != nil {
		t.Fatal(err)
	}
	if err := f.pay(t, f.order.ID, "evt_ok", f.now); err == nil {
		t.Fatal("replayed event must be rejected")
	}

	// 合法通知到达前，订单必须一直保持未支付：伪造/过期/重放都不能签发租约。
	unpaid := newFixture(t, "family-annual-12")
	if err := unpaid.service.NotifyPayment(unpaid.order.ID, "evt_forged", "paid", unpaid.now, "deadbeef"); err == nil {
		t.Fatal("forged signature must be rejected")
	}
	if err := unpaid.pay(t, unpaid.order.ID, "evt_stale", unpaid.now.Add(-10*time.Minute)); err == nil {
		t.Fatal("stale notification must be rejected")
	}
	if _, err := unpaid.activate(t, "install-1", device); err == nil {
		t.Fatal("order must stay unpaid after rejected notifications")
	}
}

func TestActivateIssuesVerifiableDeviceBoundLease(t *testing.T) {
	f := newFixture(t, "family-annual-12")
	if err := f.pay(t, f.order.ID, "evt_1", f.now); err != nil {
		t.Fatal(err)
	}
	device := newDevice(t, t.TempDir(), "install-1")
	identity, err := device.LoadOrCreate(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	// 未支付订单不能签发。
	unpaid := newFixture(t, "family-annual-12")
	if _, err := unpaid.service.Activate(unpaid.order.ID, "install-1", identity.PublicKey, "", ""); err == nil {
		t.Fatal("unpaid order must not issue lease")
	}

	license, err := f.activate(t, "install-1", device)
	if err != nil {
		t.Fatal(err)
	}

	// 客户端用同一信任根 + 同一设备身份验证：必须通过。
	root := entitlement.TrustRoot{Keys: []entitlement.TrustKey{{
		ID: "test-key", PublicKey: f.service.signingKey.Public().(ed25519.PublicKey),
	}}}
	provider := entitlement.LeaseFileProvider{
		Data: license, TrustRoot: root, Device: device,
		Now: func() time.Time { return f.now },
	}
	ent, err := provider.Verify(context.Background(), entitlement.Request{
		SkillID: SkillID, InstallID: "install-1", Feature: FeatureApply,
	})
	if err != nil {
		t.Fatalf("client-side lease verification failed: %v", err)
	}
	if ent.LicenseID != f.subscription || ent.ExpiresAt.Sub(f.now) != LeaseTTL {
		t.Fatalf("unexpected entitlement %#v", ent)
	}

	// 挑战 nonce 一次性：复用必须失败。
	challenge, err := f.service.IssueChallenge("install-1", identity.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	signature, err := device.Sign(context.Background(), challenge.Value)
	if err != nil {
		t.Fatal(err)
	}
	sig := base64.StdEncoding.EncodeToString(signature)
	if _, err := f.service.Activate(f.order.ID, "install-1", identity.PublicKey, challenge.ID, sig); err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.Activate(f.order.ID, "install-1", identity.PublicKey, challenge.ID, sig); err == nil {
		t.Fatal("nonce reuse must fail")
	}

	// 篡改签名必须失败。
	challenge2, err := f.service.IssueChallenge("install-1", identity.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.Activate(f.order.ID, "install-1", identity.PublicKey, challenge2.ID, "bogus"); err == nil {
		t.Fatal("bad signature must fail")
	}
}

func TestRefundedOrderCannotActivateOrRenew(t *testing.T) {
	f := newFixture(t, "family-annual-12")
	if err := f.pay(t, f.order.ID, "evt_1", f.now); err != nil {
		t.Fatal(err)
	}
	if err := f.service.Refund(f.order.ID); err != nil {
		t.Fatal(err)
	}
	device := newDevice(t, t.TempDir(), "install-1")
	if _, err := f.activate(t, "install-1", device); !errors.Is(err, ErrOrderNotPaid) {
		t.Fatalf("refunded activation=%v", err)
	}
	if f.service.orders[f.order.ID].Status != "refunded" {
		t.Fatalf("order status=%s", f.service.orders[f.order.ID].Status)
	}
}

func TestLeaseCopyToAnotherDeviceFails(t *testing.T) {
	f := newFixture(t, "family-annual-12")
	if err := f.pay(t, f.order.ID, "evt_1", f.now); err != nil {
		t.Fatal(err)
	}
	deviceA := newDevice(t, t.TempDir(), "install-a")
	deviceB := newDevice(t, t.TempDir(), "install-b")
	license, err := f.activate(t, "install-a", deviceA)
	if err != nil {
		t.Fatal(err)
	}

	root := entitlement.TrustRoot{Keys: []entitlement.TrustKey{{
		ID: "test-key", PublicKey: f.service.signingKey.Public().(ed25519.PublicKey),
	}}}
	provider := entitlement.LeaseFileProvider{
		Data: license, TrustRoot: root, Device: deviceB,
		Now: func() time.Time { return f.now },
	}
	if _, err := provider.Verify(context.Background(), entitlement.Request{
		SkillID: SkillID, InstallID: "install-b", Feature: FeatureApply,
	}); err == nil {
		t.Fatal("copied lease must fail on another device")
	}
}

func TestDeviceMigrationAfterRemovalRequiresNewActivation(t *testing.T) {
	f := newFixture(t, "family-annual-12")
	if err := f.pay(t, f.order.ID, "evt_1", f.now); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	oldDevice := newDevice(t, dir, "install-old")
	oldIdentity, err := oldDevice.LoadOrCreate(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.activate(t, "install-old", oldDevice); err != nil {
		t.Fatal(err)
	}
	if err := f.service.RemoveDevice(f.subscription, "install-old"); err != nil {
		t.Fatal(err)
	}

	challenge, err := f.service.IssueChallenge("install-old", oldIdentity.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	signature, err := oldDevice.Sign(context.Background(), challenge.Value)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.Renew(f.order.ID, "install-old", oldIdentity.PublicKey,
		challenge.ID, base64.StdEncoding.EncodeToString(signature)); !errors.Is(err, ErrDeviceUnknown) {
		t.Fatalf("removed device renewal=%v", err)
	}

	newDevice := newDevice(t, dir, "install-new")
	if _, err := f.activate(t, "install-new", newDevice); err != nil {
		t.Fatalf("migrated device activation failed: %v", err)
	}
}

func TestRenewStopsAfterRefundAndAdvancesEpoch(t *testing.T) {
	f := newFixture(t, "family-annual-12")
	if err := f.pay(t, f.order.ID, "evt_1", f.now); err != nil {
		t.Fatal(err)
	}
	device := newDevice(t, t.TempDir(), "install-1")
	identity, err := device.LoadOrCreate(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	renew := func() ([]byte, error) {
		challenge, err := f.service.IssueChallenge("install-1", identity.PublicKey)
		if err != nil {
			return nil, err
		}
		signature, err := device.Sign(context.Background(), challenge.Value)
		if err != nil {
			return nil, err
		}
		return f.service.Renew(f.order.ID, "install-1", identity.PublicKey, challenge.ID, base64.StdEncoding.EncodeToString(signature))
	}
	if _, err := renew(); err == nil {
		t.Fatal("renew before activation must fail (device unknown)")
	}
	if _, err := f.activate(t, "install-1", device); err != nil {
		t.Fatal(err)
	}
	if _, err := renew(); err != nil {
		t.Fatalf("renew for active subscription must succeed: %v", err)
	}

	if err := f.service.Refund(f.order.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := renew(); err == nil {
		t.Fatal("renew after refund must be rejected")
	}
	epoch, err := f.service.CurrentEpoch(f.subscription)
	if err != nil || epoch != 1 {
		t.Fatalf("epoch=%d err=%v, want 1", epoch, err)
	}
	if err := f.service.Chargeback(f.order.ID); err != nil {
		t.Fatal(err)
	}
	if epoch, _ := f.service.CurrentEpoch(f.subscription); epoch != 2 {
		t.Fatalf("chargeback must advance epoch, got %d", epoch)
	}
}

func TestFamilyAnnualDevicePool(t *testing.T) {
	f := newFixture(t, "family-annual-12")
	if err := f.pay(t, f.order.ID, "evt_1", f.now); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	activate := func(installID string) ([]byte, error) {
		device := newDevice(t, dir, installID)
		return f.activate(t, installID, device)
	}

	// 同一设备同密钥重装：不消耗名额，也不触发 7 天限速。
	if _, err := activate("install-1"); err != nil {
		t.Fatal(err)
	}
	if _, err := activate("install-1"); err != nil {
		t.Fatalf("re-registration must not consume quota: %v", err)
	}

	// 滚动 7 天限速：窗口内最多 3 台新设备（install-1 已消耗 1 个）。
	for _, id := range []string{"install-2", "install-3"} {
		if _, err := activate(id); err != nil {
			t.Fatalf("new device %s rejected: %v", id, err)
		}
	}
	if _, err := activate("install-4"); err == nil {
		t.Fatal("4th new device within 7 days must be rejected")
	}

	// 每推进一个窗口可再注册 3 台，直到年度 12 台名额耗尽。
	f.service.now = func() time.Time { return f.now }
	batches := [][]string{
		{"install-4", "install-5", "install-6"},
		{"install-7", "install-8", "install-9"},
		{"install-10", "install-11", "install-12"},
	}
	for _, batch := range batches {
		f.now = f.now.Add(8 * 24 * time.Hour)
		for _, id := range batch {
			if _, err := activate(id); err != nil {
				t.Fatalf("device %s rejected: %v", id, err)
			}
		}
	}

	// 年度 12 台 distinct 已满：新设备即使出了限速窗口也必须拒绝。
	f.now = f.now.Add(8 * 24 * time.Hour)
	if _, err := activate("install-13"); err == nil {
		t.Fatal("13th distinct device must be rejected")
	}

	// 移除设备腾出同时激活位，但年度名额不恢复。
	if err := f.service.RemoveDevice(f.subscription, "install-1"); err != nil {
		t.Fatal(err)
	}
	if _, err := activate("install-13"); err == nil {
		t.Fatal("annual quota must not be restored by device removal")
	}

	devices, err := f.service.ListDevices(f.subscription)
	if err != nil {
		t.Fatal(err)
	}
	if len(devices) != 11 {
		t.Fatalf("devices=%d want 11", len(devices))
	}
}

func TestChallengeBindsDeviceIdentity(t *testing.T) {
	f := newFixture(t, "family-annual-12")
	if err := f.pay(t, f.order.ID, "evt_1", f.now); err != nil {
		t.Fatal(err)
	}
	deviceA := newDevice(t, t.TempDir(), "install-a")
	deviceB := newDevice(t, t.TempDir(), "install-b")
	identityA, err := deviceA.LoadOrCreate(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := deviceB.LoadOrCreate(context.Background()); err != nil {
		t.Fatal(err)
	}

	// 设备 A 的挑战由设备 B 的私钥签名：必须拒绝。
	challenge, err := f.service.IssueChallenge("install-a", identityA.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	signature, err := deviceB.Sign(context.Background(), challenge.Value)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.Activate(f.order.ID, "install-a", identityA.PublicKey, challenge.ID, base64.StdEncoding.EncodeToString(signature)); err == nil {
		t.Fatal("challenge signed by another device must be rejected")
	}
}

// hmacSHA256 与 NotifyPayment 的验签格式保持一致（测试辅助）。
func hmacSHA256(secret []byte, message string) string {
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(message))
	return hex.EncodeToString(mac.Sum(nil))
}
