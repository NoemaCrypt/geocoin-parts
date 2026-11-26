// Package chain — mock-цепь сайдчейна «units» для Фазы 0.
//
// Хранит батчи (batch_root + набор unit_id→claim_hash), статус погашения и
// выдаёт Merkle-пруфы. Загрузка units.csv — потоковая (bufio); для тестовых
// батчей (≤ ~10k) допустим in-memory индекс, на проде состояние живёт в
// RocksDB сайдчейна, а batch_root якорится в L1.
package chain

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"geocoin.parts/merkle"
)

var (
	ErrUnitNotFound  = errors.New("unit not found in anchored batch")
	ErrAlreadySpent  = errors.New("unit already redeemed")
	ErrBadClaimCode  = errors.New("claim code does not match claim_hash")
	ErrBadProof      = errors.New("merkle proof does not verify against batch_root")
)

type Batch struct {
	ID    string
	Label string
	Root  [32]byte
	Units map[[32]byte][32]byte // unit_id -> claim_hash
	Leaves [][32]byte           // листья в порядке units.csv (детерминизм дерева)
	Spent map[[32]byte]bool
	VIN   map[[32]byte][32]byte // unit_id -> vin_hash (при погашении)
	SpentAt map[[32]byte]int64
}

type Chain struct {
	Batches map[string]*Batch
	trees   map[string]*merkle.Tree // mock: in-memory пруфы для тестовых батчей
}

func New() *Chain {
	return &Chain{Batches: map[string]*Batch{}, trees: map[string]*merkle.Tree{}}
}

// LoadBatch reads batch.json + units.csv and anchors it in the mock chain.
func LoadBatch(dir string) (*Batch, error) {
	raw, err := os.ReadFile(filepath.Join(dir, "batch.json"))
	if err != nil {
		return nil, err
	}
	var meta struct {
		BatchID    string `json:"batch_id"`
		Label      string `json:"label"`
		Units      int    `json:"units"`
		BatchRoot  string `json:"batch_root"`
		LeafDomain string `json:"leaf_domain"`
	}
	if err := json.Unmarshal(raw, &meta); err != nil {
		return nil, err
	}
	rootBytes, err := hex.DecodeString(meta.BatchRoot)
	if err != nil || len(rootBytes) != 32 {
		return nil, fmt.Errorf("bad batch_root: %w", err)
	}

	f, err := os.Open(filepath.Join(dir, "units.csv"))
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var root [32]byte
	copy(root[:], rootBytes)
	b := &Batch{
		ID:    meta.BatchID,
		Label: meta.Label,
		Root:  root,
		Units: make(map[[32]byte][32]byte, meta.Units),
		Spent: make(map[[32]byte]bool),
		VIN:   make(map[[32]byte][32]byte),
		SpentAt: make(map[[32]byte]int64),
	}
	r := csv.NewReader(bufio.NewReaderSize(f, 1<<20))
	header, err := r.Read()
	if err != nil {
		return nil, err
	}
	if len(header) != 2 || header[0] != "unit_id_hex" {
		return nil, errors.New("units.csv: unexpected header")
	}
	for {
		rec, err := r.Read()
		if err != nil {
			break // конец файла или ошибка разбора — обработано объёмом выше
		}
		uid, err := hex.DecodeString(rec[0])
		if err != nil || len(uid) != 32 {
			return nil, fmt.Errorf("bad unit_id row: %v", rec)
		}
		ch, err := hex.DecodeString(rec[1])
		if err != nil || len(ch) != 32 {
			return nil, fmt.Errorf("bad claim_hash row: %v", rec)
		}
		var u, c [32]byte
		copy(u[:], uid)
		copy(c[:], ch)
		b.Units[u] = c
		b.Leaves = append(b.Leaves, merkle.Leaf(u, c))
	}
	if len(b.Units) != meta.Units {
		return nil, fmt.Errorf("units.csv: %d rows, expected %d", len(b.Units), meta.Units)
	}
	return b, nil
}

