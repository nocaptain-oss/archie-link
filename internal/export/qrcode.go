package export

import (
	qrcode "github.com/skip2/go-qrcode"
)

// qrPNGSize is large enough for on-screen scanning with High error correction.
const qrPNGSize = 512

// QRPNG renders a full WireGuard config as a QR code PNG.
// The caller must treat the result as private-key material.
func QRPNG(config []byte) ([]byte, error) {
	code, err := qrcode.New(string(config), qrcode.High)
	if err != nil {
		return nil, err
	}
	return code.PNG(qrPNGSize)
}
