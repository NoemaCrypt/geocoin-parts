package sheet

import (
	"image"
	"image/color"

	"github.com/boombuler/barcode"
	"github.com/boombuler/barcode/qr"
)

// qrCode — независимый интерфейс матрицы: At == true означает чёрный модуль.
type qrCode interface {
	Bounds() image.Rectangle
	At(x, y int) bool
}

type boombulerQR struct{ b barcode.Barcode }

func (w boombulerQR) Bounds() image.Rectangle { return w.b.Bounds() }

// At — чёрный модуль: непрозрачный и тёмный (схемы цветов у boombuler разные).
func (w boombulerQR) At(x, y int) bool {
	r, g, b, a := w.b.At(x, y).RGBA()
	if a < 0x8000 {
		return false
	}
	lum := (r + g + b) / 3
	return lum < 0x8000
}

var _ color.Color = color.Black

// encodeQRInternal — QR с коррекцией M, автоматическая версия.
func encodeQRInternal(data string) (qrCode, error) {
	code, err := qr.Encode(data, qr.M, qr.Auto)
	if err != nil {
		return nil, err
	}
	return boombulerQR{code}, nil
}
