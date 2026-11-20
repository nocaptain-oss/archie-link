package controlplane

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"github.com/nocaptain-oss/archie-link/internal/entitlement"
)

type ed25519Private = ed25519.PrivateKey

// signLease 构造 lease v2 载荷并用服务端 Ed25519 key 签名，
// 输出可直接被客户端 LeaseFileProvider（编译期信任根）验证。
func (s *Service) signLease(sub *Subscription, plan Plan, installID, devicePublicKey string, now time.Time) ([]byte, error) {
	if len(s.signingKey) != ed25519.PrivateKeySize {
		return nil, fmt.Errorf("controlplane signing key is not configured")
	}
	usedThisYear := 0
	for _, at := range sub.RegistrationEvents {
		if !at.Before(sub.YearStart) {
			usedThisYear++
		}
	}
	nonce := make([]byte, 16)
	if err := fillRandom(nonce); err != nil {
		return nil, err
	}
	lease := entitlement.Lease{
		SchemaVersion:                    "2",
		SkillID:                          SkillID,
		LicenseID:                        sub.ID,
		InstallID:                        installID,
		DevicePublicKey:                  devicePublicKey,
		Plan:                             sub.Plan,
		Features:                         append([]string(nil), plan.Features...),
		IssuedAt:                         now,
		NotBefore:                        now,
		ExpiresAt:                        now.Add(LeaseTTL),
		OfflineGraceUntil:                now.Add(LeaseTTL + GracePeriod),
		LeaseID:                          newID("lease_"),
		Nonce:                            hex.EncodeToString(nonce),
		RevocationEpoch:                  sub.RevocationEpoch,
		MaxDevices:                       plan.MaxDevices,
		MaxNewDevicesPerYear:             plan.MaxNewPerYear,
		MaxNewDevicesPer7Days:            plan.MaxNewPer7Days,
		DeviceRegistrationQuotaRemaining: plan.MaxNewPerYear - usedThisYear,
	}
	payload, err := json.Marshal(lease)
	if err != nil {
		return nil, err
	}
	signature := ed25519.Sign(ed25519.PrivateKey(s.signingKey), payload)
	envelope, err := json.Marshal(entitlement.KeyedPayload{
		KeyID:     s.signingKeyID,
		Payload:   payload,
		Signature: base64.StdEncoding.EncodeToString(signature),
	})
	if err != nil {
		return nil, err
	}
	return envelope, nil
}

func newID(prefix string) string {
	raw := make([]byte, 8)
	if err := fillRandom(raw); err != nil {
		// 随机数失败属于致命环境问题；时间戳兜底保证 ID 唯一性。
		return fmt.Sprintf("%s%016x", prefix, time.Now().UnixNano())
	}
	return prefix + hex.EncodeToString(raw)
}

func fillRandom(target []byte) error {
	_, err := rand.Read(target)
	return err
}
