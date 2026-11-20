package devicebridge

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha1"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type rpcTestRunner struct {
	code string
	args []string
}

func (r *rpcTestRunner) Run(_ context.Context, name string, args ...string) ([]byte, error) {
	r.args = append([]string(nil), args...)
	if len(args) > 1 && args[0] == "devices" {
		return []byte("List of devices attached\ntvbox-1 device product:VIDAA_TV model:VIDAA_TV device:MTK9255\n"), nil
	}
	if len(args) >= 4 && args[0] == "-s" && args[2] == "shell" {
		for index := 0; index+1 < len(args); index++ {
			if args[index] == "--es" && index+2 < len(args) && args[index+1] == "pairing_code" {
				r.code = args[index+2]
			}
		}
		return []byte("Starting: Intent\nStatus: ok\nActivity: com.archielink.companion/.MainActivity\n"), nil
	}
	return nil, errors.New("unexpected command: " + name + " " + strings.Join(args, " "))
}

type rpcTestConnector struct {
	runner      *rpcTestRunner
	method      []Method
	badProof    bool
	configKey   string
	privateKey  *rsa.PrivateKey
	config      []byte
	lastPayload any
}

func (c *rpcTestConnector) Connect(_ context.Context, _ string, token string) (RPCConnection, error) {
	return &rpcTestConnection{connector: c, token: token}, nil
}

type rpcTestConnection struct {
	connector *rpcTestConnector
	token     string
}

func (c *rpcTestConnection) Call(_ context.Context, method Method, payload any) (Response, error) {
	c.connector.method = append(c.connector.method, method)
	c.connector.lastPayload = payload
	switch method {
	case MethodHello:
		deviceID := "device-1"
		proof := makePairingProof(c.token, c.connector.runner.code, deviceID)
		if c.connector.badProof {
			proof = "sha256:invalid"
		}
		payload, _ := json.Marshal(HelloPayload{
			DeviceID:     deviceID,
			Alias:        "VIDAA TV",
			Fingerprint:  "sha256:device",
			ConfigKey:    c.connector.configKey,
			PairingProof: proof,
		})
		return Response{SchemaVersion: SchemaVersion, OK: true, State: "ready", Payload: payload}, nil
	case MethodStatus:
		payload, _ := json.Marshal(StatusPayload{
			Platform: PlatformAndroidTV, Connection: "disconnected", Health: "unknown",
		})
		return Response{SchemaVersion: SchemaVersion, OK: true, State: "ready", Payload: payload}, nil
	case MethodCapabilities:
		return Response{
			SchemaVersion: SchemaVersion, OK: true, State: "ready",
			Payload: json.RawMessage(`{"methods":["prepare","status","stop","close"]}`),
		}, nil
	case MethodPrepare:
		prepare, ok := c.connector.lastPayload.(PreparePayload)
		if !ok {
			return Response{}, ErrInvalidRequest
		}
		encryptedKey, err := base64.StdEncoding.DecodeString(prepare.EncryptedKey)
		if err != nil {
			return Response{}, err
		}
		aesKey, err := rsa.DecryptOAEP(sha1.New(), nil, c.connector.privateKey, encryptedKey, nil)
		if err != nil {
			return Response{}, err
		}
		nonce, err := base64.StdEncoding.DecodeString(prepare.Nonce)
		if err != nil {
			return Response{}, err
		}
		encryptedConfig, err := base64.StdEncoding.DecodeString(prepare.EncryptedConfig)
		if err != nil {
			return Response{}, err
		}
		block, err := aes.NewCipher(aesKey)
		if err != nil {
			return Response{}, err
		}
		gcm, err := cipher.NewGCM(block)
		if err != nil {
			return Response{}, err
		}
		plain, err := gcm.Open(nil, nonce, encryptedConfig, nil)
		if err != nil {
			return Response{}, err
		}
		c.connector.config = plain
		payload, _ := json.Marshal(StatusPayload{Platform: PlatformAndroidTV, Prepared: true})
		return Response{SchemaVersion: SchemaVersion, OK: true, State: "prepared", Payload: payload}, nil
	case MethodStart:
		payload, _ := json.Marshal(StatusPayload{
			Platform: PlatformAndroidTV, Connection: "disconnected", TunnelBackend: "unavailable",
		})
		return Response{SchemaVersion: SchemaVersion, OK: true, State: "authorization_required", Payload: payload}, nil
	case MethodStop:
		payload, _ := json.Marshal(StatusPayload{
			Platform: PlatformAndroidTV, Connection: "disconnected", TunnelBackend: "unavailable",
		})
		return Response{SchemaVersion: SchemaVersion, OK: true, State: "stopped", Payload: payload}, nil
	case MethodClose:
		return Response{SchemaVersion: SchemaVersion, OK: true, State: "closed"}, nil
	default:
		return Response{}, ErrUnsupportedMethod
	}
}

