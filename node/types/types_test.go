package types

import (
	"crypto/ed25519"
	"testing"
)

func TestParseFormatAmount(t *testing.T) {
	good := map[string]Amount{
		"0": 0, "1": Unit, "1.5": 1_500_000, "0.000001": 1, "12.345678": 12_345_678, "007": 7 * Unit,
	}
	for in, want := range good {
		got, err := ParseAmount(in)
		if err != nil || got != want {
			t.Errorf("ParseAmount(%q) = %d, %v; want %d", in, got, err, want)
		}
	}
	for _, in := range []string{"", ".", ".5", "1.", "1.2345678", "-1", "1e5", "abc", "1.2.3", " 1", "18446744073710"} {
		if _, err := ParseAmount(in); err == nil {
			t.Errorf("ParseAmount(%q) succeeded, want error", in)
		}
	}
	for in, want := range map[Amount]string{0: "0", 1: "0.000001", Unit: "1", 1_500_000: "1.5", 100 * Unit: "100"} {
		if got := FormatAmount(in); got != want {
			t.Errorf("FormatAmount(%d) = %q, want %q", in, got, want)
		}
	}
}

func newKey(t *testing.T) (ed25519.PrivateKey, Address) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	return priv, AddressFromPubKey(pub)
}

func TestTxSignature(t *testing.T) {
	priv, from := newKey(t)
	_, to := newKey(t)
	tx := &Tx{ChainID: "test", From: from, To: to, Amount: 5, Fee: 1, Nonce: 0}
	tx.Sign(priv)
	if err := tx.CheckStateless("test"); err != nil {
		t.Fatalf("valid tx rejected: %v", err)
	}
	if err := tx.CheckStateless("other"); err == nil {
		t.Error("tx accepted on wrong chain")
	}

	tampered := *tx
	tampered.Amount = 500
	if err := tampered.CheckStateless("test"); err == nil {
		t.Error("tampered amount accepted")
	}

	otherPriv, _ := newKey(t)
	forged := *tx
	forged.Sign(otherPriv)
	if err := forged.CheckStateless("test"); err == nil {
		t.Error("tx signed by wrong key accepted")
	}

	self := *tx
	self.To = from
	self.Sign(priv)
	if err := self.CheckStateless("test"); err == nil {
		t.Error("self-transfer accepted")
	}

	overflow := *tx
	overflow.Amount, overflow.Fee = ^uint64(0), 1
	overflow.Sign(priv)
	if err := overflow.CheckStateless("test"); err == nil {
		t.Error("overflowing amount+fee accepted")
	}
}

func TestSignBytesUnambiguous(t *testing.T) {
	// Length prefixes must keep adjacent string fields from blending.
	a := Tx{ChainID: "ab", Memo: "c"}
	b := Tx{ChainID: "a", Memo: "bc"}
	if a.ID() == b.ID() {
		t.Error("distinct txs share an ID")
	}
}

func TestTxRoot(t *testing.T) {
	priv, from := newKey(t)
	_, to := newKey(t)
	var txs []*Tx
	for i := range 5 {
		tx := &Tx{ChainID: "t", From: from, To: to, Amount: 1, Nonce: uint64(i)}
		tx.Sign(priv)
		txs = append(txs, tx)
	}
	r := TxRoot(txs)
	if r == (Hash{}) {
		t.Fatal("empty root")
	}
	if TxRoot(txs[:4]) == r || TxRoot([]*Tx{txs[1], txs[0], txs[2], txs[3], txs[4]}) == r {
		t.Error("root does not commit to set and order")
	}
}
