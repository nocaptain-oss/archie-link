package entitlement

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

var (
	ErrTrustRootUnavailable = errors.New("embedded trust root is not configured")
	ErrUnknownTrustKey      = errors.New("verification key is not trusted")
	ErrTrustKeyNotYetValid  = errors.New("verification key is not valid yet")
	ErrTrustKeyExpired      = errors.New("verification key has expired")
)

var (
	embeddedKeyID             string
	embeddedPublicKey         string
	embeddedPreviousKeyID     string
	embeddedPreviousPublicKey string
	embeddedPreviousValidTill string
)

type KeyedPayload struct {
	KeyID     string          `json:"key_id"`
	Payload   json.RawMessage `json:"payload"`
	Signature string          `json:"signature"`
}

type TrustKey struct {
	ID         string
	PublicKey  ed25519.PublicKey
	ValidFrom  time.Time
	ValidUntil time.Time
}

type TrustRoot struct {
	Keys []TrustKey
}

func (r TrustRoot) VerifyKeyed(data []byte, now time.Time) (json.RawMessage, string, error) {
	var envelope KeyedPayload
	if err := json.Unmarshal(data, &envelope); err != nil || envelope.KeyID == "" {
		return nil, "", ErrMalformed
	}
	key, ok := r.key(envelope.KeyID)
	if !ok {
		return nil, "", ErrUnknownTrustKey
	}
	if !key.ValidFrom.IsZero() && now.Before(key.ValidFrom) {
		return nil, "", ErrTrustKeyNotYetValid
	}
	if !key.ValidUntil.IsZero() && !now.Before(key.ValidUntil) {
		return nil, "", ErrTrustKeyExpired
	}
	signature, err := base64.StdEncoding.DecodeString(envelope.Signature)
	if err != nil || len(envelope.Payload) == 0 {
		return nil, "", ErrMalformed
	}
	if !ed25519.Verify(key.PublicKey, envelope.Payload, signature) {
		return nil, "", ErrInvalidSignature
	}
	return envelope.Payload, envelope.KeyID, nil
}

func (r TrustRoot) key(id string) (TrustKey, bool) {
	for _, key := range r.Keys {
		if key.ID == id && len(key.PublicKey) == ed25519.PublicKeySize {
			return key, true
		}
	}
	return TrustKey{}, false
}

func EmbeddedTrustRoot() (TrustRoot, error) {
	current, err := decodePublicKeyText(embeddedPublicKey)
	if err != nil || strings.TrimSpace(embeddedKeyID) == "" {
		return TrustRoot{}, ErrTrustRootUnavailable
	}
	root := TrustRoot{Keys: []TrustKey{{
		ID: embeddedKeyID, PublicKey: current,
	}}}
	if strings.TrimSpace(embeddedPreviousPublicKey) != "" &&
		strings.TrimSpace(embeddedPreviousKeyID) != "" {
		previous, err := decodePublicKeyText(embeddedPreviousPublicKey)
		if err != nil {
			return TrustRoot{}, err
		}
		validUntil, err := time.Parse(time.RFC3339, embeddedPreviousValidTill)
		if err != nil {
			return TrustRoot{}, err
		}
		root.Keys = append(root.Keys, TrustKey{
			ID: embeddedPreviousKeyID, PublicKey: previous, ValidUntil: validUntil,
		})
	}
	return root, nil
}

func decodePublicKeyText(value string) (ed25519.PublicKey, error) {
	value = strings.TrimSpace(value)
	if raw, err := base64.StdEncoding.DecodeString(value); err == nil && len(raw) == ed25519.PublicKeySize {
		return ed25519.PublicKey(raw), nil
	}
	if raw, err := hex.DecodeString(value); err == nil && len(raw) == ed25519.PublicKeySize {
		return ed25519.PublicKey(raw), nil
	}
	return nil, ErrMalformed
}
