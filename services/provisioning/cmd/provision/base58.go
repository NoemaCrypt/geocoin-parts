package main

import (
	"math/big"
	"strings"
)

// base58Alphabet — Bitcoin-алфавит: исключает 0/O/I/l (требование пилота:
// ручной ввод кода мастером без похожих символов).
const base58Alphabet = "123456789ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz"

// encodeClaimCode кодирует 128-битный секрет как Base58, дополняет до 24
// символов ('1' слева, Bitcoin-style) и группирует дефисами по 6:
// XXXXXX-XXXXXX-XXXXXX-XXXXXX.
func encodeClaimCode(secret [16]byte) string {
	x := new(big.Int).SetBytes(secret[:])
	base := big.NewInt(58)
	mod := new(big.Int)
	var out []byte
	for x.Sign() > 0 {
		x.DivMod(x, base, mod)
		out = append(out, base58Alphabet[mod.Int64()])
	}
	// ведущие нулевые байты кодируются '1'
	for _, b := range secret {
		if b != 0 {
			break
		}
		out = append(out, '1')
	}
	for len(out) < 24 {
		out = append(out, '1')
	}
	// реверс: младшие разряды собирались первыми
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return group(string(out), 6)
}

func group(s string, size int) string {
	var parts []string
	for i := 0; i < len(s); i += size {
		end := i + size
		if end > len(s) {
			end = len(s)
		}
		parts = append(parts, s[i:end])
	}
	return strings.Join(parts, "-")
}
