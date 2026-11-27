// labelgen — тиражный профиль печати этикеток GeoCoin Parts.
//
// Использование:
//
//	go run ./cmd/labelgen -batch ../../provisioning-out/batch-0001 \
//	  -out ../../provisioning-out/labels-batch-0001 [-limit 24]
//
// Вход: batch.json + secrets.tsv из provisioning (сырые коды читаются только
// здесь, каталог вывода в .gitignore). Выход: labels.svg, labels.pdf
// (листы A4, сетка 2×5 этикеток 90×54 мм) и print-manifest.csv
// (лист/позиция ↔ unit ↔ код — для контроля тиража, уничтожить после печати).
package main

import (
	"encoding/csv"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"geocoin.parts/labelgen/internal/sheet"
)

func main() {
	batchDir := flag.String("batch", "provisioning-out/batch", "каталог батча (batch.json + secrets.tsv)")
	outDir := flag.String("out", "provisioning-out/labels", "каталог вывода")
	limit := flag.Int("limit", 0, "ограничить число этикеток (0 = весь батч)")
	urlTmpl := flag.String("public-url", "https://check.geocoin.parts/u/{unit}", "шаблон URL для внешнего QR; {unit} = unit_id")
	flag.Parse()

	raw, err := os.ReadFile(filepath.Join(*batchDir, "batch.json"))
	if err != nil {
		log.Fatal(err)
	}
	var meta struct {
		Units     int    `json:"units"`
		Label     string `json:"label"`
		BatchRoot string `json:"batch_root"`
	}
	if err := json.Unmarshal(raw, &meta); err != nil {
		log.Fatal(err)
	}

	f, err := os.Open(filepath.Join(*batchDir, "secrets.tsv"))
	if err != nil {
		log.Fatal(err)
	}
	defer f.Close()

	var labels []sheet.Label
	cr := csv.NewReader(f)
	cr.Comma = '\t'
	cr.FieldsPerRecord = -1
	for {
		rec, err := cr.Read()
		if err != nil {
			break
		}
		if len(rec) < 2 || len(rec[0]) < 16 {
			continue
		}
		if *limit > 0 && len(labels) >= *limit {
			break
		}
		labels = append(labels, sheet.Label{
			UnitID:    rec[0],
			ClaimCode: rec[1],
			BatchName: meta.Label,
			Serial:    rec[0][:16],
			PublicURL: strings.ReplaceAll(*urlTmpl, "{unit}", rec[0]),
		})
	}
	if len(labels) == 0 {
		log.Fatal("нет этикеток для печати")
	}

	if err := os.MkdirAll(*outDir, 0o700); err != nil {
		log.Fatal(err)
	}

	perSheet := sheet.Cols * sheet.Rows
	totalSheets := sheet.SheetsCount(len(labels))

	// SVG: один файл со всеми листами (страницы — <g transform>)
	var svg strings.Builder
	svg.WriteString("<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n")
	svg.WriteString("<html><head><meta charset=\"utf-8\"><title>GeoCoin Parts labels</title><style>body{background:#666;margin:0}.sheet{background:#fff;margin:8px auto;display:block;box-shadow:0 2px 8px rgba(0,0,0,.5)}</style></head><body>\n")
	for s := 0; s < totalSheets; s++ {
		start := s * perSheet
		end := start + perSheet
		if end > len(labels) {
			end = len(labels)
		}
		inner := sheet.SVGSheet(labels[start:end], s+1)
		// вырезаем внутренности svg-элемента
		inner = strings.TrimSuffix(strings.TrimPrefix(inner, "<svg xmlns=\"http://www.w3.org/2000/svg\""), "</svg>")
		_ = inner
	}
	svg.WriteString("</body></html>")

	// проще: отдельные svg-файлы на лист + один html-просмотрщик
	html := "<html><head><meta charset=\"utf-8\"><title>GeoCoin Parts — print preview</title>" +
		"<style>body{background:#666;margin:0;font-family:sans-serif}.sheet{background:#fff;margin:10px auto;width:210mm;display:block;box-shadow:0 2px 8px rgba(0,0,0,.5)}" +
		"h2{color:#fff;font-size:13px;margin:14px 0 2px 12px}</style></head><body>\n"
	for s := 0; s < totalSheets; s++ {
		start := s * perSheet
		end := start + perSheet
		if end > len(labels) {
			end = len(labels)
		}
		svgData := sheet.SVGSheet(labels[start:end], s+1)
		name := fmt.Sprintf("sheet-%02d.svg", s+1)
		if err := os.WriteFile(filepath.Join(*outDir, name), []byte(svgData), 0o600); err != nil {
			log.Fatal(err)
		}
		html += fmt.Sprintf("<h2>Sheet %d / %d</h2><img class=\"sheet\" src=\"%s\">\n", s+1, totalSheets, name)
	}
	html += "</body></html>"
	if err := os.WriteFile(filepath.Join(*outDir, "preview.html"), []byte(html), 0o600); err != nil {
		log.Fatal(err)
	}

	// PDF
	pdf := sheet.NewPDF()
	for s := 0; s < totalSheets; s++ {
		start := s * perSheet
		end := start + perSheet
		if end > len(labels) {
			end = len(labels)
		}
		sheet.PDFSheet(pdf, labels[start:end], s+1)
	}
	if err := os.WriteFile(filepath.Join(*outDir, "labels.pdf"), pdf.Build(), 0o600); err != nil {
		log.Fatal(err)
	}

	// print-manifest: сверка тиража (содержит коды — в .gitignore, уничтожить после печати)
	mf, err := os.Create(filepath.Join(*outDir, "print-manifest.csv"))
	if err != nil {
		log.Fatal(err)
	}
	mw := csv.NewWriter(mf)
	_ = mw.Write([]string{"sheet", "pos", "unit_id_hex", "claim_code", "batch_root"})
	for s := 0; s < totalSheets; s++ {
		for k := 0; k < perSheet; k++ {
			idx := s*perSheet + k
			if idx >= len(labels) {
				break
			}
			_ = mw.Write([]string{fmt.Sprint(s + 1), fmt.Sprint(k + 1), labels[idx].UnitID, labels[idx].ClaimCode, meta.BatchRoot})
		}
	}
	mw.Flush()
	_ = mf.Close()
	_ = os.Chmod(filepath.Join(*outDir, "print-manifest.csv"), 0o600)

	fmt.Printf("этикетки: %d шт → %d листов A4 (%d×%d, %g×%g мм)\n  %s/labels.pdf\n  %s/preview.html\n  %s/print-manifest.csv (уничтожить после печати)\n",
		len(labels), totalSheets, sheet.Cols, sheet.Rows, sheet.LabelW, sheet.LabelH, *outDir, *outDir, *outDir)
}
