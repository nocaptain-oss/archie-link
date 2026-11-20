package leaseclient

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/nocaptain-oss/archie-link/internal/entitlement"
)

type RenewalClient interface {
	Renew(context.Context, string) ([]byte, error)
	Epoch(context.Context, string) (int, error)
}

type Renewer struct {
	Client     RenewalClient
	OrderID    string
	LoadLease  func(context.Context) ([]byte, error)
	SaveLease  func(context.Context, []byte) error
	SaveEpoch  func(context.Context, int) error
	Now        func() time.Time
	RetryDelay time.Duration
}

func (r Renewer) RenewOnce(ctx context.Context) (entitlement.Lease, error) {
	if r.Client == nil {
		return entitlement.Lease{}, errors.New("lease client is not configured")
	}
	if r.LoadLease == nil || r.SaveLease == nil {
		return entitlement.Lease{}, errors.New("lease persistence is not configured")
	}
	current, err := r.LoadLease(ctx)
	if err != nil {
		return entitlement.Lease{}, err
	}
	lease, err := decodeRenewLease(current)
	if err != nil {
		return entitlement.Lease{}, err
	}
	if r.SaveEpoch != nil {
		if epoch, epochErr := r.Client.Epoch(ctx, lease.LicenseID); epochErr == nil {
			if err := r.SaveEpoch(ctx, epoch); err != nil {
				return entitlement.Lease{}, err
			}
		}
	}
	renewed, err := r.Client.Renew(ctx, r.OrderID)
	if err != nil {
		return entitlement.Lease{}, err
	}
	if err := r.SaveLease(ctx, renewed); err != nil {
		return entitlement.Lease{}, err
	}
	lease, err = decodeRenewLease(renewed)
	if err != nil {
		return entitlement.Lease{}, err
	}
	if r.SaveEpoch != nil {
		if err := r.SaveEpoch(ctx, lease.RevocationEpoch); err != nil {
			return entitlement.Lease{}, err
		}
	}
	return lease, nil
}

func (r Renewer) NextDelay(ctx context.Context) time.Duration {
	if r.Now == nil {
		r.Now = time.Now
	}
	if r.LoadLease == nil {
		return time.Hour
	}
	data, err := r.LoadLease(ctx)
	if err != nil {
		return time.Hour
	}
	lease, err := decodeRenewLease(data)
	if err != nil {
		return time.Hour
	}
	renewAt := lease.ExpiresAt.Add(-48 * time.Hour)
	delay := renewAt.Sub(r.Now())
	if delay < 0 {
		return 0
	}
	return delay
}

func (r Renewer) Run(ctx context.Context) error {
	for {
		delay := r.NextDelay(ctx)
		if delay > 0 {
			timer := time.NewTimer(delay)
			select {
			case <-ctx.Done():
				timer.Stop()
				return ctx.Err()
			case <-timer.C:
			}
		}
		if _, err := r.RenewOnce(ctx); err == nil {
			continue
		}
		retry := r.RetryDelay
		if retry <= 0 {
			retry = 30 * time.Second
		}
		timer := time.NewTimer(retry)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}

func decodeRenewLease(data []byte) (entitlement.Lease, error) {
	var envelope struct {
		Payload json.RawMessage `json:"payload"`
	}
	if err := json.Unmarshal(data, &envelope); err != nil {
		return entitlement.Lease{}, entitlement.ErrMalformed
	}
	var lease entitlement.Lease
	if err := json.Unmarshal(envelope.Payload, &lease); err != nil {
		return entitlement.Lease{}, entitlement.ErrMalformed
	}
	return lease, nil
}
