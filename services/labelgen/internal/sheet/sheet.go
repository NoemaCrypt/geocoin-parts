// Package sheet — тиражный профиль печати этикеток «GeoCoin Parts».
//
// Один лист A4 — сетка этикеток. На каждой этикетке:
//   - открытая зона: QR с unit_id (проверка до покупки) + публичный серийник;
//   - СКРЕТЧ-ЗОНА (пунктирная рамка): QR с claim_code + текстовый дубль
//     Base58 группами по 6 — закрывается скретч-наклейкой при печати тиража;
//   - сервисная строка: метка партии и индекс лист/этикетка для сверочника.
//
// Выход: labels.svg (вектор, просмотр/веб) и labels.pdf (вектор, печать) —
// модули QR рисуются прямоугольниками, шрифты base-14 (текст только ASCII).
package sheet

import (
	"fmt"
	"strings"
)

const (
	// A4 в мм
	A4W = 210.0
	A4H = 297.0

	// DefaultLabel geometry
	LabelW    = 90.0
	LabelH    = 54.0
	Cols      = 2
	Rows      = 5
	MarginX   = 10.0
	MarginY   = 13.0
	GapX      = 5.0
	GapY      = 5.0
)

// Label — данные одной этикетки.
type Label struct {
	UnitID    string // hex64
	ClaimCode string // XXXXXX-XXXXXX-XXXXXX-XXXXXX
	BatchName string
	Serial    string // короткий публичный серийник (первые 16 hex)
	PublicURL string // что кодирует внешний QR (страница проверки)
}

// SheetsCount — сколько листов A4 нужно для n этикеток.
func SheetsCount(n int) int {
	per := Cols * Rows
	return (n + per - 1) / per
}

// mm→pt
func mm(v float64) float64 { return v * 72.0 / 25.4 }

// gridPosSVG — позиция этикетки на листе (мм, от ЛЕВОГО ВЕРХНЕГО угла — SVG).
func gridPosSVG(idx int) (x, y float64) {
	col := idx % Cols
	row := (idx / Cols) % Rows
	x = MarginX + float64(col)*(LabelW+GapX)
	y = MarginY + float64(row)*(LabelH+GapY)
	return x, y
}

// gridPosPDF — та же позиция от ЛЕВОГО НИЖНЕГО угла (PDF); возвращает верх этикетки.
func gridPosPDF(idx int) (x, y float64) {
	x, yt := gridPosSVG(idx)
	return x, A4H - yt
}

// ─────────────────────────────────────────────────────────────────────────────
// SVG

// SVGSheet — один лист этикеток в SVG.
func SVGSheet(labels []Label, sheetIdx int) string {
	var b strings.Builder
	fmt.Fprintf(&b, "<svg xmlns=\"http://www.w3.org/2000/svg\" width=\"%.1fmm\" height=\"%.1fmm\" viewBox=\"0 0 %.1f %.1f\">",
		A4W, A4H, A4W, A4H)
	fmt.Fprintf(&b, "<rect width=\"%.1f\" height=\"%.1f\" fill=\"white\"/>", A4W, A4H)
	fmt.Fprintf(&b, "<text x=\"%.1f\" y=\"%.1f\" font-size=\"3.2\" fill=\"#888\" font-family=\"monospace\">GeoCoin Parts · %s · sheet %d</text>",
		6.0, 6.5, svgEsc(labels[0].BatchName), sheetIdx)

	for i, l := range labels {
		x, y := gridPosSVG(i)
		drawLabelSVG(&b, l, x, y)
	}
	b.WriteString("</svg>")
	return b.String()
}

func drawLabelSVG(b *strings.Builder, l Label, x, y float64) {
	// рамка этикетки
	fmt.Fprintf(b, "<rect x=\"%.1f\" y=\"%.1f\" width=\"%.1f\" height=\"%.1f\" fill=\"none\" stroke=\"#000\" stroke-width=\"0.3\"/>", x, y, LabelW, LabelH)

	// шапка
	fmt.Fprintf(b, "<text x=\"%.1f\" y=\"%.1f\" font-size=\"3.4\" font-weight=\"bold\" font-family=\"sans-serif\" fill=\"#000\">GeoCoin Parts — original part</text>", x+3, y+6)
	fmt.Fprintf(b, "<text x=\"%.1f\" y=\"%.1f\" font-size=\"2.6\" font-family=\"sans-serif\" fill=\"#000\">Scan the outer QR to verify before installation</text>", x+3, y+10.5)
	fmt.Fprintf(b, "<text x=\"%.1f\" y=\"%.1f\" font-size=\"2.6\" font-family=\"monospace\" fill=\"#000\">unit %s…</text>", x+3, y+14.5, l.Serial)
	fmt.Fprintf(b, "<text x=\"%.1f\" y=\"%.1f\" font-size=\"2.2\" font-family=\"monospace\" fill=\"#555\">batch %s</text>", x+3, y+18, svgEsc(l.BatchName))

	// открытый QR (unit_id), слева
	drawQRSVG(b, l.UnitID, x+3, y+20.5, 30)

	// подпись под открытым QR
	fmt.Fprintf(b, "<text x=\"%.1f\" y=\"%.1f\" font-size=\"2.3\" font-family=\"sans-serif\" fill=\"#000\">PUBLIC · verify on geocoin.parts</text>", x+3, y+53)

	// скретч-зона (справа)
	sx, sy, sw, sh := x+36, y+20.5, LabelW-36-3, 32.0
	fmt.Fprintf(b, "<rect x=\"%.1f\" y=\"%.1f\" width=\"%.1f\" height=\"%.1f\" fill=\"none\" stroke=\"#000\" stroke-width=\"0.3\" stroke-dasharray=\"1.4 1\"/>", sx, sy, sw, sh)
	fmt.Fprintf(b, "<text x=\"%.1f\" y=\"%.1f\" font-size=\"2.4\" font-weight=\"bold\" font-family=\"sans-serif\" fill=\"#000\">INSTALL CODE — under scratch</text>", sx+2, sy+5)
	drawQRSVG(b, strings.ReplaceAll(l.ClaimCode, "-", ""), sx+2, sy+6.5, 15.5)
	fmt.Fprintf(b, "<text x=\"%.1f\" y=\"%.1f\" font-size=\"2.5\" font-family=\"monospace\" fill=\"#000\">%s</text>", sx+2, sy+27, l.ClaimCode)
	fmt.Fprintf(b, "<text x=\"%.1f\" y=\"%.1f\" font-size=\"1.9\" font-family=\"sans-serif\" fill=\"#555\">or type the code, groups of 6, no 0/O/1/I/l</text>", sx+2, sy+30.2)

	// низ: сервисная строка
}

