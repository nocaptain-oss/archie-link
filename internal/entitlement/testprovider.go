package entitlement

import (
	"context"
	"time"
)

type TestProvider struct {
	Entitlement Entitlement
	Now         func() time.Time
}

func NewTestProvider(mutate func(*Entitlement)) Provider {
	now := time.Now().UTC()
	entitlement := Entitlement{
		SkillID:   "archie-link",
		LicenseID: "test-license",
		InstallID: "test-install",
		Features:  []string{"diagnostics", "network_apply", "export_private"},
		IssuedAt:  now.Add(-time.Hour),
		ExpiresAt: now.Add(24 * time.Hour),
	}
	if mutate != nil {
		mutate(&entitlement)
	}
	return TestProvider{Entitlement: entitlement}
}

func (p TestProvider) Verify(ctx context.Context, request Request) (Entitlement, error) {
	if err := ctx.Err(); err != nil {
		return Entitlement{}, err
	}
	now := time.Now()
	if p.Now != nil {
		now = p.Now()
	}
	if err := Validate(p.Entitlement, request, now); err != nil {
		return Entitlement{}, err
	}
	return p.Entitlement, nil
}
