package devicebridge

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"math/big"
	"net"
	"strings"
	"testing"
	"time"
)

type rpcPipeRunner struct {
	port      string
	forwarded string
	removed   string
}

func (r *rpcPipeRunner) Run(_ context.Context, _ string, args ...string) ([]byte, error) {
	for index := 0; index < len(args); index++ {
		if args[index] == "tcp:0" {
			return []byte(r.port + "\n"), nil
		}
		if args[index] == "forward" && index+2 < len(args) && args[index+1] == "tcp:0" {
			r.forwarded = args[index+2]
		}
		if args[index] == "--remove" && index+1 < len(args) {
			r.removed = args[index+1]
		}
	}
	return []byte("ok\n"), nil
}

func TestADBRPCConnectorAuthenticatesAndRemovesForward(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	runner := &rpcPipeRunner{port: "12345"}
	certificate, certificateHash := testTLSCertificate(t)

	serverDone := make(chan error, 1)
	go func() {
		defer serverConn.Close()
		server := tls.Server(serverConn, &tls.Config{Certificates: []tls.Certificate{certificate}})
		if err := server.Handshake(); err != nil {
			serverDone <- err
			return
		}
		reader := bufio.NewReader(server)
		challenge, err := reader.ReadString('\n')
		challenge = strings.TrimSpace(challenge)
		if err != nil || challenge == "" {
			serverDone <- ErrInvalidRequest
			return
		}
		_, err = server.Write([]byte(
			transportProof("transport-token", "server", challenge, certificateHash) + "\n",
		))
		if err != nil {
			serverDone <- err
			return
		}
		clientProof, err := reader.ReadString('\n')
		if err != nil || strings.TrimSpace(clientProof) !=
			transportProof("transport-token", "client", challenge, certificateHash) {
			serverDone <- ErrPairingProof
			return
		}
		raw, err := reader.ReadBytes('\n')
		if err != nil {
			serverDone <- err
			return
		}
		var request Request
		if err := json.Unmarshal(raw, &request); err != nil || request.Method != MethodStatus {
			serverDone <- ErrInvalidRequest
			return
		}
		response := Response{SchemaVersion: SchemaVersion, RequestID: request.RequestID, OK: true, State: "ready"}
		encoded, _ := json.Marshal(response)
		_, err = server.Write(append(encoded, '\n'))
		serverDone <- err
	}()

	connector := &ADBRPCConnector{
		Runner: runner,
		ADB:    "adb",
		Dial: func(context.Context, string, string) (net.Conn, error) {
			return clientConn, nil
		},
	}
	connection, err := connector.Connect(
		context.Background(), "tvbox-1", "transport-token",
	)
	if err != nil {
		t.Fatal(err)
	}
	response, err := connection.Call(context.Background(), MethodStatus, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !response.OK || response.State != "ready" {
		t.Fatalf("response=%#v", response)
	}
	if err := <-serverDone; err != nil {
		t.Fatal(err)
	}
	if err := connection.Close(); err != nil {
		t.Fatal(err)
	}
	if runner.removed != "tcp:"+runner.port {
		t.Fatalf("removed=%q", runner.removed)
	}
	if runner.forwarded != "tcp:"+companionPort {
		t.Fatalf("forwarded=%q", runner.forwarded)
	}
}

func testTLSCertificate(t *testing.T) (tls.Certificate, string) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	template := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "archielink-rpc"},
		NotBefore:    time.Now().Add(-time.Minute),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, &template, &template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}, sha256Hex(der)
}
