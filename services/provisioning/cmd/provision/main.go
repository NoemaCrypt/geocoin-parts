// provision — потоковый генератор батчей единиц «банкнот».
//
// Использование:
//
//	go run ./cmd/provision -n 1000 -out ../../provisioning-out/batch-0001
//
// Выход:
//   - batch.json   — batch_id, batch_root (Merkle), число единиц, версия схемы
//   - units.csv    — unit_id_hex,claim_hash_hex  (то, что якорится в сайдчейн)
//   - secrets.tsv  — unit_id_hex \t claim_code_text \t qr_payload (печатается
//     на этикетки со скретчем и уничтожается; каталог в .gitignore)
//
// Всё пишется потоково (bufio): RAM не зависит от размера батча.
package main

import (
	"bufio"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"geocoin.parts/merkle"
)

const (
	claimDomain = "geocoin:parts:claim:v1"
	secretBytes = 16 // 128 бит энтропии
)

func main() {
	n := flag.Int("n", 1000, "число единиц в батче")
	out := flag.String("out", "provisioning-out/batch", "каталог вывода")
	label := flag.String("label", "batch-0001", "метка партии (Porsche пилот: артикул/завод)")
	flag.Parse()
	if *n <= 0 {
		log.Fatal("-n должно быть положительным")
	}

	if err := os.MkdirAll(*out, 0o700); err != nil {
		log.Fatal(err)
	}

	unitsPath := filepath.Join(*out, "units.csv")
	secretsPath := filepath.Join(*out, "secrets.tsv")
	leavesPath := filepath.Join(*out, "leaves.bin")

	if err := generate(*n, unitsPath, secretsPath, leavesPath); err != nil {
		log.Fatal(err)
	}

	root, err := merkle.Root(leavesPath, *out)
	if err != nil {
		log.Fatal(err)
	}
	_ = os.Remove(leavesPath) // промежуточный уровень более не нужен

	batchID := sha256.Sum256(append([]byte("geocoin:parts:batch:v1"), []byte(*label)...))
	batchJSON := fmt.Sprintf(`{
  "schema_id": "com.geocoin.parts.unit",
  "schema_version": 1,
  "batch_id": %q,
  "label": %q,
  "units": %d,
  "batch_root": %q,
  "leaf_domain": "geocoin:parts:leaf:v1",
  "node_domain": "geocoin:parts:node:v1"
}
`, hex.EncodeToString(batchID[:]), *label, *n, merkle.RootHex(root))

	if err := os.WriteFile(filepath.Join(*out, "batch.json"), []byte(batchJSON), 0o600); err != nil {
		log.Fatal(err)
	}
	// secrets доступны только на чтение владельцу — их печатаем на этикетки и уничтожаем
	_ = os.Chmod(secretsPath, 0o600)

	fmt.Printf("батч %s: %d единиц\n  batch_root: %s\n  units.csv:  %s\n  secrets:    %s (уничтожить после печати)\n",
		*label, *n, merkle.RootHex(root), unitsPath, secretsPath)
}

// generate пишет units.csv, secrets.tsv и leaves.bin потоком, по одной единице.
func generate(n int, unitsPath, secretsPath, leavesPath string) error {
	unitsF, err := os.OpenFile(unitsPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	secretsF, err := os.OpenFile(secretsPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	leavesF, err := os.OpenFile(leavesPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	defer func() { _ = unitsF.Close(); _ = secretsF.Close(); _ = leavesF.Close() }()

	ub := bufio.NewWriterSize(unitsF, 1<<20)
	sb := bufio.NewWriterSize(secretsF, 1<<20)
	lb := bufio.NewWriterSize(leavesF, 1<<20)
	defer func() { _ = ub.Flush(); _ = sb.Flush(); _ = lb.Flush() }()

	if _, err = fmt.Fprintln(ub, "unit_id_hex,claim_hash_hex"); err != nil {
		return err
	}

	var unitID, claimHash [32]byte
	var secret [16]byte
	for i := 0; i < n; i++ {
		if _, err = rand.Read(unitID[:]); err != nil {
			return err
		}
		if _, err = rand.Read(secret[:]); err != nil {
			return err
		}

		code := encodeClaimCode(secret)                    // Base58, 24 симв., группировка дефисами
		canonical := strings.ReplaceAll(code, "-", "")     // хеш по канонической форме без дефисов
		claimHash = sha256.Sum256(append(append([]byte(claimDomain), unitID[:]...), []byte(canonical)...))
		leaf := merkle.Leaf(unitID, claimHash)

		qrPayload := fmt.Sprintf("geo:claim?u=%s&c=%s", hex.EncodeToString(unitID[:]), code)

		if _, err = fmt.Fprintf(ub, "%s,%s\n", hex.EncodeToString(unitID[:]), hex.EncodeToString(claimHash[:])); err != nil {
			return err
		}
		if _, err = fmt.Fprintf(sb, "%s\t%s\t%s\n", hex.EncodeToString(unitID[:]), code, qrPayload); err != nil {
			return err
		}
		if _, err = lb.Write(leaf[:]); err != nil {
			return err
		}
	}
	return nil
}
