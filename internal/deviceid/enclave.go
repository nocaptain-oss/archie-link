package deviceid

import "context"

// EnclaveBackend 生成并使用不可导出的设备私钥（macOS Secure Enclave）。
// 实现方必须保证私钥字节永远不经过本接口的调用方；签名在安全硬件内完成。
type EnclaveBackend interface {
	// EnsureKey 返回该 installID 对应的安全硬件身份；不存在时创建。
	// 返回的 Identity 必须带 SecureEnclave=true 与 KeyFormat="se-p256"。
	EnsureKey(ctx context.Context, installID string) (Identity, error)
	// Sign 用安全硬件私钥对 challenge 签名，输出与 crypto/ecdsa SignASN1
	// 兼容的 ASN.1 DER（ECDSA over SHA-256）。
	Sign(ctx context.Context, installID string, challenge []byte) ([]byte, error)
}
