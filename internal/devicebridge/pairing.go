package devicebridge

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

var (
	ErrPairingExpired     = errors.New("device pairing session expired")
	ErrPairingCode        = errors.New("device pairing code is invalid")
	ErrPairingAttempts    = errors.New("device pairing attempts exceeded")
	ErrPairingFingerprint = errors.New("device fingerprint is required")
)

const defaultPairingTTL = 5 * time.Minute
const maxPairingAttempts = 5

type PairingOffer struct {
	SchemaVersion int       `json:"schema_version"`
	SessionID     string    `json:"session_id"`
	Code          string    `json:"code"`
	Token         string    `json:"-"`
	Serial        string    `json:"serial"`
	Platform      Platform  `json:"platform"`
	ExpiresAt     time.Time `json:"expires_at"`
}

type PairingGrant struct {
	SchemaVersion int       `json:"schema_version"`
	SessionID     string    `json:"session_id"`
	Token         string    `json:"token"`
	Serial        string    `json:"serial"`
	Platform      Platform  `json:"platform"`
	Fingerprint   string    `json:"fingerprint"`
	ExpiresAt     time.Time `json:"expires_at"`
}

type pairingSession struct {
	offer    PairingOffer
	codeHash [32]byte
	attempts int
}

type PairingManager struct {
	mu       sync.Mutex
	now      func() time.Time
	ttl      time.Duration
	sessions map[string]*pairingSession
	active   map[string]string
}

func NewPairingManager(now func() time.Time, ttl time.Duration) *PairingManager {
	if now == nil {
		now = time.Now
	}
	if ttl <= 0 {
		ttl = defaultPairingTTL
	}
	return &PairingManager{
		now: now, ttl: ttl, sessions: map[string]*pairingSession{}, active: map[string]string{},
	}
}

func (m *PairingManager) Create(serial string, platform Platform) (PairingOffer, error) {
	serial = strings.TrimSpace(serial)
	if serial == "" {
		return PairingOffer{}, ErrInvalidRequest
	}
	sessionID, err := randomToken(18)
	if err != nil {
		return PairingOffer{}, err
	}
	code, err := randomCode()
	if err != nil {
		return PairingOffer{}, err
	}
	token, err := randomToken(32)
	if err != nil {
		return PairingOffer{}, err
	}
	offer := PairingOffer{
		SchemaVersion: SchemaVersion,
		SessionID:     sessionID,
		Code:          code,
		Token:         token,
		Serial:        serial,
		Platform:      platform,
		ExpiresAt:     m.now().Add(m.ttl).UTC(),
	}
	m.mu.Lock()
	if previous := m.active[serial]; previous != "" {
		delete(m.sessions, previous)
	}
	m.sessions[sessionID] = &pairingSession{offer: offer, codeHash: sha256.Sum256([]byte(code))}
	m.active[serial] = sessionID
	m.mu.Unlock()
	return offer, nil
}

func (m *PairingManager) Confirm(sessionID, code, fingerprint string) (PairingGrant, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	session := m.sessions[strings.TrimSpace(sessionID)]
	if session == nil {
		return PairingGrant{}, ErrPairingCode
	}
	if !m.now().Before(session.offer.ExpiresAt) {
		m.removeLocked(session.offer.SessionID)
		return PairingGrant{}, ErrPairingExpired
	}
	if session.attempts >= maxPairingAttempts {
		m.removeLocked(session.offer.SessionID)
		return PairingGrant{}, ErrPairingAttempts
	}
	fingerprint = strings.TrimSpace(fingerprint)
	if fingerprint == "" {
		session.attempts++
		return PairingGrant{}, ErrPairingFingerprint
	}
	hash := sha256.Sum256([]byte(code))
	if subtle.ConstantTimeCompare(hash[:], session.codeHash[:]) != 1 {
		session.attempts++
		if session.attempts >= maxPairingAttempts {
			m.removeLocked(session.offer.SessionID)
			return PairingGrant{}, ErrPairingAttempts
		}
		return PairingGrant{}, ErrPairingCode
	}
	m.removeLocked(session.offer.SessionID)
	return PairingGrant{
		SchemaVersion: SchemaVersion,
		SessionID:     session.offer.SessionID,
		Token:         session.offer.Token,
		Serial:        session.offer.Serial,
		Platform:      session.offer.Platform,
		Fingerprint:   fingerprint,
		ExpiresAt:     session.offer.ExpiresAt,
	}, nil
}

func (m *PairingManager) Close(sessionID string) {
	m.mu.Lock()
	m.removeLocked(strings.TrimSpace(sessionID))
	m.mu.Unlock()
}

func (m *PairingManager) removeLocked(sessionID string) {
	session := m.sessions[sessionID]
	if session == nil {
		return
	}
	delete(m.sessions, sessionID)
	if m.active[session.offer.Serial] == sessionID {
		delete(m.active, session.offer.Serial)
	}
}

func randomCode() (string, error) {
	var value [4]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", err
	}
	code := int(value[0])<<16 | int(value[1])<<8 | int(value[2])
	return fmt.Sprintf("%06d", code%1000000), nil
}

func randomToken(size int) (string, error) {
	raw := make([]byte, size)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}
