package sheet

import (
	"bytes"
	"compress/zlib"
	"fmt"
		"strings"
)

// pdfWriter — минимальный генератор векторного PDF (A4, base-14 шрифты).
// Достаточно для этикеток: страницы с content-потоками (rects/text), без
// растровых изображений и внешних шрифтов.
type pdfWriter struct {
	pages [][]byte
}

func NewPDF() *pdfWriter { return &pdfWriter{} }

func (p *pdfWriter) AddPage(f func(*content)) {
	c := &content{}
	f(c)
	p.pages = append(p.pages, c.buf.Bytes())
}

// Build собирает PDF и возвращает его байты.
func (p *pdfWriter) Build() []byte {
	var out bytes.Buffer
	write := func(s string) { out.WriteString(s) }

	write("%PDF-1.4\n%\xE2\xE3\xCF\xD3\n")

	// 1: Catalog, 2: Pages, 3..: Page/Contents pairs, затем шрифты
	nPages := len(p.pages)
	catalogObj := 1
	pagesObj := 2
	fontF1 := 3
	fontF2 := 4
	fontF3 := 5
	firstPageObj := 6
	const objsPerPage = 2

	offsets := map[int]int{}

	flushObj := func(num int, body string) {
		offsets[num] = out.Len()
		write(fmt.Sprintf("%d 0 obj\n%s\nendobj\n", num, body))
	}

	flushObj(catalogObj, fmt.Sprintf("<< /Type /Catalog /Pages %d 0 R >>", pagesObj))

	kids := make([]string, nPages)
	for i := range kids {
		kids[i] = fmt.Sprintf("%d 0 R", firstPageObj+i*objsPerPage)
	}
	flushObj(pagesObj, fmt.Sprintf("<< /Type /Pages /Kids [%s] /Count %d >>",
		strings.Join(kids, " "), nPages))

	flushObj(fontF1, "<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica /Encoding /WinAnsiEncoding >>")
	flushObj(fontF2, "<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica-Bold /Encoding /WinAnsiEncoding >>")
	flushObj(fontF3, "<< /Type /Font /Subtype /Type1 /BaseFont /Courier /Encoding /WinAnsiEncoding >>")

	w, h := mm(A4W), mm(A4H)
	for i, stream := range p.pages {
		pageObj := firstPageObj + i*objsPerPage
		contObj := pageObj + 1
		flushObj(pageObj, fmt.Sprintf(
			"<< /Type /Page /Parent %d 0 R /MediaBox [0 0 %.2f %.2f] /Resources << /Font << /F1 %d 0 R /F2 %d 0 R /F3 %d 0 R >> >> /Contents %d 0 R >>",
			pagesObj, w, h, fontF1, fontF2, fontF3, contObj))
		// content: Flate
		var z bytes.Buffer
		zw := zlib.NewWriter(&z)
		_, _ = zw.Write(stream)
		_ = zw.Close()
		offsets[contObj] = out.Len()
		fmt.Fprintf(&out, "%d 0 obj\n<< /Length %d /Filter /FlateDecode >>\nstream\n", contObj, z.Len())
		out.Write(z.Bytes())
		write("\nendstream\nendobj\n")
	}

	total := firstPageObj + nPages*objsPerPage
	xref := out.Len()
	fmt.Fprintf(&out, "xref\n0 %d\n", total)
	write("0000000000 65535 f \n")
	for i := 1; i < total; i++ {
		fmt.Fprintf(&out, "%010d 00000 n \n", offsets[i])
	}
	fmt.Fprintf(&out, "trailer\n<< /Size %d /Root %d 0 R >>\nstartxref\n%d\n%%%%EOF\n", total, catalogObj, xref)
	return out.Bytes()
}

// content — построение страницы: текст (ASCII) и прямоугольники.
type content struct {
	buf      bytes.Buffer
	font     string
	size     float64
	gray     float64
	dash     string
	curFont  string
	curSize  float64
}

func (c *content) Font(name string, size float64) *content {
	c.font, c.size = name, size
	return c
}

func (c *content) SetGray(v float64) *content {
	c.gray = v
	return c
}

// Text — ASCII текст; (x,y) — мм от левого НИЖНЕГО угла (базовая линия).
func (c *content) Text(xmm, ymm float64, s string) *content {
	if c.curFont != c.font || c.curSize != c.size {
		fmt.Fprintf(&c.buf, "BT /%s %.2f Tf ET\n", c.font, mm(c.size))
		c.curFont, c.curSize = c.font, c.size
	}
	escaped := strings.ReplaceAll(s, "\\", "\\\\")
	escaped = strings.ReplaceAll(escaped, "(", "\\(")
	escaped = strings.ReplaceAll(escaped, ")", "\\)")
	fmt.Fprintf(&c.buf, "%.3f %.3f Td (%s) Tj\n", mm(xmm), mm(ymm), escaped)
	return c
}

// Rect — прямоугольник (мм от нижнего угла); fill=true — залить чёрным.
func (c *content) Rect(xmm, ymm, wmm, hmm float64, fill bool) *content {
	if fill {
		fmt.Fprintf(&c.buf, "%.3f %.3f %.3f %.3f re f\n", mm(xmm), mm(ymm), mm(wmm), mm(hmm))
	} else {
		fmt.Fprintf(&c.buf, "%.3f %.3f %.3f %.3f re S\n", mm(xmm), mm(ymm), mm(wmm), mm(hmm))
	}
	return c
}

// DashedRect — пунктирная рамка (скретч-зона).
func (c *content) DashedRect(xmm, ymm, wmm, hmm float64) *content {
	fmt.Fprintf(&c.buf, "[2 1.4] 0 d\n%.3f %.3f %.3f %.3f re S\n[] 0 d\n", mm(xmm), mm(ymm), mm(wmm), mm(hmm))
	return c
}

