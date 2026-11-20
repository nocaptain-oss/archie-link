package entitlement

import (
	"context"
	"errors"
	"time"

	"github.com/nocaptain-oss/archie-link/internal/deviceid"
)

var ErrDeviceIdentityNotConfigured = errors.New("device identity is not configured")

type LeaseFileProvider struct {
	Data                []byte
	TrustRoot           TrustRoot
	Device              *deviceid.Manager
	RevocationFloor     int
	LastSeenAt          time.Time
	ClockReviewRequired bool
	RecordSeen          func(time.Time) error
	Now                 func() time.Time
}

func (p LeaseFileProvider) Verify(ctx context.Context, request Request) (Entitlement, error) {
	if p.Device == nil {
		return Entitlement{}, ErrDeviceIdentityNotConfigured
	}
	if err := ctx.Err(); err != nil {
		return Entitlement{}, err
	}
	identity, err := p.Device.LoadOrCreate(ctx)
	if err != nil {
		return Entitlement{}, err
	}
	now := time.Now()
	if p.Now != nil {
		now = p.Now()
	}
	if p.RecordSeen != nil {
		if err := p.RecordSeen(now); err != nil {
			return Entitlement{}, err
		}
	}
	if p.ClockReviewRequired || ClockRollback(p.LastSeenAt, now) {
		return Entitlement{}, ErrClockRollback
	}
	lease, _, err := VerifyLease(p.Data, p.TrustRoot, LeaseRequest{
		SkillID: request.SkillID, InstallID: request.InstallID,
		DevicePublicKey: identity.PublicKey, Feature: request.Feature,
		RevocationFloor: p.RevocationFloor,
	}, now)
	if err != nil {
		return Entitlement{}, err
	}
	return Entitlement{
		SkillID: lease.SkillID, LicenseID: lease.LicenseID,
		InstallID: lease.InstallID, Features: append([]string(nil), lease.Features...),
		IssuedAt: lease.IssuedAt, ExpiresAt: lease.ExpiresAt,
	}, nil
}
