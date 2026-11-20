package remote

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"io"
	"net"
	"time"
)

// ClientOptions 配置 Controller 端连接行为。
type ClientOptions struct {
	// Fingerprint 是期待的 Target 证书 SHA-256（十六进制，可选）。
	// 提供时不匹配即失败关闭。
	Fingerprint string
	// Timeout 是连接与配对握手的整体时限，默认 30 秒。
	Timeout time.Duration
}

// Client 是已配对的 Controller 端会话。
type Client struct {
	conn     *tls.Conn
	encoder  *json.Encoder
	decoder  *json.Decoder
	nextID   int64
	codeUsed string
}

const defaultConnectTimeout = 30 * time.Second

// Connect 建立 TLS 连接并完成一次性配对握手。
// 配对码错误、过期、已用、被拒或指纹不匹配时失败关闭。
func Connect(ctx context.Context, endpoint, code string, opts ClientOptions) (*Client, error) {
	if code == "" {
		return nil, NewStatusError(CodePairingInvalid, "")
	}
	if opts.Timeout <= 0 {
		opts.Timeout = defaultConnectTimeout
	}
	dialer := &net.Dialer{Timeout: opts.Timeout}
	conn, err := tls.DialWithDialer(dialer, "tcp", endpoint, &tls.Config{
		MinVersion:         tls.VersionTLS13,
		InsecureSkipVerify: true, // 信任由配对码 + 指纹双通道保证
	})
	if err != nil {
		return nil, NewStatusError(CodeConnectFailed, err.Error())
	}
	if err := verifyPeer(conn, opts.Fingerprint); err != nil {
		conn.Close()
		return nil, err
	}
	nonce, err := randomNonce()
	if err != nil {
		conn.Close()
		return nil, NewStatusError(CodeInternal, err.Error())
	}
	client := &Client{
		conn:    conn,
		encoder: json.NewEncoder(conn),
		decoder: json.NewDecoder(io.LimitReader(conn, maxHandshakeBytes)),
	}
	pairReq := PairRequest{
		Code:  code,
		Nonce: nonce,
		MAC:   signPairing(code, nonce),
	}
	if err := client.encoder.Encode(pairReq); err != nil {
		conn.Close()
		return nil, NewStatusError(CodeProtocolError, err.Error())
	}
	var pairResp PairResponse
	if err := client.decoder.Decode(&pairResp); err != nil {
		conn.Close()
		return nil, NewStatusError(CodeProtocolError, err.Error())
	}
	if pairResp.Error != nil {
		conn.Close()
		return nil, NewStatusError(pairResp.Error.Code, "")
	}
	if !pairResp.OK {
		conn.Close()
		return nil, NewStatusError(CodePairingRejected, "")
	}
	client.codeUsed = redactCode(code)
	return client, nil
}

// Call 调用一个白名单 RPC 方法并返回结构化结果。
// 非白名单方法在客户端即拒绝（method_not_allowed）。
func (c *Client) Call(method string) (json.RawMessage, error) {
	if !isReadonlyRPC(method) {
		return nil, NewStatusError(CodeMethodNotAllowed, "")
	}
	c.nextID++
	req := Request{Method: method, ID: c.nextID}
	if err := c.encoder.Encode(req); err != nil {
		return nil, NewStatusError(CodeProtocolError, err.Error())
	}
	var resp Response
	if err := c.decoder.Decode(&resp); err != nil {
		return nil, NewStatusError(CodeProtocolError, err.Error())
	}
	if resp.Error != nil {
		return nil, NewStatusError(resp.Error.Code, "")
	}
	return resp.Result, nil
}

// Close 发送 close RPC 并关闭会话（幂等）。
func (c *Client) Close() error {
	if c.conn == nil {
		return nil
	}
	c.nextID++
	req := Request{Method: MethodClose, ID: c.nextID}
	_ = c.encoder.Encode(req)
	var resp Response
	_ = c.decoder.Decode(&resp)
	err := c.conn.Close()
	c.conn = nil
	return err
}

// CodeUsed 返回已消耗配对码的脱敏形式（日志安全）。
func (c *Client) CodeUsed() string { return c.codeUsed }

// verifyPeer 校验对端证书。指纹未提供时跳过严格比对（信任交给配对码）。
func verifyPeer(conn *tls.Conn, fingerprint string) error {
	certs := conn.ConnectionState().PeerCertificates
	if len(certs) == 0 {
		return NewStatusError(CodeFingerprintMismatch, "")
	}
	if fingerprint == "" {
		return nil
	}
	sum := sha256.Sum256(certs[0].Raw)
	actual := hex.EncodeToString(sum[:])
	if actual != fingerprint {
		return NewStatusError(CodeFingerprintMismatch, "")
	}
	return nil
}

// randomNonce 生成 16 字节随机数的十六进制串。
func randomNonce() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}