// Anchor добавляет батч в mock-цепь и строит in-memory дерево для пруфов.
func (c *Chain) Anchor(b *Batch) error {
	tree := merkle.NewTree(b.Leaves)
	c.Batches[b.ID] = b
	c.trees[b.ID] = tree
	// mock-цепь проверяет, что batch_root из batch.json совпадает с деревом
	if tree.Root() != b.Root {
		return fmt.Errorf("in-memory tree root mismatch for batch %s", b.ID)
	}
	return nil
}

// ClaimHash возвращает заявленный claim_hash единицы.
func (c *Chain) ClaimHash(unitID [32]byte) ([32]byte, string, bool) {
	for _, b := range c.Batches {
		if ch, ok := b.Units[unitID]; ok {
			return ch, b.ID, true
		}
	}
	return [32]byte{}, "", false
}

// Proof выдаёт Merkle-прут единицы против batch_root её батча.
func (c *Chain) Proof(unitID [32]byte) ([][32]byte, string, bool) {
	ch, batchID, ok := c.ClaimHash(unitID)
	if !ok {
		return nil, "", false
	}
	p, ok := c.trees[batchID].Proof(merkle.Leaf(unitID, ch))
	return p, batchID, ok
}

// Redeem погашает единицу: проверка кода + Merkle-пруф + статус.
func (c *Chain) Redeem(unitID [32]byte, claimCode string, vinHash [32]byte, ts int64) error {
	ch, batchID, ok := c.ClaimHash(unitID)
	if !ok {
		return ErrUnitNotFound
	}
	b := c.Batches[batchID]
	if b.Spent[unitID] {
		return ErrAlreadySpent
	}
	want := sha256.Sum256(append(append([]byte("geocoin:parts:claim:v1"), unitID[:]...), []byte(claimCode)...))
	if !bytes.Equal(want[:], ch[:]) {
		return ErrBadClaimCode
	}
	proof, _, ok := c.Proof(unitID)
	if !ok || !merkle.VerifyProof(merkle.Leaf(unitID, ch), b.Root, proof) {
		return ErrBadProof
	}
	b.Spent[unitID] = true
	b.SpentAt[unitID] = ts
	b.VIN[unitID] = vinHash
	return nil
}

// Status единицы: redeemed-дата или nil.
func (c *Chain) Status(unitID [32]byte) (bool, int64, string, bool) {
	_, batchID, ok := c.ClaimHash(unitID)
	if !ok {
		return false, 0, "", false
	}
	b := c.Batches[batchID]
	return b.Spent[unitID], b.SpentAt[unitID], batchID, true
}

// BatchRootHex возвращает hex корня батча.
func (c *Chain) BatchRootHex(batchID string) string {
	if b, ok := c.Batches[batchID]; ok {
		return hex.EncodeToString(b.Root[:])
	}
	return ""
}

// ScanKind — тип записи скана в JSONL-логе (потоковая запись, append-only).
type ScanRecord struct {
	UnitID   string  `json:"unit_id"`
	TS       int64   `json:"ts"`
	Lat      float64 `json:"lat"`
	Lon      float64 `json:"lon"`
	Source   string  `json:"source"`
	RiskScore int    `json:"risk_score"`
	Flags    []string `json:"flags"`
}

// AppendScan дописывает скан в JSONL-лог (bufio, без перечитывания истории).
func AppendScan(logPath string, rec ScanRecord) error {
	f, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	w := bufio.NewWriter(f)
	defer w.Flush()
	return json.NewEncoder(w).Encode(rec)
}

// ScanLines читает лог построчно (используется риск-модулем и только для
// одного unit_id — на проде это индекс в БД, а не полный проход файла).
func ScanLines(logPath string, fn func(ScanRecord) error) error {
	f, err := os.Open(logPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	defer f.Close()
	s := bufio.NewScanner(f)
	s.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for s.Scan() {
		line := strings.TrimSpace(s.Text())
		if line == "" {
			continue
		}
		var rec ScanRecord
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			continue
		}
		if err := fn(rec); err != nil {
			return err
		}
	}
	return s.Err()
}
