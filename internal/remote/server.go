package remote

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"log"
	"math/big"
	"net"
	"time"
)

// Provider 是白名单 RPC 的目标端实现。remote 包只依赖该接口，
// 报告构建由装配层注入，保证远程 JSON schema 与本地一致。
type Provider interface {
	Hello(ctx context.Context) (json.RawMessage, error)
	Capabilities(ctx context.Context) (json.RawMessage, error)
	Detect(ctx context.Context) (json.RawMessage, error)
	Plan(ctx context.Context) (json.RawMessage, error)
	Doctor(ctx context.Context) (json.RawMessage, error)
}

// ConfirmRequest 描述一次待确认的配对请求，Target 端展示给用户。
type ConfirmRequest struct {
	RemoteAddr string
	At         time.Time
}

// ServerOptions 配置 serve 行为。
type ServerOptions struct {
	// Addr 监听地址，默认 "0.0.0.0:0"（随机端口）。
	Addr string
	// SessionTTL 会话整体窗口（证书有效期、配对码有效期、serve 超时），
	// 默认 10 分钟。
	SessionTTL time.Duration
	// Confirm 是 Target 端用户确认回调；nil 视为拒绝（fail closed）。
	Confirm func(ConfirmRequest) bool
	// ConfirmTimeout 等待用户确认的时限，默认 30 秒。
	ConfirmTimeout time.Duration
	// Logger 接收脱敏日志（不含完整配对码、私钥材料）。
	Logger *log.Logger
}

// ServeInfo 是 serve 启动后供 CLI 展示的一次性会话材料。
type ServeInfo struct {
	Endpoint    string
	Fingerprint string
	PairingCode string
	ExpiresAt   time.Time
	LANIPs      []net.IP
}

// Server 是 Target 端一次性只读会话服务器。
type Server struct {
	info     ServeInfo
	listener net.Listener
	pairing  *PairingState
	provider Provider
	opts     ServerOptions
	cert     tls.Certificate
}

const (
	defaultSessionTTL    = 10 * time.Minute
	defaultConfirmWait   = 30 * time.Second
	maxHandshakeBytes    = 1 << 16
	serialNumberBits     = 128
)

// Start 生成临时证书与一次性配对码并开始监听，不阻塞。
// 调用方先用 Info() 展示配对材料，再运行 Run 等待会话结束。
func Start(provider Provider, opts ServerOptions) (*Server, error) {
	if provider == nil {
		return nil, errors.New("remote: provider is required")
	}
	if opts.SessionTTL <= 0 {
		opts.SessionTTL = defaultSessionTTL
	}
	if opts.ConfirmTimeout <= 0 {
		opts.ConfirmTimeout = defaultConfirmWait
	}
	if opts.Addr == "" {
		opts.Addr = "0.0.0.0:0"
	}
	certPEM, fingerprint, err := generateSelfSignedCert(opts.SessionTTL)
	if err != nil {
		return nil, fmt.Errorf("remote: generate session certificate: %w", err)
	}
	pairing := NewPairingCode(opts.SessionTTL)
	listener, err := net.Listen("tcp", opts.Addr)
	if err != nil {
		return nil, fmt.Errorf("remote: listen %s: %w", opts.Addr, err)
	}
	lanIPs := lanIPv4s()
	port := listener.Addr().(*net.TCPAddr).Port
	return &Server{
		info: ServeInfo{
			Endpoint:    fmt.Sprintf("%s:%d", primaryLANIP(lanIPs), port),
			Fingerprint: fingerprint,
			PairingCode: pairing.Code(),
			ExpiresAt:   pairing.ExpiresAt(),
			LANIPs:      lanIPs,
		},
		listener: listener,
		pairing:  pairing,
		provider: provider,
		opts:     opts,
		cert:     certPEM,
	}, nil
}

// Info 返回一次性会话材料（端点、指纹、完整配对码、过期时间）。
func (s *Server) Info() ServeInfo { return s.info }

