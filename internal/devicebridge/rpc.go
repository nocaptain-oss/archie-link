package devicebridge

import (
	"bufio"
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

var (
	ErrDeviceNotFound    = errors.New("Android device was not found")
	ErrDeviceUnavailable = errors.New("Android device is not ready")
	ErrRPCUnavailable    = errors.New("companion RPC is unavailable")
	ErrPairingProof      = errors.New("companion pairing proof is invalid")
	ErrVPNUnsupported    = errors.New("device does not support system VPN authorization")
	ErrVPNAuthorization  = errors.New("device VPN authorization update failed")
	ErrNotPrepared       = errors.New("device VPN config is not prepared")
	ErrTunnelUnavailable = errors.New("device tunnel backend is unavailable")
	ErrTunnelStart       = errors.New("device tunnel failed to start")
)

const (
	companionPackage  = "com.archielink.companion"
	companionActivity = ".MainActivity"
	companionAction   = "com.archielink.companion.action.SHOW"
	companionPort     = "42817"
	maxRPCResponse    = 1 << 20
	rpcTimeout        = 10 * time.Second
)

type RPCConnector interface {
	Connect(context.Context, string, string) (RPCConnection, error)
}

type RPCConnection interface {
	Call(context.Context, Method, any) (Response, error)
	Close() error
}

type ADBRPCConnector struct {
	Runner Runner
	ADB    string
	Dial   func(context.Context, string, string) (net.Conn, error)
}

func (c *ADBRPCConnector) Connect(ctx context.Context, serial, token string) (RPCConnection, error) {
	if c == nil || c.Runner == nil || !validSerial(serial) || strings.TrimSpace(token) == "" {
		return nil, ErrRPCUnavailable
	}
	adb := strings.TrimSpace(c.ADB)
	if adb == "" {
		adb = "adb"
	}
	output, err := c.Runner.Run(ctx, adb, "-s", serial, "forward", "tcp:0", "tcp:"+companionPort)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrRPCUnavailable, err)
	}
	port, err := parseForwardPort(string(output))
	if err != nil {
		return nil, err
	}
	dial := c.Dial
	if dial == nil {
		dialer := net.Dialer{Timeout: 2 * time.Second}
		dial = dialer.DialContext
	}
	address := net.JoinHostPort("127.0.0.1", port)
	var lastErr error
	for attempt := 0; attempt < 10; attempt++ {
		conn, err := dial(ctx, "tcp", address)
		if err != nil {
			lastErr = fmt.Errorf("%w: %v", ErrRPCUnavailable, err)
			if !waitRetry(ctx) {
				break
			}
			continue
		}
		// The paired transport authenticates the TLS channel with a token-bound HMAC
		// over the observed certificate, so a prior certificate pin is unnecessary.
		tlsConn := tls.Client(conn, &tls.Config{
			InsecureSkipVerify: true, // Verified by the certificate-bound HMAC below.
			MinVersion:         tls.VersionTLS12,
		})
		if err := tlsConn.HandshakeContext(ctx); err != nil {
			_ = conn.Close()
			lastErr = fmt.Errorf("%w: TLS handshake: %v", ErrRPCUnavailable, err)
			if !waitRetry(ctx) {
				break
			}
			continue
		}
		certificateHash := sha256Hex(tlsConn.ConnectionState().PeerCertificates[0].Raw)
		client := &adbRPCConnection{
			conn:            tlsConn,
			reader:          bufio.NewReaderSize(tlsConn, 64*1024),
			runner:          c.Runner,
			adb:             adb,
			serial:          serial,
			port:            port,
			certificateHash: certificateHash,
		}
		if err := client.authenticate(ctx, token); err != nil {
			_ = tlsConn.Close()
			if errors.Is(err, ErrPairingProof) {
				_ = removeForward(context.Background(), c.Runner, adb, serial, port)
				return nil, err
			}
			lastErr = err
			if !waitRetry(ctx) {
				break
			}
			continue
		}
		return client, nil
	}
	_ = removeForward(context.Background(), c.Runner, adb, serial, port)
	if lastErr == nil {
		lastErr = ErrRPCUnavailable
	}
	return nil, lastErr
}

