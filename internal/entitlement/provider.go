package entitlement

import (
	"context"
	"errors"
	"time"
)

var (
	ErrInvalidSignature      = errors.New("entitlement signature is invalid")
	ErrExpired               = errors.New("entitlement has expired")
	ErrRevoked               = errors.New("entitlement has been revoked")
	ErrFeatureNotGranted     = errors.New("entitlement does not grant the requested feature")
	ErrInstallMismatch       = errors.New("entitlement is bound to another install")
	ErrSkillMismatch         = errors.New("entitlement is bound to another skill")
	ErrNotYetValid           = errors.New("entitlement is not valid yet")
	ErrMalformed             = errors.New("entitlement payload is malformed")
	ErrProviderNotConfigured = errors.New("entitlement provider is not configured")
	ErrClockRollback         = errors.New("system clock moved backwards beyond the 24-hour review window; online lease renewal is required")
)

type Request struct {
	SkillID   string `json:"skill_id"`
	InstallID string `json:"install_id"`
	Feature   string `json:"feature"`
}

type Entitlement struct {
	SkillID   string     `json:"skill_id"`
	LicenseID string     `json:"license_id"`
	InstallID string     `json:"install_id"`
	Features  []string   `json:"features"`
	IssuedAt  time.Time  `json:"issued_at"`
	ExpiresAt time.Time  `json:"expires_at"`
	RevokedAt *time.Time `json:"revoked_at,omitempty"`
	Signature string     `json:"signature,omitempty"`
}

type Provider interface {
	Verify(context.Context, Request) (Entitlement, error)
}

func Validate(entitlement Entitlement, request Request, now time.Time) error {
	if entitlement.RevokedAt != nil && !entitlement.RevokedAt.After(now) {
		return ErrRevoked
	}
	if !entitlement.ExpiresAt.After(now) {
		return ErrExpired
	}
	if entitlement.IssuedAt.After(now.Add(time.Minute)) {
		return ErrNotYetValid
	}
	if entitlement.SkillID != request.SkillID {
		return ErrSkillMismatch
	}
	if entitlement.InstallID != request.InstallID {
		return ErrInstallMismatch
	}
	for _, feature := range entitlement.Features {
		if feature == request.Feature {
			return nil
		}
	}
	return ErrFeatureNotGranted
}

func ClockRollback(lastSeenAt, now time.Time) bool {
	return !lastSeenAt.IsZero() && now.Add(24*time.Hour).Before(lastSeenAt)
}
