package remote

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"strings"
	"testing"
	"time"
)

// fakeProvider 返回固定 JSON 的最小 Provider 实现。
type fakeProvider struct {
	failDoctor bool
}

func (f *fakeProvider) Hello(context.Context) (json.RawMessage, error) {
	return json.RawMessage(`{"app":"archielink","schema_version":"1"}`), nil
}

func (f *fakeProvider) Capabilities(context.Context) (json.RawMessage, error) {
	return json.RawMessage(`{"network_mutation":false}`), nil
}

func (f *fakeProvider) Detect(context.Context) (json.RawMessage, error) {
	return json.RawMessage(`{"mode":"guided"}`), nil
}

func (f *fakeProvider) Plan(context.Context) (json.RawMessage, error) {
	return json.RawMessage(`{"actions":[]}`), nil
}

func (f *fakeProvider) Doctor(context.Context) (json.RawMessage, error) {
	if f.failDoctor {
		return nil, errors.New("boom")
	}
	return json.RawMessage(`{"findings":[]}`), nil
}

// startServer 启动一个 serve goroutine 并等待就绪，返回 server、信息与
// 结果通道。
func startServer(t *testing.T, provider Provider, opts ServerOptions) (*Server, ServeInfo, chan error) {
	t.Helper()
	server, err := Start(provider, opts)
	if err != nil {
		t.Fatal(err)
	}
	info := server.Info()
	done := make(chan error, 1)
	go func() { done <- server.Run(context.Background()) }()
	return server, info, done
}