func waitRetry(ctx context.Context) bool {
	timer := time.NewTimer(200 * time.Millisecond)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

type adbRPCConnection struct {
	conn            *tls.Conn
	reader          *bufio.Reader
	runner          Runner
	adb             string
	serial          string
	port            string
	certificateHash string
}

func (c *adbRPCConnection) authenticate(ctx context.Context, token string) error {
	if err := c.conn.SetDeadline(rpcDeadline(ctx)); err != nil {
		return err
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return err
	}
	challenge := base64.RawURLEncoding.EncodeToString(raw)
	if _, err := io.WriteString(c.conn, challenge+"\n"); err != nil {
		return fmt.Errorf("%w: authenticate companion: %v", ErrRPCUnavailable, err)
	}
	supplied, err := c.reader.ReadBytes('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return fmt.Errorf("%w: read pairing proof: %v", ErrRPCUnavailable, err)
	}
	expected := transportProof(token, "server", challenge, c.certificateHash)
	if !hmac.Equal(bytes.TrimSpace(supplied), []byte(expected)) {
		return ErrPairingProof
	}
	if _, err := io.WriteString(c.conn,
		transportProof(token, "client", challenge, c.certificateHash)+"\n"); err != nil {
		return fmt.Errorf("%w: write pairing proof: %v", ErrRPCUnavailable, err)
	}
	return nil
}

func (c *adbRPCConnection) Call(ctx context.Context, method Method, payload any) (Response, error) {
	requestID, err := newRequestID()
	if err != nil {
		return Response{}, err
	}
	request := Request{
		SchemaVersion: SchemaVersion,
		RequestID:     requestID,
		Method:        method,
	}
	if payload != nil {
		raw, err := json.Marshal(payload)
		if err != nil {
			return Response{}, err
		}
		request.Payload = raw
	}
	if err := request.Validate(); err != nil {
		return Response{}, err
	}
	raw, err := json.Marshal(request)
	if err != nil {
		return Response{}, err
	}
	if err := c.conn.SetDeadline(rpcDeadline(ctx)); err != nil {
		return Response{}, err
	}
	if _, err := c.conn.Write(append(raw, '\n')); err != nil {
		return Response{}, fmt.Errorf("%w: write request: %v", ErrRPCUnavailable, err)
	}
	line, err := c.reader.ReadBytes('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return Response{}, fmt.Errorf("%w: read response: %v", ErrRPCUnavailable, err)
	}
	if len(line) == 0 || len(line) > maxRPCResponse {
		return Response{}, fmt.Errorf("%w: invalid response size", ErrRPCUnavailable)
	}
	var response Response
	if err := json.Unmarshal(line, &response); err != nil {
		return Response{}, fmt.Errorf("%w: decode response: %v", ErrRPCUnavailable, err)
	}
	if err := response.Validate(requestID); err != nil {
		return Response{}, err
	}
	return response, nil
}

func (c *adbRPCConnection) Close() error {
	_ = c.conn.Close()
	cleanupCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	return removeForward(cleanupCtx, c.runner, c.adb, c.serial, c.port)
}

func removeForward(ctx context.Context, runner Runner, adb, serial, port string) error {
	if runner == nil || !validSerial(serial) || !validSerial(port) {
		return nil
	}
	_, err := runner.Run(ctx, adb, "-s", serial, "forward", "--remove", "tcp:"+port)
	return err
}

func parseForwardPort(output string) (string, error) {
	lines := strings.Split(strings.TrimSpace(output), "\n")
	if len(lines) == 0 {
		return "", fmt.Errorf("%w: missing forward port", ErrRPCUnavailable)
	}
	port := strings.TrimSpace(lines[len(lines)-1])
	value, err := strconv.Atoi(port)
	if err != nil || value <= 0 || value > 65535 {
		return "", fmt.Errorf("%w: invalid forward port", ErrRPCUnavailable)
	}
	return port, nil
}

func newRequestID() (string, error) {
	raw := make([]byte, 18)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func rpcDeadline(ctx context.Context) time.Time {
	if deadline, ok := ctx.Deadline(); ok {
		return deadline
	}
	return time.Now().Add(rpcTimeout)
}

func validSerial(serial string) bool {
	serial = strings.TrimSpace(serial)
	return serial != "" && !strings.ContainsAny(serial, " \t\r\n") && !strings.HasPrefix(serial, "-")
}

type HelloPayload struct {
	DeviceID     string   `json:"device_id"`
	Alias        string   `json:"alias"`
	Fingerprint  string   `json:"fingerprint"`
	PairingProof string   `json:"pairing_proof"`
	ConfigKey    string   `json:"config_key"`
	Capabilities []string `json:"capabilities"`
}

type LaunchReport struct {
	SchemaVersion int    `json:"schema_version"`
	Serial        string `json:"serial"`
	Launched      bool   `json:"launched"`
}

type PairReport struct {
	SchemaVersion int       `json:"schema_version"`
	Serial        string    `json:"serial"`
	Platform      Platform  `json:"platform"`
	Code          string    `json:"code"`
	Fingerprint   string    `json:"fingerprint"`
	ExpiresAt     time.Time `json:"expires_at"`
}

type DeviceStatusReport struct {
	SchemaVersion int           `json:"schema_version"`
	Serial        string        `json:"serial"`
	State         string        `json:"state"`
	Status        StatusPayload `json:"status"`
}

type CloseReport struct {
	SchemaVersion int    `json:"schema_version"`
	Serial        string `json:"serial"`
	Closed        bool   `json:"closed"`
}

type ActionReport struct {
	SchemaVersion int           `json:"schema_version"`
	Serial        string        `json:"serial"`
	State         string        `json:"state"`
	Status        StatusPayload `json:"status"`
}

type CapabilitiesReport struct {
	SchemaVersion int             `json:"schema_version"`
	Serial        string          `json:"serial"`
	State         string          `json:"state"`
	Capabilities  json.RawMessage `json:"capabilities"`
}

type VPNAuthorizationReport struct {
	SchemaVersion int    `json:"schema_version"`
	Serial        string `json:"serial"`
	Mode          string `json:"mode"`
	Authorized    bool   `json:"authorized"`
}

func (h *Host) Launch(ctx context.Context, serial string) (LaunchReport, error) {
	return h.LaunchTool(ctx, serial, "adb")
}

func (h *Host) LaunchTool(ctx context.Context, serial, tool string) (LaunchReport, error) {
	var err error
	switch strings.ToLower(strings.TrimSpace(tool)) {
	case "", "adb":
		err = h.launchCompanion(ctx, serial, nil)
	case "hdc":
		err = h.launchHarmonyCompanion(ctx, serial)
	default:
		err = ErrUnsupportedDeviceTool
	}
	if err != nil {
		return LaunchReport{}, err
	}
	return LaunchReport{SchemaVersion: SchemaVersion, Serial: serial, Launched: true}, nil
}

func (h *Host) Pair(ctx context.Context, serial string) (PairReport, error) {
	if h == nil || h.Pairing == nil || h.Connector == nil {
		return PairReport{}, ErrRPCUnavailable
	}
	discovery, err := h.Discover(ctx)
	if err != nil {
		return PairReport{}, err
	}
	var device *Device
	for index := range discovery.Devices {
		if discovery.Devices[index].Serial == serial {
			device = &discovery.Devices[index]
			break
		}
	}
	if device == nil {
		return PairReport{}, ErrDeviceNotFound
	}
	if device.State != "device" {
		return PairReport{}, fmt.Errorf("%w: %s", ErrDeviceUnavailable, device.State)
	}
	offer, err := h.Pairing.Create(serial, device.Platform)
	if err != nil {
		return PairReport{}, err
	}
	extras := map[string]string{
		"session_id":    offer.SessionID,
		"pairing_code":  offer.Code,
		"session_token": offer.Token,
		"platform":      string(offer.Platform),
		"expires_at":    offer.ExpiresAt.Format(time.RFC3339),
	}
	if err := h.launchCompanion(ctx, serial, extras); err != nil {
		h.Pairing.Close(offer.SessionID)
		return PairReport{}, err
	}
	connection, err := h.Connector.Connect(ctx, serial, offer.Token)
	if err != nil {
		h.Pairing.Close(offer.SessionID)
		return PairReport{}, err
	}
	defer connection.Close()
	response, err := connection.Call(ctx, MethodHello, nil)
	if err != nil {
		h.Pairing.Close(offer.SessionID)
		return PairReport{}, err
	}
	if !response.OK || response.State != "ready" {
		h.Pairing.Close(offer.SessionID)
		return PairReport{}, fmt.Errorf("%w: hello state=%s error=%s",
			ErrRPCUnavailable, response.State, response.ErrorCode)
	}
	var hello HelloPayload
	if err := json.Unmarshal(response.Payload, &hello); err != nil || hello.DeviceID == "" ||
		hello.Fingerprint == "" || hello.ConfigKey == "" ||
		hello.PairingProof != makePairingProof(offer.Token, offer.Code, hello.DeviceID) {
		h.Pairing.Close(offer.SessionID)
		return PairReport{}, ErrPairingProof
	}
	grant, err := h.Pairing.Confirm(offer.SessionID, offer.Code, hello.Fingerprint)
	if err != nil {
		return PairReport{}, err
	}
	if err := h.Sessions.Save(Session{
		SchemaVersion: SchemaVersion,
		Serial:        grant.Serial,
		Platform:      grant.Platform,
		Token:         grant.Token,
		Fingerprint:   grant.Fingerprint,
		DeviceID:      hello.DeviceID,
		ConfigKey:     hello.ConfigKey,
		ExpiresAt:     grant.ExpiresAt,
	}); err != nil {
		return PairReport{}, err
	}
	return PairReport{
		SchemaVersion: SchemaVersion,
		Serial:        grant.Serial,
		Platform:      grant.Platform,
		Code:          offer.Code,
		Fingerprint:   grant.Fingerprint,
		ExpiresAt:     grant.ExpiresAt,
	}, nil
}

func (h *Host) Status(ctx context.Context, serial string) (DeviceStatusReport, error) {
	if h == nil || h.Connector == nil {
		return DeviceStatusReport{}, ErrRPCUnavailable
	}
	session, err := h.Sessions.Load(serial, time.Now())
	if err != nil {
		return DeviceStatusReport{}, err
	}
	connection, err := h.Connector.Connect(ctx, serial, session.Token)
	if err != nil {
		return DeviceStatusReport{}, err
	}
	defer connection.Close()
	response, err := connection.Call(ctx, MethodStatus, nil)
	if err != nil {
		return DeviceStatusReport{}, err
	}
	if !response.OK {
		return DeviceStatusReport{}, fmt.Errorf("%w: status state=%s error=%s",
			ErrRPCUnavailable, response.State, response.ErrorCode)
	}
	var status StatusPayload
	if err := json.Unmarshal(response.Payload, &status); err != nil {
		return DeviceStatusReport{}, ErrInvalidRequest
	}
	return DeviceStatusReport{
		SchemaVersion: SchemaVersion,
		Serial:        serial,
		State:         response.State,
		Status:        status,
	}, nil
}

func (h *Host) Capabilities(ctx context.Context, serial string) (CapabilitiesReport, error) {
	session, connection, err := h.openSession(ctx, serial)
	if err != nil {
		return CapabilitiesReport{}, err
	}
	_ = session
	defer connection.Close()
	response, err := connection.Call(ctx, MethodCapabilities, nil)
	if err != nil {
		return CapabilitiesReport{}, err
	}
	if !response.OK {
		return CapabilitiesReport{}, fmt.Errorf("%w: capabilities state=%s error=%s",
			ErrRPCUnavailable, response.State, response.ErrorCode)
	}
	return CapabilitiesReport{
		SchemaVersion: SchemaVersion,
		Serial:        serial,
		State:         response.State,
		Capabilities:  response.Payload,
	}, nil
}

func (h *Host) SetVPNAuthorization(
	ctx context.Context, serial string, allow bool,
) (VPNAuthorizationReport, error) {
	if h == nil || h.Runner == nil || !validSerial(serial) {
		return VPNAuthorizationReport{}, ErrVPNAuthorization
	}
	adb := strings.TrimSpace(h.ADB)
	if adb == "" {
		adb = "adb"
	}
	mode := "default"
	if allow {
		mode = "allow"
	}
	output, err := h.Runner.Run(
		ctx, adb, "-s", serial, "shell", "appops", "set",
		companionPackage, "ACTIVATE_VPN", mode,
	)
	if err != nil {
		return VPNAuthorizationReport{}, fmt.Errorf(
			"%w: %v: %s", ErrVPNAuthorization, err, strings.TrimSpace(string(output)),
		)
	}
	output, err = h.Runner.Run(
		ctx, adb, "-s", serial, "shell", "appops", "get",
		companionPackage, "ACTIVATE_VPN",
	)
	if err != nil {
		return VPNAuthorizationReport{}, fmt.Errorf(
			"%w: %v: %s", ErrVPNAuthorization, err, strings.TrimSpace(string(output)),
		)
	}
	value := strings.TrimSpace(string(output))
	authorized := strings.Contains(value, ": allow") || strings.HasPrefix(value, "allow;")
	if authorized != allow {
		return VPNAuthorizationReport{}, fmt.Errorf(
			"%w: expected mode %s, got %s", ErrVPNAuthorization, mode, value,
		)
	}
	return VPNAuthorizationReport{
		SchemaVersion: SchemaVersion,
		Serial:        serial,
		Mode:          mode,
		Authorized:    authorized,
	}, nil
}

func (h *Host) Prepare(ctx context.Context, serial, profile string, config []byte) (ActionReport, error) {
	if len(config) == 0 || len(config) > 64*1024 {
		return ActionReport{}, ErrInvalidRequest
	}
	session, connection, err := h.openSession(ctx, serial)
	if err != nil {
		return ActionReport{}, err
	}
	defer connection.Close()
	rawKey, err := base64.StdEncoding.DecodeString(session.ConfigKey)
	if err != nil {
		return ActionReport{}, ErrInvalidRequest
	}
	parsed, err := x509.ParsePKIXPublicKey(rawKey)
	if err != nil {
		return ActionReport{}, ErrInvalidRequest
	}
	publicKey, ok := parsed.(*rsa.PublicKey)
	if !ok {
		return ActionReport{}, ErrInvalidRequest
	}
	aesKey := make([]byte, 32)
	if _, err := rand.Read(aesKey); err != nil {
		return ActionReport{}, err
	}
	block, err := aes.NewCipher(aesKey)
	if err != nil {
		return ActionReport{}, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return ActionReport{}, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return ActionReport{}, err
	}
	encryptedConfig := gcm.Seal(nil, nonce, config, nil)
	encryptedKey, err := rsa.EncryptOAEP(sha1.New(), rand.Reader, publicKey, aesKey, nil)
	if err != nil {
		return ActionReport{}, err
	}
	if strings.TrimSpace(profile) == "" {
		profile = "default"
	}
	expiresAt := time.Now().Add(5 * time.Minute)
	if session.ExpiresAt.Before(expiresAt) {
		expiresAt = session.ExpiresAt
	}
	response, err := connection.Call(ctx, MethodPrepare, PreparePayload{
		Profile:         profile,
		EncryptedKey:    base64.StdEncoding.EncodeToString(encryptedKey),
		EncryptedConfig: base64.StdEncoding.EncodeToString(encryptedConfig),
		Nonce:           base64.StdEncoding.EncodeToString(nonce),
		ExpiresAt:       expiresAt.UTC().Format(time.RFC3339),
	})
	if err != nil {
		return ActionReport{}, err
	}
	if !response.OK {
		return ActionReport{}, fmt.Errorf("%w: prepare state=%s error=%s",
			ErrRPCUnavailable, response.State, response.ErrorCode)
	}
	var status StatusPayload
	_ = json.Unmarshal(response.Payload, &status)
	return ActionReport{
		SchemaVersion: SchemaVersion, Serial: serial, State: response.State, Status: status,
	}, nil
}

func (h *Host) Start(ctx context.Context, serial string) (ActionReport, error) {
	return h.callAction(ctx, serial, MethodStart)
}

func (h *Host) Stop(ctx context.Context, serial string) (ActionReport, error) {
	return h.callAction(ctx, serial, MethodStop)
}

func (h *Host) callAction(ctx context.Context, serial string, method Method) (ActionReport, error) {
	_, connection, err := h.openSession(ctx, serial)
	if err != nil {
		return ActionReport{}, err
	}
	defer connection.Close()
	response, err := connection.Call(ctx, method, nil)
	if err != nil {
		return ActionReport{}, err
	}
	if !response.OK {
		switch response.ErrorCode {
		case "vpn_unsupported":
			return ActionReport{}, ErrVPNUnsupported
		case "not_prepared":
			return ActionReport{}, ErrNotPrepared
		case "tunnel_backend_unavailable":
			return ActionReport{}, ErrTunnelUnavailable
		case "tunnel_start_failed":
			return ActionReport{}, fmt.Errorf("%w: %s", ErrTunnelStart, response.Message)
		}
		return ActionReport{}, fmt.Errorf("%w: %s state=%s error=%s message=%s",
			ErrRPCUnavailable, method, response.State, response.ErrorCode, response.Message)
	}
	var status StatusPayload
	_ = json.Unmarshal(response.Payload, &status)
	return ActionReport{
		SchemaVersion: SchemaVersion, Serial: serial, State: response.State, Status: status,
	}, nil
}

func (h *Host) openSession(ctx context.Context, serial string) (Session, RPCConnection, error) {
	if h == nil || h.Connector == nil {
		return Session{}, nil, ErrRPCUnavailable
	}
	session, err := h.Sessions.Load(serial, time.Now())
	if err != nil {
		return Session{}, nil, err
	}
	connection, err := h.Connector.Connect(ctx, serial, session.Token)
	if err != nil {
		return Session{}, nil, err
	}
	return session, connection, nil
}

func (h *Host) CloseSession(ctx context.Context, serial string) (CloseReport, error) {
	if h == nil || h.Connector == nil {
		return CloseReport{}, ErrRPCUnavailable
	}
	session, err := h.Sessions.Load(serial, time.Now())
	if errors.Is(err, ErrSessionExpired) {
		return CloseReport{SchemaVersion: SchemaVersion, Serial: serial, Closed: true}, nil
	}
	if err != nil {
		return CloseReport{}, err
	}
	connection, connectErr := h.Connector.Connect(ctx, serial, session.Token)
	var callErr error
	if connectErr == nil {
		var response Response
		response, callErr = connection.Call(ctx, MethodClose, nil)
		if callErr == nil && !response.OK {
			callErr = fmt.Errorf("%w: close state=%s error=%s",
				ErrRPCUnavailable, response.State, response.ErrorCode)
		}
		_ = connection.Close()
	}
	deleteErr := h.Sessions.Delete()
	if connectErr != nil {
		return CloseReport{}, connectErr
	}
	if callErr != nil {
		return CloseReport{}, callErr
	}
	if deleteErr != nil {
		return CloseReport{}, deleteErr
	}
	return CloseReport{SchemaVersion: SchemaVersion, Serial: serial, Closed: true}, nil
}

func (h *Host) launchCompanion(ctx context.Context, serial string, extras map[string]string) error {
	if h == nil || h.Runner == nil || !validSerial(serial) {
		return ErrRPCUnavailable
	}
	adb := strings.TrimSpace(h.ADB)
	if adb == "" {
		adb = "adb"
	}
	args := []string{
		"-s", serial, "shell", "am", "start", "-W",
		"-a", companionAction,
		"-n", companionPackage + "/" + companionActivity,
	}
	for _, key := range []string{"session_id", "pairing_code", "session_token", "platform", "expires_at"} {
		value, ok := extras[key]
		if !ok {
			continue
		}
		if !validExtra(value) {
			return ErrInvalidRequest
		}
		args = append(args, "--es", key, value)
	}
	output, err := h.Runner.Run(ctx, adb, args...)
	if err != nil {
		return fmt.Errorf("%w: %v: %s", ErrRPCUnavailable, err, strings.TrimSpace(string(output)))
	}
	if !strings.Contains(string(output), "Status: ok") && !strings.Contains(string(output), "Activity:") {
		return fmt.Errorf("%w: %s", ErrRPCUnavailable, strings.TrimSpace(string(output)))
	}
	return nil
}

func (h *Host) launchHarmonyCompanion(ctx context.Context, serial string) error {
	if h == nil || h.Runner == nil || !validSerial(serial) {
		return ErrHDCLaunchFailed
	}
	hdc := strings.TrimSpace(h.HDC)
	if hdc == "" {
		hdc = "hdc"
	}
	output, err := h.Runner.Run(
		ctx, hdc, "-t", serial,
		"shell", "aa", "start", "-a", "EntryAbility", "-b", companionPackage, "-D",
	)
	if err != nil {
		return fmt.Errorf(
			"%w: %v: %s", ErrHDCLaunchFailed, err, strings.TrimSpace(string(output)),
		)
	}
	return nil
}

func validExtra(value string) bool {
	if value == "" || len(value) > 256 {
		return false
	}
	for _, char := range value {
		if (char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z') ||
			(char >= '0' && char <= '9') || strings.ContainsRune("-._:+", char) {
			continue
		}
		return false
	}
	return true
}

func makePairingProof(token, code, deviceID string) string {
	sum := sha256.Sum256([]byte(token + ":" + code + ":" + deviceID))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func transportProof(token, role, challenge, certificateHash string) string {
	mac := hmac.New(sha256.New, []byte(token))
	_, _ = mac.Write([]byte(role + ":" + challenge + ":" + certificateHash))
	return hex.EncodeToString(mac.Sum(nil))
}

func sha256Hex(value []byte) string {
	sum := sha256.Sum256(value)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func defaultDeviceSessionPath() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "archie-link", "device-session.json")
}
