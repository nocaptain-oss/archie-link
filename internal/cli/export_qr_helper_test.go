package cli

import (
	"bytes"
	"image"
	"image/png"

	"github.com/makiuchi-d/gozxing"
	qrreader "github.com/makiuchi-d/gozxing/qrcode"
)

func pngToImage(raw []byte) (image.Image, error) {
	return png.Decode(bytes.NewReader(raw))
}

func decodeQR(img image.Image) (string, error) {
	bitmap, err := gozxing.NewBinaryBitmapFromImage(img)
	if err != nil {
		return "", err
	}
	result, err := qrreader.NewQRCodeReader().Decode(bitmap, nil)
	if err != nil {
		return "", err
	}
	return result.GetText(), nil
}
