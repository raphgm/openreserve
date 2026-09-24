// Package mnemonic encodes 32-byte key seeds as 24 BIP39 English words and
// back, so people can write their recovery key down by hand. The seed is
// used directly as BIP39 entropy (no passphrase or HD derivation), matching
// the ORPay web wallet.
package mnemonic

import (
	"crypto/sha256"
	_ "embed"
	"errors"
	"fmt"
	"math/big"
	"strings"
)

//go:embed english.txt
var raw string

var (
	words = strings.Split(raw, "\n")
	index = func() map[string]int {
		m := make(map[string]int, len(words))
		for i, w := range words {
			m[w] = i
		}
		return m
	}()
)

// Encode turns a 32-byte seed into 24 words.
func Encode(seed []byte) (string, error) {
	if len(seed) != 32 {
		return "", errors.New("seed must be 32 bytes")
	}
	// 256 bits of entropy followed by an 8-bit checksum = 24 x 11 bits.
	sum := sha256.Sum256(seed)
	n := new(big.Int).SetBytes(append(append([]byte{}, seed...), sum[0]))
	out := make([]string, 24)
	mask := big.NewInt(2047)
	for i := 23; i >= 0; i-- {
		out[i] = words[new(big.Int).And(n, mask).Int64()]
		n.Rsh(n, 11)
	}
	return strings.Join(out, " "), nil
}

// Decode turns 24 words back into the seed, verifying the checksum so a
// mistyped word is caught instead of silently producing another wallet.
func Decode(phrase string) ([]byte, error) {
	ws := strings.Fields(strings.ToLower(phrase))
	if len(ws) != 24 {
		return nil, fmt.Errorf("expected 24 words, got %d", len(ws))
	}
	n := new(big.Int)
	for i, w := range ws {
		v, ok := index[w]
		if !ok {
			return nil, fmt.Errorf("word %d (%q) is not in the word list", i+1, w)
		}
		n.Lsh(n, 11).Or(n, big.NewInt(int64(v)))
	}
	buf := n.FillBytes(make([]byte, 33))
	seed, check := buf[:32], buf[32]
	if sum := sha256.Sum256(seed); sum[0] != check {
		return nil, errors.New("checksum mismatch: check the words and their order")
	}
	return seed, nil
}
