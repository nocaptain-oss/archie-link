package entitlement

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"time"
)

type SignedPayload struct {
	Payload   json.RawMessage `json:"payload"`
	Signature string          `json:"signature"`
}

type SignedProvider struct {
	Data      []byte
	PublicKey ed25519.PublicKey
	Request   Request
	Now       func() time.Time
}

type PinnedSignedProvider struct {
	Data      []byte
	TrustRoot TrustRoot
	Now       func() time.Time
}

func (p PinnedSignedProvider) Verify(ctx context.Context, request Request) (Entitlement, error) {
	if err := ctx.Err(); err != nil {
		return Entitlement{}, err
	}
	now := time.Now()
	if p.Now != nil {
		now = p.Now()
	}
	payload, _, err := p.TrustRoot.VerifyKeyed(p.Data, now)
	if err != nil {
		return Entitlement{}, err
	}
	var entitlement Entitlement
	if err := json.Unmarshal(payload, &entitlement); err != nil {
		return Entitlement{}, ErrMalformed
	}
	if err := Validate(entitlement, request, now); err != nil {
		return Entitlement{}, err
	}
	return entitlement, nil
}

func (p SignedProvider) Verify(ctx context.Context, request Request) (Entitlement, error) {
	if err := ctx.Err(); err != nil {
		return Entitlement{}, err
	}
	entitlement, err := VerifyEd25519(p.Data, p.PublicKey)
	if err != nil {
		return Entitlement{}, err
	}
	now := time.Now()
	if p.Now != nil {
		now = p.Now()
	}
	if err := Validate(entitlement, request, now); err != nil {
		return Entitlement{}, err
	}
	return entitlement, nil
}

func VerifyEd25519(data []byte, publicKey ed25519.PublicKey) (Entitlement, error) {
	if len(publicKey) != ed25519.PublicKeySize {
		return Entitlement{}, ErrMalformed
	}
	var signed SignedPayload
	if err := json.Unmarshal(data, &signed); err != nil {
		return Entitlement{}, ErrMalformed
	}
	if len(signed.Payload) == 0 || signed.Signature == "" {
		return Entitlement{}, ErrMalformed
	}
	signature, err := base64.StdEncoding.DecodeString(signed.Signature)
	if err != nil {
		return Entitlement{}, ErrMalformed
	}
	if !ed25519.Verify(publicKey, signed.Payload, signature) {
		return Entitlement{}, ErrInvalidSignature
	}
	var entitlement Entitlement
	if err := json.Unmarshal(signed.Payload, &entitlement); err != nil {
		return Entitlement{}, ErrMalformed
	}
	if !entitlement.ExpiresAt.After(entitlement.IssuedAt) {
		return Entitlement{}, ErrMalformed
	}
	return entitlement, nil
}
