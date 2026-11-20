//go:build darwin && cgo

package deviceid

// #cgo CFLAGS: -mmacosx-version-min=12.0
// #cgo LDFLAGS: -mmacosx-version-min=12.0 -framework Security -framework CoreFoundation
// #include <Security/Security.h>
// #include <CoreFoundation/CoreFoundation.h>
import "C"

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"unsafe"
)

// SecureEnclaveStore 在 macOS Secure Enclave 中创建并使用不可导出的 P-256 私钥。
// 私钥通过确定性 ApplicationTag 绑定到 installID；公钥可导出为 PKIX DER。
// 私钥字节不经过本类型的任何调用方，也不进入 SecretStore。
type SecureEnclaveStore struct {
	// Service 用于派生 ApplicationTag，默认 "com.archielink.device"。
	Service string
}

const seTagPrefix = "com.archielink.device.se."

func (s SecureEnclaveStore) tag(installID string) []byte {
	service := s.Service
	if service == "" {
		service = "com.archielink.device"
	}
	return []byte(service + "." + seTagPrefix + installID)
}

func (s SecureEnclaveStore) EnsureKey(ctx context.Context, installID string) (Identity, error) {
	if installID == "" {
		return Identity{}, errors.New("install id is required for secure enclave keys")
	}
	key, err := seFindOrCreate(s.tag(installID))
	if err != nil {
		return Identity{}, err
	}
	defer C.CFRelease(C.CFTypeRef(key))

	publicDER, err := sePublicKeyDER(key)
	if err != nil {
		return Identity{}, err
	}
	digest := sha256.Sum256(publicDER)
	return Identity{
		InstallID:     installID,
		PublicKey:     base64.StdEncoding.EncodeToString(publicDER),
		KeyID:         hex.EncodeToString(digest[:8]),
		SecureEnclave: true,
		KeyFormat:     "se-p256",
	}, nil
}

func (s SecureEnclaveStore) Sign(ctx context.Context, installID string, challenge []byte) ([]byte, error) {
	key, err := seFindOrCreate(s.tag(installID))
	if err != nil {
		return nil, err
	}
	defer C.CFRelease(C.CFTypeRef(key))

	digest := sha256.Sum256(challenge)
	return seSign(key, digest[:])
}

func seFindOrCreate(tag []byte) (C.SecKeyRef, error) {
	key, found, err := seFindKey(tag)
	if err != nil {
		return C.SecKeyRef(0), err
	}
	if found {
		return key, nil
	}
	return seCreateKey(tag)
}

func seFindKey(tag []byte) (C.SecKeyRef, bool, error) {
	query := cfDictionary(map[C.CFTypeRef]C.CFTypeRef{
		C.CFTypeRef(C.kSecClass):              C.CFTypeRef(C.kSecClassKey),
		C.CFTypeRef(C.kSecAttrKeyType):        C.CFTypeRef(C.kSecAttrKeyTypeECSECPrimeRandom),
		C.CFTypeRef(C.kSecAttrKeySizeInBits):  cfNumber(256),
		C.CFTypeRef(C.kSecAttrTokenID):        C.CFTypeRef(C.kSecAttrTokenIDSecureEnclave),
		C.CFTypeRef(C.kSecAttrApplicationTag): cfData(tag),
		C.CFTypeRef(C.kSecReturnRef):          C.CFTypeRef(C.kCFBooleanTrue),
	})
	defer C.CFRelease(C.CFTypeRef(query))

	var result C.CFTypeRef
	status := C.SecItemCopyMatching(query, &result)
	switch status {
	case C.errSecSuccess:
		return C.SecKeyRef(result), true, nil
	case C.errSecItemNotFound:
		return C.SecKeyRef(0), false, nil
	default:
		return C.SecKeyRef(0), false, fmt.Errorf("secure enclave key lookup failed: %d (%s)", int(status), secErrorMessage(status))
	}
}

func seCreateKey(tag []byte) (C.SecKeyRef, error) {
	privateAttrs := cfDictionary(map[C.CFTypeRef]C.CFTypeRef{
		C.CFTypeRef(C.kSecAttrIsPermanent):    C.CFTypeRef(C.kCFBooleanTrue),
		C.CFTypeRef(C.kSecAttrApplicationTag): cfData(tag),
		C.CFTypeRef(C.kSecAttrAccessible):     C.CFTypeRef(C.kSecAttrAccessibleAfterFirstUnlockThisDeviceOnly),
	})
	defer C.CFRelease(C.CFTypeRef(privateAttrs))

	query := cfDictionary(map[C.CFTypeRef]C.CFTypeRef{
		C.CFTypeRef(C.kSecAttrKeyType):       C.CFTypeRef(C.kSecAttrKeyTypeECSECPrimeRandom),
		C.CFTypeRef(C.kSecAttrKeySizeInBits): cfNumber(256),
		C.CFTypeRef(C.kSecAttrTokenID):       C.CFTypeRef(C.kSecAttrTokenIDSecureEnclave),
		C.CFTypeRef(C.kSecPrivateKeyAttrs):   C.CFTypeRef(privateAttrs),
	})
	defer C.CFRelease(C.CFTypeRef(query))

	var errRef C.CFErrorRef
	key := C.SecKeyCreateRandomKey(query, &errRef)
	if key == 0 {
		return C.SecKeyRef(0), fmt.Errorf("secure enclave key creation failed: %s", cfErrorDescription(errRef))
	}
	return C.SecKeyRef(key), nil
}

