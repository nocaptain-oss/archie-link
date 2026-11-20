package remote

import (
	"encoding/json"
	"errors"
)

// RPC 方法白名单。远端 helper 不接受白名单之外的任何方法名。
const (
	MethodPair        = "pair"
	MethodHello       = "hello"
	MethodCapabilities = "capabilities"
	MethodDetect      = "detect"
	MethodPlan        = "plan"
	MethodDoctor      = "doctor"
	MethodClose       = "close"
)

// 会话层稳定错误码（RPC error.code 与配对失败原因共用）。
const (
	CodePairingExpired        = "pairing_expired"
	CodePairingRejected       = "pairing_rejected"
	CodePairingInvalid        = "pairing_invalid"
	CodePairingUsed           = "pairing_used"
	CodeFingerprintMismatch   = "fingerprint_mismatch"
	CodeMethodNotAllowed      = "method_not_allowed"
	CodeSessionClosed         = "session_closed"
	CodeConfirmTimeout        = "confirm_timeout"
	CodeConnectFailed         = "connect_failed"
	CodeProtocolError         = "protocol_error"
	CodeInternal              = "internal_error"
)

// StatusError 携带稳定错误码与可读细节。CLI 层取 Code 展示，
// Detail 仅进 message，不混入稳定码。
type StatusError struct {
	Code   string
	Detail string
}

func (e *StatusError) Error() string {
	if e.Detail == "" {
		return e.Code
	}
	return e.Code + ": " + e.Detail
}

// NewStatusError 构造稳定码错误。
func NewStatusError(code, detail string) error {
	return &StatusError{Code: code, Detail: detail}
}

// ErrorCodeFrom 提取错误的稳定码；非 StatusError 时回退原文本。
func ErrorCodeFrom(err error) string {
	if err == nil {
		return ""
	}
	var status *StatusError
	if errors.As(err, &status) {
		return status.Code
	}
	return err.Error()
}

// Request 是单请求单响应协议的请求帧。
type Request struct {
	Method string          `json:"method"`
	ID     int64           `json:"id"`
	Params json.RawMessage `json:"params,omitempty"`
}

// Response 是单请求单响应协议的响应帧。
type Response struct {
	ID     int64           `json:"id"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *RPCError       `json:"error,omitempty"`
}

// RPCError 携带稳定错误码，不携带自由文本堆栈。
type RPCError struct {
	Code string `json:"code"`
}

func (e *RPCError) Error() string { return "remote rpc error: " + e.Code }

// NewResponse 构造成功响应。
func NewResponse(id int64, result json.RawMessage) Response {
	return Response{ID: id, Result: result}
}

// NewErrorResponse 构造错误响应。
func NewErrorResponse(id int64, code string) Response {
	return Response{ID: id, Error: &RPCError{Code: code}}
}

// PairRequest 是配对握手帧：配对码 + 客户端随机数（HMAC 防重放）。
type PairRequest struct {
	Code  string `json:"code"`
	Nonce string `json:"nonce"`
	MAC   string `json:"mac"`
}

// PairResponse 是配对握手的结果帧。
type PairResponse struct {
	OK    bool      `json:"ok"`
	Error *RPCError `json:"error,omitempty"`
}

// isReadonlyRPC 判断方法是否属于已配对会话允许的白名单。
func isReadonlyRPC(method string) bool {
	switch method {
	case MethodHello, MethodCapabilities, MethodDetect, MethodPlan, MethodDoctor, MethodClose:
		return true
	default:
		return false
	}
}