func TestServeConnectFullSession(t *testing.T) {
	confirmedWith := ""
	_, info, done := startServer(t, &fakeProvider{}, ServerOptions{
		Confirm: func(req ConfirmRequest) bool {
			confirmedWith = req.RemoteAddr
			return true
		},
		SessionTTL: time.Minute,
	})
	if info.PairingCode == "" || len(info.PairingCode) != 8 {
		t.Fatalf("8-digit pairing code expected: %q", info.PairingCode)
	}
	if len(info.Fingerprint) != 64 {
		t.Fatalf("sha-256 fingerprint expected: %q", info.Fingerprint)
	}
	client, err := Connect(context.Background(), info.Endpoint, info.PairingCode, ClientOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(confirmedWith, ":") {
		t.Fatalf("confirm callback expected remote addr, got %q", confirmedWith)
	}
	for _, method := range []string{MethodHello, MethodCapabilities, MethodDetect, MethodPlan, MethodDoctor} {
		result, err := client.Call(method)
		if err != nil {
			t.Fatalf("%s: %v", method, err)
		}
		if len(result) == 0 {
			t.Fatalf("%s: empty result", method)
		}
	}
	if err := client.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("serve must end cleanly after close: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("serve did not finish after close")
	}
}

func TestConnectRejectsWrongCode(t *testing.T) {
	_, info, done := startServer(t, &fakeProvider{}, ServerOptions{
		Confirm: func(ConfirmRequest) bool { return true },
	})
	_, err := Connect(context.Background(), info.Endpoint, "00000000", ClientOptions{})
	if err == nil || !errors.Is(err, errors.New(CodePairingInvalid)) && err.Error() != CodePairingInvalid {
		t.Fatalf("pairing_invalid expected: %v", err)
	}
	select {
	case err := <-done:
		if err == nil || err.Error() != CodePairingInvalid {
			t.Fatalf("serve must fail closed on wrong code: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("serve did not fail closed")
	}
}

func TestConnectRejectsWhenUserDenies(t *testing.T) {
	_, info, done := startServer(t, &fakeProvider{}, ServerOptions{
		Confirm: func(ConfirmRequest) bool { return false },
	})
	_, err := Connect(context.Background(), info.Endpoint, info.PairingCode, ClientOptions{})
	if err == nil || err.Error() != CodePairingRejected {
		t.Fatalf("pairing_rejected expected: %v", err)
	}
	select {
	case err := <-done:
		if err == nil || err.Error() != CodePairingRejected {
			t.Fatalf("serve must fail closed on rejection: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("serve did not fail closed")
	}
}

func TestConnectFailsClosedWithoutConfirmHook(t *testing.T) {
	// Confirm 为 nil 必须拒绝（fail closed），不能默认放行。
	_, info, done := startServer(t, &fakeProvider{}, ServerOptions{})
	_, err := Connect(context.Background(), info.Endpoint, info.PairingCode, ClientOptions{})
	if err == nil || err.Error() != CodePairingRejected {
		t.Fatalf("pairing_rejected expected: %v", err)
	}
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("serve did not fail closed")
	}
}

func TestPairingCodeSingleUse(t *testing.T) {
	pairing := NewPairingCode(time.Minute)
	code := pairing.Code()
	if _, err := pairing.Verify(code); err != nil {
		t.Fatal(err)
	}
	pairing.Commit()
	if _, err := pairing.Verify(code); err == nil || err.Error() != CodePairingUsed {
		t.Fatalf("pairing_used expected after commit: %v", err)
	}
}

func TestPairingCodeExpiry(t *testing.T) {
	pairing := NewPairingCode(50 * time.Millisecond)
	time.Sleep(80 * time.Millisecond)
	if _, err := pairing.Verify(pairing.Code()); err == nil || err.Error() != CodePairingExpired {
		t.Fatalf("pairing_expired expected: %v", err)
	}
}

func TestPairingCodeRedaction(t *testing.T) {
	pairing := NewPairingCode(time.Minute)
	redacted := pairing.Redacted()
	if redacted == pairing.Code() {
		t.Fatal("redacted form must not equal full code")
	}
	if len(redacted) != 8 || !strings.Contains(redacted, "****") {
		t.Fatalf("redacted shape unexpected: %q", redacted)
	}
}

func TestClientRejectsNonWhitelistMethod(t *testing.T) {
	client := &Client{}
	for _, method := range []string{"shell", "exec", "read_file", "write_file", "install", "setup.apply"} {
		if _, err := client.Call(method); err == nil || err.Error() != CodeMethodNotAllowed {
			t.Fatalf("%s must be rejected client-side: %v", method, err)
		}
	}
}

func TestServerRejectsNonWhitelistMethod(t *testing.T) {
	server, info, done := startServer(t, &fakeProvider{}, ServerOptions{
		Confirm: func(ConfirmRequest) bool { return true },
	})
	client, err := Connect(context.Background(), info.Endpoint, info.PairingCode, ClientOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	// 绕过客户端白名单，直接向会话写入非白名单方法。
	client.nextID++
	if err := client.encoder.Encode(Request{Method: "shell", ID: 99}); err != nil {
		t.Fatal(err)
	}
	var resp Response
	if err := client.decoder.Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if resp.Error == nil || resp.Error.Code != CodeMethodNotAllowed {
		t.Fatalf("server must reject non-whitelist method: %+v", resp)
	}
	// 会话仍然可用：白名单方法正常返回。
	result, err := client.Call(MethodHello)
	if err != nil || len(result) == 0 {
		t.Fatalf("session must survive rejected method: %v", err)
	}
	// 关闭 serve 资源，防止泄漏。
	client.Close()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
	}
	_ = server
}

func TestConnectFingerprintMismatch(t *testing.T) {
	_, info, done := startServer(t, &fakeProvider{}, ServerOptions{
		Confirm: func(ConfirmRequest) bool { return true },
	})
	_, err := Connect(context.Background(), info.Endpoint, info.PairingCode, ClientOptions{
		Fingerprint: strings.Repeat("0", 64),
	})
	if err == nil || err.Error() != CodeFingerprintMismatch {
		t.Fatalf("fingerprint_mismatch expected: %v", err)
	}
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("serve did not finish")
	}
}

func TestConnectAcceptsFingerprintMatch(t *testing.T) {
	_, info, done := startServer(t, &fakeProvider{}, ServerOptions{
		Confirm: func(ConfirmRequest) bool { return true },
	})
	client, err := Connect(context.Background(), info.Endpoint, info.PairingCode, ClientOptions{
		Fingerprint: info.Fingerprint,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Call(MethodDoctor); err != nil {
		t.Fatal(err)
	}
	client.Close()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
	}
}

func TestClientFailsFastWhenServerDies(t *testing.T) {
	// 用 ctx 取消模拟 helper 崩溃：会话连接被立即关闭，
	// 控制端后续 Call 必须限时失败而不是无限挂起。
	ctx, cancel := context.WithCancel(context.Background())
	server, err := Start(&fakeProvider{}, ServerOptions{
		Confirm: func(ConfirmRequest) bool { return true },
	})
	if err != nil {
		t.Fatal(err)
	}
	info := server.Info()
	done := make(chan error, 1)
	go func() { done <- server.Run(ctx) }()
	client, err := Connect(context.Background(), info.Endpoint, info.PairingCode, ClientOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	cancel()
	start := time.Now()
	_, err = client.Call(MethodHello)
	if err == nil {
		t.Fatal("client call must fail after server death")
	}
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Fatalf("failure took too long: %v", elapsed)
	}
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("serve did not finish after cancel")
	}
}

func TestServerProviderErrorReturnsInternalCode(t *testing.T) {
	_, info, _ := startServer(t, &fakeProvider{failDoctor: true}, ServerOptions{
		Confirm: func(ConfirmRequest) bool { return true },
	})
	client, err := Connect(context.Background(), info.Endpoint, info.PairingCode, ClientOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	_, err = client.Call(MethodDoctor)
	if err == nil || err.Error() != CodeInternal {
		t.Fatalf("internal_error expected: %v", err)
	}
}

func TestStartRequiresProvider(t *testing.T) {
	if _, err := Start(nil, ServerOptions{}); err == nil {
		t.Fatal("provider is required")
	}
}

func TestServerEndpointIsReachable(t *testing.T) {
	_, info, done := startServer(t, &fakeProvider{}, ServerOptions{
		Confirm: func(ConfirmRequest) bool { return true },
	})
	conn, err := net.DialTimeout("tcp", info.Endpoint, time.Second)
	if err != nil {
		t.Fatalf("endpoint must be reachable: %v", err)
	}
	conn.Close()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
	}
}

// 验收发现（deskdesk-windows 多网卡）：链路本地地址（169.254.0.0/16
// APIPA）不得进入端点提示或证书 SAN，否则 serve 提示不可达端点。
func TestFilterLANIPv4SkipsLinkLocal(t *testing.T) {
	addrs := []net.Addr{
		&net.IPNet{IP: net.ParseIP("169.254.66.229"), Mask: net.CIDRMask(16, 32)},
		&net.IPNet{IP: net.ParseIP("192.168.1.131"), Mask: net.CIDRMask(24, 32)},
		&net.IPNet{IP: net.ParseIP("127.0.0.1"), Mask: net.CIDRMask(8, 32)},
		&net.IPNet{IP: net.ParseIP("fe80::1"), Mask: net.CIDRMask(64, 128)},
	}
	ips := filterLANIPv4(addrs)
	if len(ips) != 1 || ips[0].String() != "192.168.1.131" {
		t.Fatalf("only the routable IPv4 must remain: %v", ips)
	}
}