// Run 阻塞等待单个会话完成：配对 → 确认 → 白名单 RPC 循环 → 关闭。
// `--once` 语义：服务一个连接（无论配对成败）后即关闭监听退出——
// 错误配对码同样失败关闭，杜绝暴力尝试。返回 nil 表示会话正常结束
// （close 或对端断开）；ctx 取消、配对码过期、用户拒绝或内部错误
// 均关闭监听并返回对应错误。
func (s *Server) Run(ctx context.Context) error {
	defer s.listener.Close()
	deadline := time.NewTimer(s.opts.SessionTTL)
	defer deadline.Stop()
	type accepted struct{ conn net.Conn }
	connCh := make(chan accepted, 1)
	acceptErr := make(chan error, 1)
	go func() {
		conn, err := s.listener.Accept()
		if err != nil {
			acceptErr <- err
			return
		}
		connCh <- accepted{conn}
	}()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-deadline.C:
		return NewStatusError(CodePairingExpired, "")
	case err := <-acceptErr:
		return fmt.Errorf("remote: accept: %w", err)
	case a := <-connCh:
		return s.handlePairing(ctx, a.conn)
	}
}

// handlePairing 处理单次连接的配对握手与后续 RPC 会话。
func (s *Server) handlePairing(ctx context.Context, conn net.Conn) error {
	defer conn.Close()
	tlsConn := tls.Server(conn, &tls.Config{
		Certificates: []tls.Certificate{s.cert},
		MinVersion:   tls.VersionTLS13,
	})
	handshakeCtx, cancel := context.WithTimeout(ctx, s.opts.ConfirmTimeout)
	defer cancel()
	if err := tlsConn.HandshakeContext(handshakeCtx); err != nil {
		return fmt.Errorf("tls handshake: %w", err)
	}
	remoteAddr := conn.RemoteAddr().String()
	decoder := json.NewDecoder(io.LimitReader(tlsConn, maxHandshakeBytes))
	encoder := json.NewEncoder(tlsConn)
	var pairReq PairRequest
	if err := decoder.Decode(&pairReq); err != nil {
		s.reply(encoder, NewErrorResponse(0, CodeProtocolError))
		return fmt.Errorf("decode pair request: %w", err)
	}
	code, err := s.pairing.Verify(pairReq.Code)
	if err != nil {
		s.reply(encoder, NewErrorResponse(0, err.Error()))
		return err
	}
	if !verifyPairing(code, pairReq.Nonce, pairReq.MAC) {
		s.pairing.Commit() // 消耗配对码，防嗅探重试
		s.reply(encoder, NewErrorResponse(0, CodeProtocolError))
		return NewStatusError(CodeProtocolError, "")
	}
	if !s.confirm(remoteAddr) {
		s.pairing.Commit()
		s.reply(encoder, NewErrorResponse(0, CodePairingRejected))
		return NewStatusError(CodePairingRejected, "")
	}
	s.pairing.Commit()
	s.reply(encoder, PairResponse{OK: true})
	s.logf("session established from %s (code %s)", remoteAddr, s.pairing.Redacted())
	return s.serveSession(ctx, tlsConn, decoder, encoder)
}

// confirm 执行 Target 端用户确认，带超时。Confirm 为 nil 时拒绝。
func (s *Server) confirm(remoteAddr string) bool {
	if s.opts.Confirm == nil {
		return false
	}
	answer := make(chan bool, 1)
	go func() {
		answer <- s.opts.Confirm(ConfirmRequest{RemoteAddr: remoteAddr, At: time.Now()})
	}()
	timer := time.NewTimer(s.opts.ConfirmTimeout)
	defer timer.Stop()
	select {
	case ok := <-answer:
		return ok
	case <-timer.C:
		return false
	}
}