func (c *rpcTestConnection) Close() error { return nil }

func TestHostPairStatusAndClosePersistOnlyPrivateSession(t *testing.T) {
	now := time.Now().UTC()
	runner := &rpcTestRunner{}
	connector := &rpcTestConnector{runner: runner}
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	connector.privateKey = key
	public, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	connector.configKey = base64.StdEncoding.EncodeToString(public)
	host := NewHost(runner)
	host.Connector = connector
	host.Pairing = NewPairingManager(func() time.Time { return now }, time.Minute)
	host.Sessions = SessionStore{Path: filepath.Join(t.TempDir(), "device-session.json")}

	pair, err := host.Pair(context.Background(), "tvbox-1")
	if err != nil {
		t.Fatal(err)
	}
	if pair.Platform != PlatformAndroidTV || pair.Fingerprint != "sha256:device" || pair.Code == "" {
		t.Fatalf("pair=%#v", pair)
	}
	if runner.code != pair.Code {
		t.Fatalf("device code=%s host code=%s", runner.code, pair.Code)
	}
	session, err := host.Sessions.Load("tvbox-1", now)
	if err != nil {
		t.Fatal(err)
	}
	if session.DeviceID != "device-1" || session.Token == "" {
		t.Fatalf("session=%#v", session)
	}
	if session.ConfigKey != connector.configKey {
		t.Fatalf("config key not persisted")
	}

	status, err := host.Status(context.Background(), "tvbox-1")
	if err != nil {
		t.Fatal(err)
	}
	if status.State != "ready" || status.Status.Platform != PlatformAndroidTV {
		t.Fatalf("status=%#v", status)
	}
	capabilities, err := host.Capabilities(context.Background(), "tvbox-1")
	if err != nil || capabilities.State != "ready" {
		t.Fatalf("capabilities=%#v err=%v", capabilities, err)
	}
	prepared, err := host.Prepare(context.Background(), "tvbox-1", "default",
		[]byte("[Interface]\nPrivateKey = test\n\n[Peer]\nPublicKey = test\n"))
	if err != nil || prepared.State != "prepared" {
		t.Fatalf("prepare=%#v err=%v", prepared, err)
	}
	if string(connector.config) != "[Interface]\nPrivateKey = test\n\n[Peer]\nPublicKey = test\n" {
		t.Fatalf("device config=%q", connector.config)
	}
	started, err := host.Start(context.Background(), "tvbox-1")
	if err != nil || started.State != "authorization_required" {
		t.Fatalf("start=%#v err=%v", started, err)
	}
	stopped, err := host.Stop(context.Background(), "tvbox-1")
	if err != nil || stopped.State != "stopped" {
		t.Fatalf("stop=%#v err=%v", stopped, err)
	}

	closed, err := host.CloseSession(context.Background(), "tvbox-1")
	if err != nil {
		t.Fatal(err)
	}
	if !closed.Closed {
		t.Fatalf("close=%#v", closed)
	}
	if _, err := host.Sessions.Load("tvbox-1", now); !errors.Is(err, ErrSessionMissing) {
		t.Fatalf("session survived close: %v", err)
	}
	if len(connector.method) != 7 || connector.method[6] != MethodClose {
		t.Fatalf("methods=%v", connector.method)
	}
}

func TestHostPairRejectsInvalidDeviceProof(t *testing.T) {
	runner := &rpcTestRunner{}
	connector := &rpcTestConnector{runner: runner, badProof: true}
	host := NewHost(runner)
	host.Connector = connector
	host.Sessions = SessionStore{Path: filepath.Join(t.TempDir(), "device-session.json")}
	if _, err := host.Pair(context.Background(), "tvbox-1"); !errors.Is(err, ErrPairingProof) {
		t.Fatalf("invalid proof=%v", err)
	}
	if _, err := host.Sessions.Load("tvbox-1", time.Now()); !errors.Is(err, ErrSessionMissing) {
		t.Fatalf("invalid proof stored session: %v", err)
	}
}
