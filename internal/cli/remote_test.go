package cli

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"strings"
	"testing"
	"time"
)

// startRemoteServe 在 goroutine 中启动 remote serve --yes --json，
// 返回启动信息读取管道与退出码通道。
func startRemoteServe(t *testing.T, deps Dependencies) (*bufio.Reader, *bytes.Buffer, chan int) {
	t.Helper()
	pr, pw := io.Pipe()
	stderr := &bytes.Buffer{}
	done := make(chan int, 1)
	go func() {
		done <- RunWithDeps([]string{"remote", "serve", "--yes", "--json"}, pw, stderr, deps)
		pw.Close()
	}()
	reader := bufio.NewReader(pr)
	return reader, stderr, done
}

type serveStartup struct {
	Endpoint    string `json:"endpoint"`
	PairingCode string `json:"pairing_code"`
	Fingerprint string `json:"fingerprint"`
}

func readServeStartup(t *testing.T, reader *bufio.Reader) serveStartup {
	t.Helper()
	line, err := reader.ReadString('\n')
	if err != nil {
		t.Fatalf("read serve startup: %v", err)
	}
	var startup serveStartup
	if err := json.Unmarshal([]byte(strings.TrimSpace(line)), &startup); err != nil {
		t.Fatalf("parse serve startup %q: %v", line, err)
	}
	return startup
}

func remoteTestDeps() Dependencies {
	return Dependencies{
		Registry: fakeRegistry{adapter: fakeAdapter{}},
		GOOS:     "linux",
	}
}

func TestRemoteServeConnectCheckup(t *testing.T) {
	deps := remoteTestDeps()
	reader, serveStderr, done := startRemoteServe(t, deps)
	startup := readServeStartup(t, reader)
	if startup.Endpoint == "" || len(startup.PairingCode) != 8 || len(startup.Fingerprint) != 64 {
		t.Fatalf("startup fields incomplete: %+v", startup)
	}
	go io.Copy(io.Discard, reader) // 排水 serve 后续 stdout 输出，避免管道写阻塞
	var out bytes.Buffer
	var errOut bytes.Buffer
	code := RunWithDeps([]string{"remote", "connect", startup.Endpoint, startup.PairingCode, "--json"}, &out, &errOut, deps)
	if code != 0 {
		t.Fatalf("connect rc=%d stderr=%s out=%s", code, errOut.String(), out.String())
	}
	var checkup struct {
		SchemaVersion string `json:"schema_version"`
		Paired        bool   `json:"paired"`
		Hello         struct {
			GOOS string `json:"goos"`
		} `json:"hello"`
		Detect struct {
			Mode string `json:"mode"`
		} `json:"detect"`
		Doctor struct {
			Findings []struct {
				Code string `json:"code"`
			} `json:"findings"`
		} `json:"doctor"`
	}
	if err := json.Unmarshal(out.Bytes(), &checkup); err != nil {
		t.Fatalf("checkup json: %v (%s)", err, out.String())
	}
	if !checkup.Paired {
		t.Fatal("paired must be true")
	}
	if checkup.Hello.GOOS != "darwin" || checkup.Detect.Mode != "auto" {
		t.Fatalf("remote target must reflect fake adapter (darwin/auto): %s", out.String())
	}
	if checkup.Doctor.Findings == nil {
		t.Fatal("doctor findings must be a non-null empty slice")
	}
	select {
	case serveRC := <-done:
		if serveRC != 0 {
			t.Fatalf("serve rc=%d stderr=%s", serveRC, serveStderr.String())
		}
	case <-time.After(5 * time.Second):
		t.Fatal("serve did not finish after close")
	}
	if strings.Contains(serveStderr.String(), startup.PairingCode) {
		t.Fatalf("full pairing code must not appear in logs: %s", serveStderr.String())
	}
}

func TestRemoteConnectWrongCodeFailsClosed(t *testing.T) {
	deps := remoteTestDeps()
	reader, serveStderr, done := startRemoteServe(t, deps)
	startup := readServeStartup(t, reader)
	go io.Copy(io.Discard, reader) // 排水 serve 错误 JSON 输出，避免管道写阻塞
	var out bytes.Buffer
	var errOut bytes.Buffer
	code := RunWithDeps([]string{"remote", "connect", startup.Endpoint, "00000000", "--json"}, &out, &errOut, deps)
	if code != 5 {
		t.Fatalf("connect rc must be 5: %d", code)
	}
	var payload struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(out.Bytes(), &payload); err != nil {
		t.Fatalf("error json: %v (%s)", err, out.String())
	}
	if payload.Error.Code != "pairing_invalid" {
		t.Fatalf("pairing_invalid expected: %s", out.String())
	}
	select {
	case serveRC := <-done:
		if serveRC != 5 {
			t.Fatalf("serve must fail closed with rc 5: %d stderr=%s", serveRC, serveStderr.String())
		}
	case <-time.After(5 * time.Second):
		t.Fatal("serve did not fail closed")
	}
}

func TestRemoteConnectRejectsNonWhitelistRPC(t *testing.T) {
	deps := remoteTestDeps()
	var out bytes.Buffer
	var errOut bytes.Buffer
	code := RunWithDeps([]string{"remote", "connect", "127.0.0.1:1", "12345678", "--rpc", "shell"}, &out, &errOut, deps)
	if code != 2 {
		t.Fatalf("non-whitelist rpc must be a usage error: %d", code)
	}
}

func TestRemoteUsageErrors(t *testing.T) {
	deps := remoteTestDeps()
	cases := [][]string{
		{"remote"},
		{"remote", "bogus"},
		{"remote", "connect"},
		{"remote", "connect", "only-endpoint"},
		{"remote", "serve", "--bogus"},
	}
	for _, args := range cases {
		var out, errOut bytes.Buffer
		if code := RunWithDeps(args, &out, &errOut, deps); code != 2 {
			t.Fatalf("%v must be usage error 2, got %d", args, code)
		}
	}
}
