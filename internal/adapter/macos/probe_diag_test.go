package macos

import (
	"bufio"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"golang.zx2c4.com/wireguard/conn"
	"golang.zx2c4.com/wireguard/device"
	"golang.zx2c4.com/wireguard/tun"
)

// TestDiagProbeAllCandidates is a manual diagnostic (ARCHIE_LINK_DIAG=1):
// it runs a userspace wireguard-go handshake against every candidate endpoint.
// It creates a temporary utun with no routes and no DNS changes; only the
// handshake UDP packets leave the machine. No privileges are required.
func TestDiagProbeAllCandidates(t *testing.T) {
	if os.Getenv("ARCHIE_LINK_DIAG") == "" {
		t.Skip("diagnostic only: set ARCHIE_LINK_DIAG=1")
	}
	configPath := os.Getenv("ARCHIE_LINK_WG_CONFIG")
	if configPath == "" {
		t.Fatal("ARCHIE_LINK_WG_CONFIG is required")
	}
	privateKey, publicKey := parseProbeKeys(t, configPath)
	if privateKey == "" || publicKey == "" {
		t.Fatal("could not parse PrivateKey/PublicKey from config")
	}
	privateKey = base64ToHex(t, privateKey)
	publicKey = base64ToHex(t, publicKey)

	type candidate struct {
		address string
		port    int
	}
	candidates := []candidate{
		{"188.114.97.1", 500},
		{"188.114.96.1", 500},
		{"188.114.98.1", 500},
		{"162.159.192.1", 2408},
		{"162.159.193.1", 2408},
		{"162.159.192.1", 500},
		{"162.159.193.1", 500},
		{"188.114.97.1", 2408},
		{"162.159.192.1", 4500},
		{"162.159.192.1", 1701},
	}
	anyOK := false
	for _, c := range candidates {
		endpoint := c.address + ":" + strconv.Itoa(c.port)
		start := time.Now()
		handshake := probeEndpointHandshake(t, privateKey, publicKey, endpoint, 5*time.Second)
		elapsed := time.Since(start).Truncate(time.Millisecond)
		status := "FAIL"
		if handshake {
			status = "HANDSHAKE"
			anyOK = true
		}
		t.Logf("%s -> %s elapsed=%v", endpoint, status, elapsed)
	}
	if !anyOK {
		t.Log("no candidate completed a handshake")
	}
}

// probeEndpointHandshake brings up a throwaway in-memory WireGuard device
// (dummy TUN, no kernel interface, no routes, no DNS) and reports whether a
// handshake with the endpoint is observed within timeout. Only the handshake
// UDP packets leave the machine.
func probeEndpointHandshake(t *testing.T, privateKey, publicKey, endpoint string, timeout time.Duration) bool {
	t.Helper()
	dummy := newDummyTUN()
	defer dummy.Close()
	logger := device.NewLogger(device.LogLevelError, "archiediag")
	dev := device.NewDevice(dummy, conn.NewDefaultBind(), logger)
	defer dev.Close()
	conf := "private_key=" + privateKey + "\n" +
		"replace_peers=true\n" +
		"public_key=" + publicKey + "\n" +
		"endpoint=" + endpoint + "\n" +
		"persistent_keepalive_interval=25\n" +
		"replace_allowed_ips=true\n" +
		"allowed_ip=0.0.0.0/0\n"
	if err := dev.IpcSet(conf); err != nil {
		t.Logf("ipc set %s: %v", endpoint, err)
		return false
	}
	if err := dev.Up(); err != nil {
		t.Logf("device up %s: %v", endpoint, err)
		return false
	}
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		state, err := dev.IpcGet()
		if err != nil {
			t.Logf("ipc get %s: %v", endpoint, err)
			return false
		}
		for _, line := range strings.Split(state, "\n") {
			if strings.HasPrefix(line, "last_handshake_time_nsec=") {
				value := strings.TrimPrefix(line, "last_handshake_time_nsec=")
				if parsed, err := strconv.ParseInt(value, 10, 64); err == nil && parsed > 0 {
					return true
				}
			}
		}
		time.Sleep(200 * time.Millisecond)
	}
	return false
}

func parseProbeKeys(t *testing.T, configPath string) (string, string) {
	t.Helper()
	raw, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	privateKey := ""
	publicKey := ""
	section := ""
	scanner := bufio.NewScanner(strings.NewReader(string(raw)))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			section = strings.Trim(line, "[]")
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.ToLower(strings.TrimSpace(key))
		value = strings.TrimSpace(value)
		if section == "Interface" && key == "privatekey" {
			privateKey = value
		}
		if section == "Peer" && key == "publickey" {
			publicKey = value
		}
	}
	return privateKey, publicKey
}

// base64ToHex converts a standard base64 WireGuard key into the hex form the
// wireguard-go UAPI expects.
func base64ToHex(t *testing.T, value string) string {
	t.Helper()
	raw, err := base64.StdEncoding.DecodeString(value)
	if err != nil {
		t.Fatalf("decode key: %v", err)
	}
	return hex.EncodeToString(raw)
}

// dummyTUN is an in-memory tun.Device: reads block forever, writes are
// discarded, and no kernel network object is ever created.
type dummyTUN struct {
	events chan tun.Event
	closed chan struct{}
}

func newDummyTUN() *dummyTUN {
	events := make(chan tun.Event, 1)
	events <- tun.EventUp
	return &dummyTUN{events: events, closed: make(chan struct{})}
}

func (d *dummyTUN) File() *os.File                   { return nil }
func (d *dummyTUN) MTU() (int, error)                { return 1280, nil }
func (d *dummyTUN) Name() (string, error)            { return "archiediag0", nil }
func (d *dummyTUN) BatchSize() int                   { return 1 }
func (d *dummyTUN) Events() <-chan tun.Event         { return d.events }
func (d *dummyTUN) Write([][]byte, int) (int, error) { return 1, nil }

func (d *dummyTUN) Read([][]byte, []int, int) (int, error) {
	<-d.closed
	return 0, errors.New("device closed")
}

func (d *dummyTUN) Close() error {
	select {
	case <-d.closed:
	default:
		close(d.closed)
	}
	return nil
}
