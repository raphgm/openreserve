package mnemonic

import (
	"bytes"
	"crypto/rand"
	"strings"
	"testing"
)

// Vector produced by @scure/bip39 (the library the web wallet uses).
const vector = "abandon debris luxury debate crawl blush thought trip essence people sudden spray alter satisfy bike mystery one ask cloud hope sword grass enter certain"

func seed() []byte {
	b := make([]byte, 32)
	for i := range b {
		b[i] = byte(i * 7)
	}
	return b
}

func TestMatchesWebWallet(t *testing.T) {
	if len(words) != 2048 {
		t.Fatalf("word list has %d words", len(words))
	}
	got, err := Encode(seed())
	if err != nil || got != vector {
		t.Fatalf("Encode = %q, %v", got, err)
	}
	back, err := Decode(strings.ToUpper(vector))
	if err != nil || !bytes.Equal(back, seed()) {
		t.Fatalf("Decode = %x, %v", back, err)
	}
}

func TestRoundTripAndErrors(t *testing.T) {
	for range 50 {
		s := make([]byte, 32)
		rand.Read(s)
		p, _ := Encode(s)
		back, err := Decode(p)
		if err != nil || !bytes.Equal(back, s) {
			t.Fatalf("round trip failed for %x: %v", s, err)
		}
	}
	ws := strings.Fields(vector)
	ws[0], ws[1] = ws[1], ws[0]
	if _, err := Decode(strings.Join(ws, " ")); err == nil {
		t.Error("swapped words accepted")
	}
	if _, err := Decode("abandon abandon"); err == nil {
		t.Error("short phrase accepted")
	}
	if _, err := Decode(strings.Replace(vector, "abandon", "abandonx", 1)); err == nil {
		t.Error("unknown word accepted")
	}
}