// serveSession 运行白名单 RPC 循环直到 close、对端断开或 ctx 取消。
func (s *Server) serveSession(ctx context.Context, conn net.Conn, decoder *json.Decoder, encoder *json.Encoder) error {
	sessionCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	go func() {
		<-sessionCtx.Done()
		conn.Close()
	}()
	for {
		var req Request
		if err := decoder.Decode(&req); err != nil {
			// 对端断开、连接被取消或任何解码异常都结束会话，
			// 由 Run 的 defer 关闭监听并完成清理。
			return nil
		}
		if req.Method == MethodClose {
			s.reply(encoder, NewResponse(req.ID, json.RawMessage(`{"closed":true}`)))
			return nil
		}
		if !isReadonlyRPC(req.Method) {
			s.reply(encoder, NewErrorResponse(req.ID, CodeMethodNotAllowed))
			continue
		}
		result, code := s.dispatch(sessionCtx, req.Method)
		if code == "" {
			s.reply(encoder, NewResponse(req.ID, result))
			continue
		}
		s.reply(encoder, NewErrorResponse(req.ID, code))
	}
}

// dispatch 将白名单方法映射到 Provider。
func (s *Server) dispatch(ctx context.Context, method string) (json.RawMessage, string) {
	var (
		result json.RawMessage
		err    error
	)
	switch method {
	case MethodHello:
		result, err = s.provider.Hello(ctx)
	case MethodCapabilities:
		result, err = s.provider.Capabilities(ctx)
	case MethodDetect:
		result, err = s.provider.Detect(ctx)
	case MethodPlan:
		result, err = s.provider.Plan(ctx)
	case MethodDoctor:
		result, err = s.provider.Doctor(ctx)
	default:
		return nil, CodeMethodNotAllowed
	}
	if err != nil {
		return nil, CodeInternal
	}
	return result, ""
}

func (s *Server) reply(encoder *json.Encoder, value any) {
	if err := encoder.Encode(value); err != nil {
		s.logf("write reply: %v", err)
	}
}

func (s *Server) logf(format string, args ...any) {
	if s.opts.Logger != nil {
		s.opts.Logger.Printf(format, args...)
	}
}

// generateSelfSignedCert 生成临时 Ed25519 自签证书，返回证书与
// DER SHA-256 指纹（十六进制）。证书仅在本会话窗口内有效。
func generateSelfSignedCert(ttl time.Duration) (tls.Certificate, string, error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return tls.Certificate{}, "", err
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), serialNumberBits))
	if err != nil {
		return tls.Certificate{}, "", err
	}
	template := x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: "archielink-remote-session"},
		NotBefore:             time.Now().Add(-time.Minute),
		NotAfter:              time.Now().Add(ttl),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
		BasicConstraintsValid: true,
	}
	for _, ip := range lanIPv4s() {
		template.IPAddresses = append(template.IPAddresses, ip)
	}
	template.IPAddresses = append(template.IPAddresses, net.ParseIP("127.0.0.1"))
	template.DNSNames = []string{"localhost"}
	der, err := x509.CreateCertificate(rand.Reader, &template, &template, pub, priv)
	if err != nil {
		return tls.Certificate{}, "", err
	}
	keyBytes, err := x509.MarshalPKCS8PrivateKey(priv)
	if err != nil {
		return tls.Certificate{}, "", err
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyBytes})
	pair, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return tls.Certificate{}, "", err
	}
	sum := sha256.Sum256(der)
	return pair, hex.EncodeToString(sum[:]), nil
}

// lanIPv4s 枚举本机可用于远程会话的 IPv4 地址。
// 链路本地地址（169.254.0.0/16，Windows 多网卡 APIPA）不可达，
// 不作为端点提示或证书 SAN 候选。
func lanIPv4s() []net.IP {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return nil
	}
	return filterLANIPv4(addrs)
}

// filterLANIPv4 过滤地址列表：保留非 loopback、非链路本地的 IPv4。
func filterLANIPv4(addrs []net.Addr) []net.IP {
	var ips []net.IP
	for _, addr := range addrs {
		ipNet, ok := addr.(*net.IPNet)
		if !ok || ipNet.IP.IsLoopback() {
			continue
		}
		ip4 := ipNet.IP.To4()
		if ip4 == nil || ip4.IsLinkLocalUnicast() {
			continue
		}
		ips = append(ips, ip4)
	}
	return ips
}

func primaryLANIP(ips []net.IP) string {
	if len(ips) == 0 {
		return "127.0.0.1"
	}
	return ips[0].String()
}
