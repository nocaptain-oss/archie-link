// Package devicebridge defines the semantic RPC shared by the host CLI and
// signed companion apps. It intentionally has no arbitrary command method.
package devicebridge

import (
	"bytes"
	"encoding/json"
	"errors"
	"regexp"
	"strings"
)

const SchemaVersion = 1

var (
	ErrInvalidRequest    = errors.New("device bridge request is invalid")
	ErrUnsupportedMethod = errors.New("device bridge method is not allowed")
)

type Platform string

const (
	PlatformAndroid   Platform = "android"
	PlatformAndroidTV Platform = "android-tv"
	PlatformHarmonyOS Platform = "harmonyos"
	PlatformIOS       Platform = "ios"
)

type Method string

const (
	MethodHello        Method = "hello"
	MethodCapabilities Method = "capabilities"
	MethodPrepare      Method = "prepare"
	MethodStart        Method = "start"
	MethodStatus       Method = "status"
	MethodStop         Method = "stop"
	MethodClose        Method = "close"
)

var allowedMethods = map[Method]struct{}{
	MethodHello:        {},
	MethodCapabilities: {},
	MethodPrepare:      {},
	MethodStart:        {},
	MethodStatus:       {},
	MethodStop:         {},
	MethodClose:        {},
}

var requestIDPattern = regexp.MustCompile(`^[A-Za-z0-9._:-]{8,128}$`)

type Request struct {
	SchemaVersion int             `json:"schema_version"`
	RequestID     string          `json:"request_id"`
	Method        Method          `json:"method"`
	Payload       json.RawMessage `json:"payload,omitempty"`
}

func (r Request) Validate() error {
	if r.SchemaVersion != SchemaVersion || !requestIDPattern.MatchString(r.RequestID) {
		return ErrInvalidRequest
	}
	if _, ok := allowedMethods[r.Method]; !ok {
		return ErrUnsupportedMethod
	}
	if len(r.Payload) > 0 {
		trimmed := bytes.TrimSpace(r.Payload)
		if !json.Valid(trimmed) {
			return ErrInvalidRequest
		}
	}
	return nil
}

type Response struct {
	SchemaVersion int             `json:"schema_version"`
	RequestID     string          `json:"request_id"`
	OK            bool            `json:"ok"`
	State         string          `json:"state"`
	ErrorCode     string          `json:"error_code,omitempty"`
	Message       string          `json:"message,omitempty"`
	Payload       json.RawMessage `json:"payload,omitempty"`
}

func (r Response) Validate(requestID string) error {
	if r.SchemaVersion != SchemaVersion || r.RequestID != requestID || strings.TrimSpace(r.State) == "" {
		return ErrInvalidRequest
	}
	if len(r.Payload) > 0 && !json.Valid(bytes.TrimSpace(r.Payload)) {
		return ErrInvalidRequest
	}
	return nil
}

type PreparePayload struct {
	Profile         string `json:"profile"`
	EncryptedKey    string `json:"encrypted_key"`
	EncryptedConfig string `json:"encrypted_config"`
	Nonce           string `json:"nonce"`
	ExpiresAt       string `json:"expires_at"`
}

type StatusPayload struct {
	Platform      Platform `json:"platform"`
	VPNSupported  bool     `json:"vpn_supported"`
	VPNAuthorized bool     `json:"vpn_authorized"`
	Prepared      bool     `json:"prepared"`
	Connection    string   `json:"connection"`
	Health        string   `json:"health"`
	TunnelBackend string   `json:"tunnel_backend"`
	RoutedApps    int      `json:"routed_app_count"`
}
