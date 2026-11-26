// backend — verification backend + mock-цепь для Фазы 0.
//
// Использование:
//
//	go run ./cmd/backend -batches ../../provisioning-out -log ../../provisioning-out/scans.jsonl -addr :8087
//
// Якорит все каталоги с batch.json + units.csv из -batches и поднимает API.
package main

import (
	"flag"
	"log"
	"net/http"
	"os"
	"path/filepath"

	"geocoin.parts/backend/internal/api"
	"geocoin.parts/backend/internal/chain"
)

func main() {
	batchesDir := flag.String("batches", "provisioning-out", "каталог с подкаталогами батчей")
	logPath := flag.String("log", "provisioning-out/scans.jsonl", "JSONL-лог сканов")
	addr := flag.String("addr", ":8087", "адрес HTTP")
	flag.Parse()

	c := chain.New()
	entries, err := os.ReadDir(*batchesDir)
	if err != nil {
		log.Fatal(err)
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		dir := filepath.Join(*batchesDir, e.Name())
		if _, err := os.Stat(filepath.Join(dir, "batch.json")); err != nil {
			continue
		}
		b, err := chain.LoadBatch(dir)
		if err != nil {
			log.Printf("батч %s пропущен: %v", e.Name(), err)
			continue
		}
		if err := c.Anchor(b); err != nil {
			log.Fatalf("батч %s: %v", e.Name(), err)
		}
		log.Printf("якорен батч %s (%s): %d единиц, root %s", b.ID, b.Label, len(b.Units), c.BatchRootHex(b.ID))
	}

	srv := &api.Server{Chain: c, ScanLog: *logPath}
	log.Printf("verification backend на %s", *addr)
	log.Fatal(http.ListenAndServe(*addr, srv.Router()))
}
