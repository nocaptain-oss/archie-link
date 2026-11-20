package export

import (
	"strings"
	"testing"
)

func TestWireGuardMasksPrivateKeyByDefault(t *testing.T) {
	input := []byte("[Interface]\nPrivateKey = secret\nAddress = 172.16.0.2/32\n\n[Peer]\nPublicKey = public\nPresharedKey = shared-secret\n")
	output, err := WireGuard(input, false)
	if err != nil {
		t.Fatal(err)
	}
	text := string(output)
	if strings.Contains(text, "secret") || strings.Contains(text, "shared-secret") {
		t.Fatal("private key material leaked")
	}
	if !strings.Contains(text, "PrivateKey = <redacted>") ||
		!strings.Contains(text, "PresharedKey = <redacted>") {
		t.Fatalf("output=%s", output)
	}
	if !strings.Contains(text, "PublicKey = public") {
		t.Fatal("public key should remain visible")
	}
}

func TestWireGuardCanExplicitlyIncludePrivateKey(t *testing.T) {
	input := []byte("[Interface]\nPrivateKey = secret\n\n[Peer]\nPublicKey = public\n")
	output, err := WireGuard(input, true)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(output), "PrivateKey = secret") {
		t.Fatalf("output=%s", output)
	}
}

func TestWireGuardRejectsIncompleteConfig(t *testing.T) {
	if _, err := WireGuard([]byte("PrivateKey = secret\n"), false); err == nil {
		t.Fatal("expected incomplete config error")
	}
}