// drawQRSVG рисует матрицу QR чёрными квадратами (мм координаты).
func drawQRSVG(b *strings.Builder, data string, x, y, size float64) {
	qr, err := encodeQR(data)
	if err != nil {
		return
	}
	bd := qr.Bounds()
	n := bd.Dx() // квадрат
	step := size / float64(n)
	fmt.Fprintf(b, "<g fill=\"#000\">")
	for j := 0; j < bd.Dy(); j++ {
		for i := 0; i < bd.Dx(); i++ {
			if qr.At(i, j) {
				fmt.Fprintf(b, "<rect x=\"%.2f\" y=\"%.2f\" width=\"%.2f\" height=\"%.2f\"/>",
					x+float64(i)*step, y+float64(j)*step, step+0.02, step+0.02)
			}
		}
	}
	b.WriteString("</g>")
}

// ascii — только печатные ASCII для PDF (base-14 шрифты).
func ascii(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r >= 32 && r < 127 {
			b.WriteRune(r)
		} else {
			b.WriteRune('?')
		}
	}
	return b.String()
}

func svgEsc(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	return s
}

// ─────────────────────────────────────────────────────────────────────────────
// PDF (вектор, base-14 шрифты, ASCII-текст)

// PDFSheet добавляет лист в документ pdfWriter.
func PDFSheet(p *pdfWriter, labels []Label, sheetIdx int) {
	p.AddPage(func(c *content) {
		c.Font("F1", 7).SetGray(0.5).Text(6, A4H-6, fmt.Sprintf("GeoCoin Parts · %s · sheet %d", ascii(labels[0].BatchName), sheetIdx)).SetGray(0)
		for i, l := range labels {
			x, y := gridPosPDF(i)
			drawLabelPDF(c, l, x, y)
		}
	})
}

func drawLabelPDF(c *content, l Label, x, y float64) {
	// координаты PDF — снизу вверх; y здесь "верх этикетки" в мм от низа листа
	top := y
	c.Rect(x, top-LabelH, LabelW, LabelH, false) // рамка

	c.Font("F2", 9.6).Text(x+mm(3), top-mm(6), "GeoCoin Parts - original part")
	c.Font("F1", 7.4).Text(x+mm(3), top-mm(10.5), "Scan the outer QR to verify before installation")
	c.Font("F3", 7.4).Text(x+mm(3), top-mm(14.5), "unit "+l.Serial+"...")
	c.Font("F3", 6.2).SetGray(0.4).Text(x+mm(3), top-mm(18), "batch "+ascii(l.BatchName)).SetGray(0)

	// открытый QR
	drawQRPDF(c, l.UnitID, x+mm(3), top-mm(20.5)-mm(30), mm(30))
	c.Font("F1", 6.5).Text(x+mm(3), top-mm(52.5), "PUBLIC · verify")

	// скретч-зона
	sx, sw, sh := x+mm(36), mm(LabelW-36-3), mm(32)
	c.DashedRect(sx, top-mm(20.5)-sh, sw, sh)
	c.Font("F2", 6.8).Text(sx+mm(2), top-mm(25.5), "INSTALL CODE - under scratch")
	drawQRPDF(c, strings.ReplaceAll(l.ClaimCode, "-", ""), sx+mm(2), top-mm(27)-mm(15.5), mm(15.5))
	c.Font("F3", 7.1).Text(sx+mm(2), top-mm(47), l.ClaimCode)
	c.Font("F1", 5.4).SetGray(0.4).Text(sx+mm(2), top-mm(50.2), "or type the code, groups of 6, no 0/O/1/I/l").SetGray(0)

	c.Font("F3", 5.7).SetGray(0.4).Text(x+mm(3), top-mm(2.2), "geo:claim?u="+l.UnitID[:24]).SetGray(0)
}

// drawQRPDF рисует QR в PDF-координатах: (px,py) — нижний левый угол, size в pt.
func drawQRPDF(c *content, data string, px, py, size float64) {
	qr, err := encodeQR(data)
	if err != nil {
		return
	}
	bd := qr.Bounds()
	n := bd.Dx()
	step := size / float64(n)
	for j := 0; j < bd.Dy(); j++ {
		for i := 0; i < bd.Dx(); i++ {
			if qr.At(i, j) {
				c.Rect(px+float64(i)*step, py+size-float64(j+1)*step, step, step, true)
			}
		}
	}
}

// encodeQR — QR level M, максимально «грубая» версия для надёжности печати.
func encodeQR(data string) (qrCode, error) {
	return encodeQRInternal(data)
}

