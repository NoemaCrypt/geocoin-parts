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
	"time"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"geocoin.parts/backend/internal/api"
	"geocoin.parts/backend/internal/chain"
	"geocoin.parts/backend/internal/node"
)

func main() {
	batchesDir := flag.String("batches", "provisioning-out", "каталог с подкаталогами батчей")
	logPath := flag.String("log", "provisioning-out/scans.jsonl", "JSONL-лог сканов")
	addr := flag.String("addr", ":8087", "адрес HTTP")

	// якорение в изолированную ноду degree-load
	anchor := flag.Bool("anchor", false, "якорить батчи и погашения в L1 через RPC ноды")
	rpcURL := flag.String("rpc-url", "http://127.0.0.1:23033/", "RPC ноды GeoCoin")
	rpcUser := flag.String("rpc-user", "admin", "RPC пользователь (admin)")
	rpcPassFile := flag.String("rpc-pass-file", "data/rpc-admin-pass", "файл с RPC паролем")
	keyFile := flag.String("node-key-file", "data/node-key", "файл с приватным ключом владельца (hex32, dev-only)")
	signer := flag.String("signer", "tools/falcon-signer/falcon-signer", "путь к falcon-signer")
	schemaHash := flag.String("schema-hash", "1df5c03de9b074d6768d9c79a97e06f924c11c9d1fbad116ddf3621c2c9e068f", "schema_hash учётного сайдчейна")
	sidechainName := flag.String("sidechain-name", "geocoin-parts-units", "имя учётного сайдчейна")
	anchorStatePath := flag.String("anchor-state", "provisioning-out/anchor-state.json", "файл состояния якорения")
	flag.Parse()

	c := chain.New()
	entries, err := os.ReadDir(*batchesDir)
	if err != nil {
		log.Fatal(err)
	}
	var firstRoot string
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
		if firstRoot == "" {
			firstRoot = c.BatchRootHex(b.ID)
		}
		log.Printf("якорен батч %s (%s): %d единиц, root %s", b.ID, b.Label, len(b.Units), c.BatchRootHex(b.ID))
	}

	srv := &api.Server{Chain: c, ScanLog: *logPath, Custody: chain.NewCustodyRegistry(filepath.Join(*batchesDir, "custody-state.json"))}

	if *anchor {
		pass, err := os.ReadFile(*rpcPassFile)
		if err != nil {
			log.Fatalf("rpc-pass-file: %v", err)
		}
		key, err := os.ReadFile(*keyFile)
		if err != nil {
			log.Fatalf("node-key-file: %v (см. scripts/bootstrap-node.sh)", err)
		}
		client := node.NewClient(*rpcURL, *rpcUser, strings.TrimSpace(string(pass)), *signer, strings.TrimSpace(string(key)))
		anc, err := node.LoadAnchorer(client, *schemaHash, "parts-units", *sidechainName, *anchorStatePath, 8.0, 5000, log.Printf)
		if err != nil {
			log.Fatal(err)
		}
		if err := anc.EnsureRegistered(); err != nil {
			log.Fatalf("якорение: %v", err)
		}
		if firstRoot != "" {
			srv.BaseRoot = firstRoot
			srv.PendingBaseline = true
		}
		srv.Anchor = anc
		// фоновый ретрай чекпоинтов: подтверждение регистрации и интервал L1
		go func() {
			for {
				time.Sleep(10 * time.Second)
				if !srv.HasPendingWork() {
					continue
				}
				if txid, err := srv.FlushPending(); err != nil {
					log.Printf("anchor retry: %v", err)
				} else {
					log.Printf("чекпоинт L1 отправлен (retry): txid=%s", txid)
				}
			}
		}()
	}

	log.Printf("verification backend на %s (anchor=%v)", *addr, *anchor)
	log.Fatal(http.ListenAndServe(*addr, srv.Router()))
}
