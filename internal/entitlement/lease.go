package entitlement

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

const SkillID = "archie-link"

var (
	ErrDeviceMismatch   = errors.New("lease is bound to another device")
	ErrLeaseNotYetValid = errors.New("lease is not valid yet")
	ErrRevocationEpoch  = fmt.Errorf("%w: lease is older than the current revocation epoch", ErrRevoked)
)

type LeaseRequest struct {
	SkillID         string `json:"skill_id"`
	InstallID       string `json:"install_id"`
	DevicePublicKey string `json:"device_public_key"`
	Feature         string `json:"feature"`
	RevocationFloor int    `json:"revocation_floor"`
}

type Lease struct {
	SchemaVersion                    string    `json:"schema_version"`
	SkillID                          string    `json:"skill_id"`
	LicenseID                        string    `json:"license_id"`
	InstallID                        string    `json:"install_id"`
	DevicePublicKey                  string    `json:"device_public_key"`
	Plan                             string    `json:"plan"`
	Features                         []string  `json:"features"`
	IssuedAt                         time.Time `json:"issued_at"`
	NotBefore                        time.Time `json:"not_before"`
	ExpiresAt                        time.Time `json:"expires_at"`
	OfflineGraceUntil                time.Time `json:"offline_grace_until"`
	LeaseID                          string    `json:"lease_id"`
	Nonce                            string    `json:"nonce"`
	RevocationEpoch                  int       `json:"revocation_epoch"`
	MaxDevices                       int       `json:"max_devices"`
	MaxNewDevicesPerYear             int       `json:"max_new_devices_per_year"`
	MaxNewDevicesPer7Days            int       `json:"max_new_devices_per_7_days"`
	DeviceRegistrationQuotaRemaining int       `json:"device_registration_quota_remaining"`
}

type LeaseState string

const (
	LeaseActive       LeaseState = "active"
	LeaseNeedsRenewal LeaseState = "needs_renewal"
	LeaseOfflineGrace LeaseState = "offline_grace"
)

func VerifyLease(
	data []byte,
	root TrustRoot,
	request LeaseRequest,
	now time.Time,
) (Lease, LeaseState, error) {
	payload, _, err := root.VerifyKeyed(data, now)
	if err != nil {
		return Lease{}, "", err
	}
	var lease Lease
	if err := json.Unmarshal(payload, &lease); err != nil {
		return Lease{}, "", ErrMalformed
	}
	if lease.SchemaVersion != "2" || lease.LeaseID == "" || lease.Nonce == "" {
		return Lease{}, "", ErrMalformed
	}
	if lease.SkillID != request.SkillID {
		return Lease{}, "", ErrSkillMismatch
	}
	if lease.InstallID != request.InstallID {
		return Lease{}, "", ErrInstallMismatch
	}
	if lease.DevicePublicKey != request.DevicePublicKey {
		return Lease{}, "", ErrDeviceMismatch
	}
	if request.RevocationFloor > 0 && lease.RevocationEpoch < request.RevocationFloor {
		return Lease{}, "", ErrRevocationEpoch
	}
	if lease.NotBefore.After(now.Add(time.Minute)) {
		return Lease{}, "", ErrLeaseNotYetValid
	}
	if !containsFeature(lease.Features, request.Feature) {
		return Lease{}, "", ErrFeatureNotGranted
	}
	if !now.Before(lease.ExpiresAt) {
		if now.Before(lease.OfflineGraceUntil) {
			return lease, LeaseOfflineGrace, nil
		}
		return Lease{}, "", ErrExpired
	}
	if lease.ExpiresAt.Sub(now) <= 48*time.Hour {
		return lease, LeaseNeedsRenewal, nil
	}
	return lease, LeaseActive, nil
}

func containsFeature(features []string, want string) bool {
	for _, feature := range features {
		if feature == want {
			return true
		}
	}
	return false
}