func seSign(key C.SecKeyRef, digest []byte) ([]byte, error) {
	digestData := cfData(digest)
	defer C.CFRelease(C.CFTypeRef(digestData))

	var errRef C.CFErrorRef
	signature := C.SecKeyCreateSignature(
		key,
		C.kSecKeyAlgorithmECDSASignatureDigestX962SHA256,
		C.CFDataRef(digestData),
		&errRef,
	)
	if signature == 0 {
		return nil, fmt.Errorf("secure enclave signing failed: %s", cfErrorDescription(errRef))
	}
	defer C.CFRelease(C.CFTypeRef(signature))
	return cfDataBytes(C.CFDataRef(signature)), nil
}

func sePublicKeyDER(key C.SecKeyRef) ([]byte, error) {
	public := C.SecKeyCopyPublicKey(key)
	if public == 0 {
		return nil, errors.New("secure enclave public key is unavailable")
	}
	defer C.CFRelease(C.CFTypeRef(public))

	var errRef C.CFErrorRef
	external := C.SecKeyCopyExternalRepresentation(public, &errRef)
	if external == 0 {
		return nil, fmt.Errorf("secure enclave public key export failed: %s", cfErrorDescription(errRef))
	}
	defer C.CFRelease(C.CFTypeRef(external))

	raw := cfDataBytes(C.CFDataRef(external))
	if len(raw) != 65 || raw[0] != 0x04 {
		return nil, fmt.Errorf("unexpected secure enclave public key layout: %d bytes", len(raw))
	}
	x := new(big.Int).SetBytes(raw[1:33])
	y := new(big.Int).SetBytes(raw[33:65])
	pkix, err := x509.MarshalPKIXPublicKey(&ecdsa.PublicKey{
		Curve: elliptic.P256(),
		X:     x,
		Y:     y,
	})
	if err != nil {
		return nil, err
	}
	return pkix, nil
}

func cfNumber(value int) C.CFTypeRef {
	number := C.CFNumberCreate(C.CFAllocatorRef(0), C.kCFNumberIntType, unsafe.Pointer(&value))
	return C.CFTypeRef(number)
}

func cfData(data []byte) C.CFTypeRef {
	var ptr unsafe.Pointer
	if len(data) > 0 {
		ptr = unsafe.Pointer(&data[0])
	}
	created := C.CFDataCreate(C.CFAllocatorRef(0), (*C.UInt8)(ptr), C.CFIndex(len(data)))
	return C.CFTypeRef(created)
}

func cfDataBytes(data C.CFDataRef) []byte {
	length := int(C.CFDataGetLength(data))
	if length == 0 {
		return nil
	}
	return C.GoBytes(unsafe.Pointer(C.CFDataGetBytePtr(data)), C.int(length))
}

func cfDictionary(items map[C.CFTypeRef]C.CFTypeRef) C.CFDictionaryRef {
	keys := make([]C.CFTypeRef, 0, len(items))
	values := make([]C.CFTypeRef, 0, len(items))
	for k, v := range items {
		keys = append(keys, k)
		values = append(values, v)
	}
	created := C.CFDictionaryCreate(
		C.CFAllocatorRef(0),
		(*unsafe.Pointer)(unsafe.Pointer(&keys[0])),
		(*unsafe.Pointer)(unsafe.Pointer(&values[0])),
		C.CFIndex(len(items)),
		&C.kCFTypeDictionaryKeyCallBacks,
		&C.kCFTypeDictionaryValueCallBacks,
	)
	return C.CFDictionaryRef(created)
}

func cfErrorDescription(err C.CFErrorRef) string {
	if err == 0 {
		return "unknown error"
	}
	code := int(C.CFErrorGetCode(err))
	description := C.CFErrorCopyDescription(err)
	defer C.CFRelease(C.CFTypeRef(description))
	return cfStringGo(C.CFStringRef(description)) + fmt.Sprintf(" (code %d)", code)
}

func secErrorMessage(status C.OSStatus) string {
	message := C.SecCopyErrorMessageString(status, nil)
	if message == 0 {
		return fmt.Sprintf("status %d", int(status))
	}
	defer C.CFRelease(C.CFTypeRef(message))
	return cfStringGo(message)
}

func cfStringGo(value C.CFStringRef) string {
	if value == 0 {
		return ""
	}
	length := C.CFIndex(C.CFStringGetLength(value))
	if length == 0 {
		return ""
	}
	var rng C.CFRange
	rng.location = 0
	rng.length = length
	buffer := make([]byte, length*4)
	var used C.CFIndex
	if C.CFStringGetBytes(
		value,
		rng,
		C.kCFStringEncodingUTF8,
		0,
		0,
		(*C.UInt8)(unsafe.Pointer(&buffer[0])),
		C.CFIndex(len(buffer)),
		&used,
	) == 0 {
		return ""
	}
	return string(buffer[:used])
}

// ResetKey 删除 installID 对应的 Secure Enclave 私钥。
// 仅用于显式的设备身份重置流程与测试；删除后旧身份不可恢复。
func (s SecureEnclaveStore) ResetKey(installID string) error {
	query := cfDictionary(map[C.CFTypeRef]C.CFTypeRef{
		C.CFTypeRef(C.kSecClass):              C.CFTypeRef(C.kSecClassKey),
		C.CFTypeRef(C.kSecAttrKeyType):        C.CFTypeRef(C.kSecAttrKeyTypeECSECPrimeRandom),
		C.CFTypeRef(C.kSecAttrTokenID):        C.CFTypeRef(C.kSecAttrTokenIDSecureEnclave),
		C.CFTypeRef(C.kSecAttrApplicationTag): cfData(s.tag(installID)),
	})
	defer C.CFRelease(C.CFTypeRef(query))

	status := C.SecItemDelete(query)
	if status == C.errSecSuccess || status == C.errSecItemNotFound {
		return nil
	}
	return fmt.Errorf("secure enclave key reset failed: %d (%s)", int(status), secErrorMessage(status))
}
