package types

import (
	"encoding/hex"
	"testing"
)

// Signing vectors shared with the web wallet (apps/orpay/frontend/test).
// If either side changes its encoding, both tests fail: wallets and nodes
// must agree byte for byte or every signature breaks.
const (
	vecFrom = "1111111111111111111111111111111111111111111111111111111111111111"
	vecTo   = "2222222222222222222222222222222222222222222222222222222222222222"
)

func SigningVectors() map[string]*Tx {
	id := Hash{0xab}
	return map[string]*Tx{
		"transfer":  {ChainID: "c1", From: vecFrom, To: vecTo, Amount: 1_500_000, Fee: 1000, Nonce: 7, Memo: "lunch ✓"},
		"ngn":       {ChainID: "c1", From: vecFrom, To: vecTo, Amount: 50_000_000, Fee: 10_000, Nonce: 1, Asset: "NGN"},
		"burn":      {ChainID: "c1", From: vecFrom, Amount: 20_000_000, Fee: 10_000, Nonce: 2, Kind: KindBurn, Asset: "NGN", Memo: "wd:abc"},
		"pool":      {ChainID: "c1", From: vecFrom, Fee: 1000, Nonce: 3, Pool: &PoolOp{Op: PoolCreate, Name: "Ajo", Members: []Address{vecFrom, vecTo}, Contribution: 10_000_000}},
		"poolsched": {ChainID: "c1", From: vecFrom, Fee: 1000, Nonce: 6, Asset: "NGN", Pool: &PoolOp{Op: PoolCreate, Name: "Ajo", Members: []Address{vecFrom, vecTo}, Contribution: 10_000_000, RoundSecs: 604800, Deposit: 10_000_000}},
		"pooljoin":  {ChainID: "c1", From: vecTo, Nonce: 0, Fee: 1000, Pool: &PoolOp{Op: PoolJoin, ID: id}},
		"escrow": {ChainID: "c1", From: vecFrom, Fee: 1000, Nonce: 4, Asset: "NGN", Escrow: &EscrowOp{
			Op: EscrowCreate, Seller: vecTo, Arbiter: "3333333333333333333333333333333333333333333333333333333333333333",
			Milestones: []Amount{40_000_000, 60_000_000}, ShipBy: 1_800_000_000_000, ReviewSecs: 259200, Ref: "job-17"}},
		"resolve": {ChainID: "c1", From: vecTo, Nonce: 5, Escrow: &EscrowOp{Op: EscrowResolve, ID: id, ToSeller: 5_000_000}},
	}
}

var vectorHashes = map[string]string{
	"transfer":  "5efb3fa58edd543e91f41e4d61c7355aa1cf90f6beb0478432ed538ad69d2bc5",
	"ngn":       "b3a15dfb5df2c960d3493acb5a7073761280bb0966322447e2da225697b78a6e",
	"burn":      "8ca4d14556866aa578420a5db2deabd0c3392543e4ef00a9370f88488cc40da7",
	"pool":      "36f569c0bb0fc3e642bcc2b5a0885c3282d5f35a4b188292c93a53c3df191e0c",
	"poolsched": "f115f625edf0067d576e3bb8336fcb077161ef8bd55341284b20a620edb507c2",
	"pooljoin":  "82ebaedcce1ec1bbfc48bf170294f7a95c02913657098321586997bc922aedf7",
	"escrow":    "75e8db01e8d1f033d182d1488505431e3f35f6bc858e5c0e722e10d13d7dfbb7",
	"resolve":   "690b1de96de2bb17c07bd8dda70bd9b7d40180218126757ab81b661672b3b25d",
}

func TestSigningVectorsStable(t *testing.T) {
	for name, tx := range SigningVectors() {
		id := tx.ID()
		got := hex.EncodeToString(id[:])
		if want := vectorHashes[name]; got != want {
			t.Errorf("%s: sign bytes changed: id %s, pinned %s", name, got, want)
		}
		t.Logf("%s %s", name, got)
	}
}
