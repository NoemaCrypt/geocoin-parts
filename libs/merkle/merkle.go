// Package merkle implements a streaming SHA-256 Merkle tree over 32-byte leaves.
//
// Сборка дерева не держит все листья в памяти: листья пишутся на диск поэтапно
// (bufio), каждый следующий уровень редуцируется из файла предыдущего уровня.
// Позволяет строить batch_root для 1M+ единиц при фиксированном расходе RAM.
package merkle

import (
	"bufio"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

const (
	leafDomain = "geocoin:parts:leaf:v1"
	nodeDomain = "geocoin:parts:node:v1"
	leafSize   = 32
)

// Leaf computes the canonical leaf hash for a unit:
// SHA-256("geocoin:parts:leaf:v1" || unit_id || claim_hash).
func Leaf(unitID, claimHash [32]byte) [32]byte {
	var buf [32 + 32 + len(leafDomain)]byte
	n := copy(buf[:], leafDomain)
	n += copy(buf[n:], unitID[:])
	n += copy(buf[n:], claimHash[:])
	return sha256.Sum256(buf[:n])
}

// Root reads a raw file of 32-byte leaves (streaming, bufio), reduces it
// level by level through temporary files in workDir, and returns the root.
// Odd trailing node at each level is promoted as-is (duplication-free).
func Root(leavesPath, workDir string) ([32]byte, error) {
	var root [32]byte
	level := 0
	src := leavesPath
	defer func() {
		_ = os.RemoveAll(filepath.Join(workDir, "merkle"))
	}()

	for {
		count, err := leafCount(src)
		if err != nil {
			return root, err
		}
		switch count {
		case 0:
			return root, errors.New("merkle: empty leaf set")
		case 1:
			return readLeaf(src, 0)
		}

		dst := filepath.Join(workDir, "merkle", fmt.Sprintf("level-%d.bin", level))
		if err := reduce(src, dst); err != nil {
			return root, err
		}
		if src != leavesPath {
			_ = os.Remove(src)
		}
		src = dst
		level++
	}
}

func leafCount(path string) (int64, error) {
	fi, err := os.Stat(path)
	if err != nil {
		return 0, err
	}
	if fi.Size()%leafSize != 0 {
		return 0, fmt.Errorf("merkle: %s is not a multiple of %d bytes", path, leafSize)
	}
	return fi.Size() / leafSize, nil
}

// reduce streams src 2 leaves at a time and writes SHA-256(nodeDomain || L || R).
// Пара сортируется по байтам (canonical Merkle): родитель не зависит от
// позиционного порядка, поэтому офлайн-верификация пруфа возможна без
// знания индекса листа. Нечётный хвост уровня продвигается как есть.
func reduce(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	w := bufio.NewWriterSize(out, 1<<20)
	defer func() { _ = out.Close() }()

	r := bufio.NewReaderSize(in, 1<<20)
	var l, rr [32]byte
	for {
		_, err = io.ReadFull(r, l[:])
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return err
		}
		if _, err = io.ReadFull(r, rr[:]); err != nil {
			if errors.Is(err, io.EOF) {
				// нечётный хвост уровня продвигается как есть
				if _, err = w.Write(l[:]); err != nil {
					return err
				}
				break
			}
			return err
		}
		a, b := l, rr
		if !less(a, b) {
			a, b = b, a
		}
		h := sha256.Sum256(append(append([]byte(nodeDomain), a[:]...), b[:]...))
		if _, err = w.Write(h[:]); err != nil {
			return err
		}
	}
	return w.Flush()
}

func readLeaf(path string, i int64) ([32]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return [32]byte{}, err
	}
	defer f.Close()
	var leaf [32]byte
	if _, err = f.Seek(i*leafSize, io.SeekStart); err != nil {
		return leaf, err
	}
	if _, err = io.ReadFull(f, leaf[:]); err != nil {
		return leaf, err
	}
	return leaf, nil
}

// Tree is an in-memory Merkle tree used by the mock chain (test batches only;
// production relies on the streaming Root and proofs anchored on-chain).
type Tree struct {
	levels [][][32]byte
	index  map[[32]byte]int64
}

// NewTree builds an in-memory tree from leaf hashes.
func NewTree(leaves [][32]byte) *Tree {
	levels := make([][][32]byte, 0, 20)
	levels = append(levels, leaves)
	for {
		cur := levels[len(levels)-1]
		if len(cur) <= 1 {
			break
		}
		var next [][32]byte
		for i := 0; i < len(cur); i += 2 {
			if i+1 == len(cur) { // odd tail promoted as-is
				next = append(next, cur[i])
			} else {
				a, b := cur[i], cur[i+1]
				if !less(a, b) {
					a, b = b, a
				}
				h := sha256.Sum256(append(append([]byte(nodeDomain), a[:]...), b[:]...))
				next = append(next, h)
			}
		}
		levels = append(levels, next)
	}
	t := &Tree{levels: levels, index: make(map[[32]byte]int64, len(leaves))}
	for i, l := range leaves {
		t.index[l] = int64(i)
	}
	return t
}

// Root returns the tree root.
func (t *Tree) Root() [32]byte { return t.levels[len(t.levels)-1][0] }

// Proof returns sibling hashes from leaf at leafHash up to the root.
func (t *Tree) Proof(leafHash [32]byte) ([][32]byte, bool) {
	i, ok := t.index[leafHash]
	if !ok {
		return nil, false
	}
	var proof [][32]byte
	for lvl := 0; lvl < len(t.levels)-1; lvl++ {
		cur := t.levels[lvl]
		if i%2 == 1 {
			proof = append(proof, cur[i-1])
		} else if i+1 < int64(len(cur)) {
			proof = append(proof, cur[i+1])
		} // odd tail has no sibling
		i /= 2
	}
	return proof, true
}

// VerifyProof checks leafHash against root with the given sibling path.
// Родитель — canonical: пара сортируется по байтам, поэтому направление
// (лево/право) не требуется.
func VerifyProof(leafHash, root [32]byte, proof [][32]byte) bool {
	h := leafHash
	for _, sib := range proof {
		a, b := h, sib
		if !less(a, b) {
			a, b = b, a
		}
		h = sha256.Sum256(append(append([]byte(nodeDomain), a[:]...), b[:]...))
	}
	return h == root
}

// less orders two hashes (canonical parent pairing).
func less(a, b [32]byte) bool {
	return binary.BigEndian.Uint64(a[:8]) < binary.BigEndian.Uint64(b[:8])
}

// RootHex returns hex of the root.
func RootHex(r [32]byte) string { return hex.EncodeToString(r[:]) }
