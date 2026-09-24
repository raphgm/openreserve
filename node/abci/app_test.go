package abci

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"testing"
	"time"

	abcitypes "github.com/cometbft/cometbft/abci/types"

	"github.com/openreserve/node/chain"
	"github.com/openreserve/node/types"
)

func setup(t *testing.T) (*Application, *chain.Genesis, ed25519.PrivateKey, string) {
	_, alice, _ := ed25519.GenerateKey(nil)
	g := &chain.Genesis{ChainID: "bft", Time: 1, Consensus: chain.ConsensusCometBFT, MinFee: 1000,
		Allocations: []chain.Allocation{{Address: types.AddressFromPubKey(alice.Public().(ed25519.PublicKey)), Amount: 100 * types.Unit}}}
	if err := g.Validate(); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	c, err := chain.Open(dir, g)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	return New(c), g, alice, dir
}

func rawTx(t *testing.T, key ed25519.PrivateKey, to types.Address, amt types.Amount, nonce uint64) []byte {
	tx := &types.Tx{ChainID: "bft", From: types.AddressFromPubKey(key.Public().(ed25519.PublicKey)), To: to, Amount: amt, Fee: 1000, Nonce: nonce}
	tx.Sign(key)
	b, _ := json.Marshal(tx)
	return b
}

func finalize(t *testing.T, a *Application, h int64, txs ...[]byte) *abcitypes.ResponseFinalizeBlock {
	t.Helper()
	res, err := a.FinalizeBlock(context.Background(), &abcitypes.RequestFinalizeBlock{Height: h, Time: time.UnixMilli(1000 * h), Txs: txs, ProposerAddress: []byte{1, 2}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.Commit(context.Background(), &abcitypes.RequestCommit{}); err != nil {
		t.Fatal(err)
	}
	return res
}

func TestDecidedBlocksSkipBadTxsDeterministically(t *testing.T) {
	a, _, alice, _ := setup(t)
	_, bobKey, _ := ed25519.GenerateKey(nil)
	bob := types.AddressFromPubKey(bobKey.Public().(ed25519.PublicKey))

	// CheckTx admits a valid tx and a future nonce, refuses garbage and overdrafts.
	check := func(b []byte) uint32 {
		r, _ := a.CheckTx(context.Background(), &abcitypes.RequestCheckTx{Tx: b})
		return r.Code
	}
	if check(rawTx(t, alice, bob, types.Unit, 0)) != codeOK || check(rawTx(t, alice, bob, types.Unit, 1)) != codeOK {
		t.Fatal("valid txs refused by CheckTx")
	}
	if check([]byte("not json")) == codeOK || check(rawTx(t, alice, bob, 1000*types.Unit, 0)) == codeOK {
		t.Fatal("bad txs admitted by CheckTx")
	}

	// A block mixing good, duplicate-nonce, overdraft and garbage txs: only
	// the good ones apply, and every node gets the same results.
	good := rawTx(t, alice, bob, 10*types.Unit, 0)
	res := finalize(t, a, 1, good, rawTx(t, alice, bob, 3*types.Unit, 0), rawTx(t, alice, bob, 500*types.Unit, 1), []byte("{bad"))
	codes := []uint32{}
	for _, r := range res.TxResults {
		codes = append(codes, r.Code)
	}
	if codes[0] != codeOK || codes[1] == codeOK || codes[2] == codeOK || codes[3] != codeBadTx {
		t.Fatalf("result codes %v", codes)
	}
	acc, _ := a.c.Account(bob)
	if acc.Balance != 10*types.Unit {
		t.Fatalf("bob %d", acc.Balance)
	}
	// App hash is the committed state root, which validators agree on.
	root := a.c.StateRoot()
	if string(res.AppHash) != string(root[:]) {
		t.Fatal("app hash is not the state root")
	}

	// Empty blocks keep heights aligned with CometBFT.
	finalize(t, a, 2)
	info, _ := a.Info(context.Background(), &abcitypes.RequestInfo{})
	if info.LastBlockHeight != 2 || string(info.LastBlockAppHash) != string(root[:]) {
		t.Fatalf("info after empty block: %+v", info)
	}
}

func TestRestartReportsCommittedHeight(t *testing.T) {
	a, g, alice, dir := setup(t)
	_, bobKey, _ := ed25519.GenerateKey(nil)
	bob := types.AddressFromPubKey(bobKey.Public().(ed25519.PublicKey))
	finalize(t, a, 1, rawTx(t, alice, bob, types.Unit, 0))
	finalize(t, a, 2)
	want := a.c.StateRoot()
	a.c.Close()

	// Reopen from disk: the block log replays (BFT blocks carry no proposer
	// signature) and Info tells CometBFT to resume after height 2.
	c, err := chain.Open(dir, g)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	info, _ := New(c).Info(context.Background(), &abcitypes.RequestInfo{})
	if info.LastBlockHeight != 2 || string(info.LastBlockAppHash) != string(want[:]) {
		t.Fatalf("after restart: %+v", info)
	}
}
