package entitlement

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/nocaptain-oss/archie-link/internal/deviceid"
)

func TestVerifyLeaseActiveGraceAndExpired(t *testing.T) {
	root, keyID, privateKey := leaseTestRoot(t)
	now := time.Now().UTC()
	lease := validLease(now)

	data := signLease(t, keyID, privateKey, lease)
	got, state, err := VerifyLease(data, root, LeaseRequest{
		SkillID: "archie-link", InstallID: "install-1",
		DevicePublicKey: "device-public", Feature: "network_apply",
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	if state != LeaseActive || got.LeaseID != lease.LeaseID {
		t.Fatalf("lease=%#v state=%s", got, state)
	}

	_, state, err = VerifyLease(data, root, LeaseRequest{
		SkillID: "archie-link", InstallID: "install-1",
		DevicePublicKey: "device-public", Feature: "network_apply",
	}, now.Add(10*24*time.Hour))
	if err != nil || state != LeaseOfflineGrace {
		t.Fatalf("state=%s err=%v", state, err)
	}

	_, _, err = VerifyLease(data, root, LeaseRequest{
		SkillID: "archie-link", InstallID: "install-1",
		DevicePublicKey: "device-public", Feature: "network_apply",
	}, now.Add(40*24*time.Hour))
	if !errors.Is(err, ErrExpired) {
		t.Fatalf("expected expired, got %v", err)
	}
}

func TestVerifyLeaseRejectsWrongDeviceFeatureAndRevocation(t *testing.T) {
	root, keyID, privateKey := leaseTestRoot(t)
	now := time.Now().UTC()
	data := signLease(t, keyID, privateKey, validLease(now))
	cases := []struct {
		name    string
		request LeaseRequest
		want    error
	}{
		{
			name: "device", want: ErrDeviceMismatch,
			request: LeaseRequest{SkillID: "archie-link", InstallID: "install-1", DevicePublicKey: "other", Feature: "network_apply"},
		},
		{
			name: "feature", want: ErrFeatureNotGranted,
			request: LeaseRequest{SkillID: "archie-link", InstallID: "install-1", DevicePublicKey: "device-public", Feature: "enterprise_admin"},
		},
		{
			name: "revoked", want: ErrRevoked,
			request: LeaseRequest{SkillID: "archie-link", InstallID: "install-1", DevicePublicKey: "device-public", Feature: "network_apply", RevocationFloor: 13},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, _, err := VerifyLease(data, root, tc.request, now); !errors.Is(err, tc.want) {
				t.Fatalf("got %v want %v", err, tc.want)
			}
		})
	}
}

func TestLeaseNeedsRenewalWithin48Hours(t *testing.T) {
	root, keyID, privateKey := leaseTestRoot(t)
	now := time.Now().UTC()
	lease := validLease(now)
	data := signLease(t, keyID, privateKey, lease)
	_, state, err := VerifyLease(data, root, LeaseRequest{
		SkillID: "archie-link", InstallID: "install-1",
		DevicePublicKey: "device-public", Feature: "network_apply",
	}, now.Add(6*24*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if state != LeaseNeedsRenewal {
		t.Fatalf("state=%s", state)
	}
}

func TestLeaseFileProviderBindsToDeviceIdentity(t *testing.T) {
	root, keyID, privateKey := leaseTestRoot(t)
	now := time.Now().UTC()
	manager := deviceid.New(deviceid.FileStore{Path: filepath.Join(t.TempDir(), "device.key")}, "install-1")
	identity, err := manager.LoadOrCreate(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	lease := validLease(now)
	lease.DevicePublicKey = identity.PublicKey
	data := signLease(t, keyID, privateKey, lease)
	provider := LeaseFileProvider{
		Data: data, TrustRoot: root, Device: manager,
		Now: func() time.Time { return now },
	}
	if _, err := provider.Verify(context.Background(), Request{
		SkillID: "archie-link", InstallID: "install-1", Feature: "network_apply",
	}); err != nil {
		t.Fatal(err)
	}
}

func TestLeaseFileProviderRejectsClockRollback(t *testing.T) {
	root, keyID, privateKey := leaseTestRoot(t)
	now := time.Now().UTC()
	manager := deviceid.New(deviceid.FileStore{Path: filepath.Join(t.TempDir(), "device.key")}, "install-1")
	identity, err := manager.LoadOrCreate(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	lease := validLease(now)
	lease.DevicePublicKey = identity.PublicKey
	provider := LeaseFileProvider{
		Data:       signLease(t, keyID, privateKey, lease),
		TrustRoot:  root,
		Device:     manager,
		LastSeenAt: now.Add(25 * time.Hour),
		Now:        func() time.Time { return now },
	}
	if _, err := provider.Verify(context.Background(), Request{
		SkillID: "archie-link", InstallID: "install-1", Feature: "network_apply",
	}); !errors.Is(err, ErrClockRollback) {
		t.Fatalf("expected clock rollback, got %v", err)
	}
}

func TestLeaseFileProviderRecordsObservationAndKeeps24HourTolerance(t *testing.T) {
	root, keyID, privateKey := leaseTestRoot(t)
	now := time.Now().UTC()
	manager := deviceid.New(deviceid.FileStore{Path: filepath.Join(t.TempDir(), "device.key")}, "install-1")
	identity, err := manager.LoadOrCreate(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	lease := validLease(now)
	lease.DevicePublicKey = identity.PublicKey
	var observed time.Time
	provider := LeaseFileProvider{
		Data:       signLease(t, keyID, privateKey, lease),
		TrustRoot:  root,
		Device:     manager,
		LastSeenAt: now.Add(23 * time.Hour),
		RecordSeen: func(at time.Time) error {
			observed = at
			return nil
		},
		Now: func() time.Time { return now },
	}
	if _, err := provider.Verify(context.Background(), Request{
		SkillID: "archie-link", InstallID: "install-1", Feature: "network_apply",
	}); err != nil {
		t.Fatal(err)
	}
	if !observed.Equal(now) {
		t.Fatalf("observed=%s", observed)
	}
}

func TestLeaseFileProviderRequiresOnlineReview(t *testing.T) {
	root, keyID, privateKey := leaseTestRoot(t)
	now := time.Now().UTC()
	manager := deviceid.New(deviceid.FileStore{Path: filepath.Join(t.TempDir(), "device.key")}, "install-1")
	identity, err := manager.LoadOrCreate(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	lease := validLease(now)
	lease.DevicePublicKey = identity.PublicKey
	provider := LeaseFileProvider{
		Data:                signLease(t, keyID, privateKey, lease),
		TrustRoot:           root,
		Device:              manager,
		ClockReviewRequired: true,
		Now:                 func() time.Time { return now },
	}
	if _, err := provider.Verify(context.Background(), Request{
		SkillID: "archie-link", InstallID: "install-1", Feature: "network_apply",
	}); !errors.Is(err, ErrClockRollback) {
		t.Fatalf("expected online review, got %v", err)
	}
}

func TestLeaseFileProviderFloorLoaderRatchetsRevocationFloor(t *testing.T) {
	root, keyID, privateKey := leaseTestRoot(t)
	now := time.Now().UTC()
	manager := deviceid.New(deviceid.FileStore{Path: filepath.Join(t.TempDir(), "device.key")}, "install-1")
	identity, err := manager.LoadOrCreate(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	lease := validLease(now) // RevocationEpoch=12
	lease.DevicePublicKey = identity.PublicKey
	data := signLease(t, keyID, privateKey, lease)
	request := Request{SkillID: "archie-link", InstallID: "install-1", Feature: "network_apply"}

	// FloorLoader 返回更高纪元：Verify 按在线刷新后的 floor 判撤销，无需重建 provider。
	revoked := LeaseFileProvider{
		Data:        data,
		TrustRoot:   root,
		Device:      manager,
		FloorLoader: func() (int, error) { return 13, nil },
		Now:         func() time.Time { return now },
	}
	if _, err := revoked.Verify(context.Background(), request); !errors.Is(err, ErrRevoked) {
		t.Fatalf("expected revoked, got %v", err)
	}

	// FloorLoader 失败：回退构造时快照（5 ≤ 12，验签通过）。
	alive := LeaseFileProvider{
		Data:            data,
		TrustRoot:       root,
		Device:          manager,
		RevocationFloor: 5,
		FloorLoader:     func() (int, error) { return 0, errors.New("store unavailable") },
		Now:             func() time.Time { return now },
	}
	if _, err := alive.Verify(context.Background(), request); err != nil {
		t.Fatalf("expected fallback to snapshot floor, got %v", err)
	}
}

func leaseTestRoot(t *testing.T) (TrustRoot, string, ed25519.PrivateKey) {
	t.Helper()
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	return TrustRoot{Keys: []TrustKey{{
		ID: "lease", PublicKey: publicKey,
		ValidFrom: now.Add(-time.Hour), ValidUntil: now.Add(365 * 24 * time.Hour),
	}}}, "lease", privateKey
}

func validLease(now time.Time) Lease {
	return Lease{
		SchemaVersion: "2",
		SkillID:       "archie-link", LicenseID: "lic-1", InstallID: "install-1",
		DevicePublicKey: "device-public", Plan: "family-annual-12",
		Features: []string{"network_apply"},
		IssuedAt: now, NotBefore: now.Add(-time.Minute), ExpiresAt: now.Add(7 * 24 * time.Hour),
		OfflineGraceUntil: now.Add(30 * 24 * time.Hour),
		LeaseID:           "lease-1", Nonce: "nonce-1", RevocationEpoch: 12,
		MaxDevices: 12, MaxNewDevicesPerYear: 12, MaxNewDevicesPer7Days: 3,
		DeviceRegistrationQuotaRemaining: 10,
	}
}

func signLease(t *testing.T, keyID string, key ed25519.PrivateKey, lease Lease) []byte {
	t.Helper()
	payload, err := json.Marshal(lease)
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(KeyedPayload{
		KeyID: keyID, Payload: payload,
		Signature: base64.StdEncoding.EncodeToString(ed25519.Sign(key, payload)),
	})
	if err != nil {
		t.Fatal(err)
	}
	return data
}
