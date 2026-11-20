package remote

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"
	"time"
)

// ErrPairingClosed 表示配对码已消耗且会话已建立，后续配对一律拒绝。
var ErrPairingClosed = errors.New("remote: pairing code consumed")

// PairingState 是一次性配对码的生命周期状态机。
// 状态迁移：issued -> confirmed（会话建立，码作废）。
// 过期、重复使用、三次错误尝试均失败关闭。
type PairingState struct {
	mu        sync.Mutex
	code      string
	expiresAt time.Time
	confirmed bool
	failures  int
}

const maxPairingFailures = 3

// NewPairingCode 生成 8 位数字一次性配对码，有效期 ttl。
func NewPairingCode(ttl time.Duration) *PairingState {
	buf := make([]byte, 4)
	if _, err := rand.Read(buf); err != nil {
		panic("remote: crypto/rand unavailable: " + err.Error())
	}
	value := uint32(buf[0])<<24 | uint32(buf[1])<<16 | uint32(buf[2])<<8 | uint32(buf[3])
	code := fmt.Sprintf("%08d", value%100000000)
	return &PairingState{code: code, expiresAt: time.Now().Add(ttl)}
}

// Code 返回完整配对码（仅 CLI 展示与客户端输入使用，禁止入日志）。
func (p *PairingState) Code() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.code
}

// Redacted 返回脱敏形式（首尾各 2 位），用于日志。
func (p *PairingState) Redacted() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return redactCode(p.code)
}

// ExpiresAt 返回过期时间。
func (p *PairingState) ExpiresAt() time.Time {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.expiresAt
}

func redactCode(code string) string {
	if len(code) < 5 {
		return "****"
	}
	return code[:2] + "****" + code[len(code)-2:]
}

// Verify 校验配对码但不消耗：过期、已用或尝试超限返回对应错误码。
// 三次错误尝试后配对失败关闭。完全验证成功（含 HMAC 与用户确认）后
// 必须调用 Commit 才建立会话。
func (p *PairingState) Verify(attempt string) (string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.confirmed {
		return "", NewStatusError(CodePairingUsed, "")
	}
	if time.Now().After(p.expiresAt) {
		return "", NewStatusError(CodePairingExpired, "")
	}
	if !hmac.Equal([]byte(attempt), []byte(p.code)) {
		p.failures++
		return "", NewStatusError(CodePairingInvalid, "")
	}
	return p.code, nil
}

// Commit 将配对码标记为已消耗：一次配对只能建立一个会话。
func (p *PairingState) Commit() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.confirmed = true
}

// Consumed 报告配对码是否已消耗。
func (p *PairingState) Consumed() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.confirmed
}

// Expired 报告配对码是否已过期（serve 等待循环用）。
func (p *PairingState) Expired() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return time.Now().After(p.expiresAt)
}

// pairingKey 从配对码派生 HMAC 密钥。配对码即共享秘密，短时一次性。
func pairingKey(code string) []byte {
	sum := sha256.Sum256([]byte("archielink-remote-pairing-v1:" + code))
	return sum[:]
}

// signPairing 对 nonce 计算 HMAC-SHA256（十六进制）。
func signPairing(code, nonce string) string {
	mac := hmac.New(sha256.New, pairingKey(code))
	mac.Write([]byte(nonce))
	return hex.EncodeToString(mac.Sum(nil))
}

// verifyPairing 校验配对握手的 HMAC。
func verifyPairing(code, nonce, mac string) bool {
	expected := signPairing(code, nonce)
	return hmac.Equal([]byte(expected), []byte(mac))
}
