package leaseclient

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"testing"
	"time"

	"github.com/nocaptain-oss/archie-link/internal/entitlement"
)

func TestRenewerSavesLeaseAndEpoch(t *testing.T) {
	now := time.Now().UTC()
	current := entitlement.Lease{
		SchemaVersion: "2", LicenseID: "sub-1",
		ExpiresAt:       now.Add(7 * time.Hour),
		RevocationEpoch: 2,
	}
	renewed := current
	renewed.ExpiresAt = now.Add(7 * 24 * time.Hour)
	renewed.RevocationEpoch = 3
	client := &fakeRenewalClient{
		epoch: 3, renewed: leaseBytes(t, renewed),
	}
	var saved []byte
	epoch := 0
	renewer := Renewer{
		Client: client, OrderID: "order-1",
		LoadLease: func(context.Context) ([]byte, error) {
			return leaseBytes(t, current), nil
		},
		SaveLease: func(_ context.Context, data []byte) error {
			saved = append([]byte(nil), data...)
			return nil
		},
		SaveEpoch: func(_ context.Context, value int) error {
			if value > epoch {
				epoch = value
			}
			return nil
		},
		Now: func() time.Time { return now },
	}
	got, err := renewer.RenewOnce(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got.RevocationEpoch != 3 || len(saved) == 0 || epoch != 3 {
		t.Fatalf("lease=%#v saved=%d epoch=%d", got, len(saved), epoch)
	}
}

func TestRenewerNextDelayUses48HourWindow(t *testing.T) {
	now := time.Now().UTC()
	lease := entitlement.Lease{ExpiresAt: now.Add(72 * time.Hour)}
	renewer := Renewer{
		LoadLease: func(context.Context) ([]byte, error) {
			return leaseBytes(t, lease), nil
		},
		Now: func() time.Time { return now },
	}
	if got := renewer.NextDelay(context.Background()); got != 24*time.Hour {
		t.Fatalf("delay=%s", got)
	}
}

type fakeRenewalClient struct {
	epoch   int
	renewed []byte
	err     error
}

func (f *fakeRenewalClient) Renew(context.Context, string) ([]byte, error) {
	return f.renewed, f.err
}

func (f *fakeRenewalClient) Epoch(context.Context, string) (int, error) {
	return f.epoch, f.err
}

func leaseBytes(t *testing.T, lease entitlement.Lease) []byte {
	t.Helper()
	payload, err := json.Marshal(lease)
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(entitlement.KeyedPayload{
		KeyID: "test", Payload: payload,
		Signature: base64.StdEncoding.EncodeToString([]byte("not-used-by-renewer")),
	})
	if err != nil {
		t.Fatal(err)
	}
	return data
}
