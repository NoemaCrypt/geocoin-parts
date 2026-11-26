package merkle

import (
	"crypto/rand"
	"crypto/sha256"
	"os"
	"path/filepath"
	"testing"
)

func TestStreamingRootMatchesInMemoryTree(t *testing.T) {
	for _, n := range []int{1, 2, 3, 4, 5, 15, 16, 17, 31, 33, 125, 997, 1000} {
		n := n
		t.Run(itoa(n), func(t *testing.T) {
			leaves := make([][32]byte, n)
			for i := range leaves {
				b := sha256.Sum256([]byte(itoa(i)))
				leaves[i] = b
			}
			dir := t.TempDir()
			path := filepath.Join(dir, "leaves.bin")
			var raw []byte
			for _, l := range leaves {
				raw = append(raw, l[:]...)
			}
			if err := os.WriteFile(path, raw, 0o600); err != nil {
				t.Fatal(err)
			}
			root, err := Root(path, dir)
			if err != nil {
				t.Fatalf("n=%d: %v", n, err)
			}
			inmem := NewTree(leaves)
			if root != inmem.Root() {
				t.Fatalf("n=%d: streaming root != in-memory tree root", n)
			}
			// офлайн-проверка пруфа для каждого листа
			for i, l := range leaves {
				proof, ok := inmem.Proof(l)
				if !ok {
					t.Fatalf("n=%d: leaf %d not found", n, i)
				}
				if !VerifyProof(l, root, proof) {
					t.Fatalf("n=%d: proof for leaf %d rejected", n, i)
				}
			}
			// чужой лист должен быть отклонён
			foreign := sha256.Sum256([]byte("foreign"))
			if VerifyProof(foreign, root, nil) {
				t.Fatal("foreign leaf accepted")
			}
		})
	}
}

func TestLeafDomain(t *testing.T) {
	var u, c [32]byte
	_, _ = rand.Read(u[:])
	_, _ = rand.Read(c[:])
	l1 := Leaf(u, c)
	l2 := Leaf(u, c)
	if l1 != l2 {
		t.Fatal("leaf must be deterministic")
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
