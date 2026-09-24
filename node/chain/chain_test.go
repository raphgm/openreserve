package chain

import (
	"crypto/ed25519"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/openreserve/node/ledger"
	"github.com/openreserve/node/types"
)

type actor struct {
	priv ed25519.PrivateKey
	addr types.Address
}

func newActor(t *testing.T) actor {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	return actor{priv, types.AddressFromPubKey(pub)}
}

const minFee = 1000

type fixture struct {
	t                    *testing.T
	g                    *Genesis
	proposer, alice, bob actor
	now                  int64
}

func newFixture(t *testing.T) *fixture {
	f := &fixture{t: t, proposer: newActor(t), alice: newActor(t), bob: newActor(t), now: 1_000}
	f.g = &Genesis{
		ChainID: "test-1", Time: 1, Proposer: f.proposer.addr, MinFee: minFee,
		Allocations: []Allocation{{Address: f.alice.addr, Amount: 100 * types.Unit}},
	}
	if err := f.g.Validate(); err != nil {
		t.Fatal(err)
	}
	return f
}

func (f *fixture) open(dir string) *Chain {
	f.t.Helper()
	c, err := Open(dir, f.g)
	if err != nil {
		f.t.Fatal(err)
	}
	f.t.Cleanup(func() { c.Close() })
	return c
}

func (f *fixture) tx(from, to actor, amount types.Amount, nonce uint64) *types.Tx {
	tx := &types.Tx{ChainID: f.g.ChainID, From: from.addr, To: to.addr, Amount: amount, Fee: minFee, Nonce: nonce}
	tx.Sign(from.priv)
	return tx
}

func (f *fixture) produce(c *Chain) *types.Block {
	f.t.Helper()
	f.now += 1000
	b, err := c.Produce(f.proposer.priv, f.now)
	if err != nil {
		f.t.Fatal(err)
	}
	return b
}

func mustSubmit(t *testing.T, c *Chain, tx *types.Tx) {
	t.Helper()
	if _, err := c.Submit(tx); err != nil {
		t.Fatalf("submit: %v", err)
	}
}

func TestTransferAndSupply(t *testing.T) {
	f := newFixture(t)
	c := f.open(t.TempDir())

	mustSubmit(t, c, f.tx(f.alice, f.bob, 10*types.Unit, 0))
	b := f.produce(c)
	if b == nil || len(b.Txs) != 1 || b.Header.Height != 1 {
		t.Fatalf("unexpected block %+v", b)
	}
	alice, _ := c.Account(f.alice.addr)
	bob, _ := c.Account(f.bob.addr)
	if alice.Balance != 90*types.Unit-minFee || alice.Nonce != 1 {
		t.Errorf("alice = %+v", alice)
	}
	if bob.Balance != 10*types.Unit {
		t.Errorf("bob = %+v", bob)
	}
	st := c.Status()
	if st.Supply != 100*types.Unit-minFee || st.Burned != minFee {
		t.Errorf("supply %d burned %d", st.Supply, st.Burned)
	}
	if st.Supply != alice.Balance+bob.Balance {
		t.Error("supply does not equal sum of balances")
	}
	if f.produce(c) != nil {
		t.Error("empty mempool produced a block")
	}
}

func TestRejections(t *testing.T) {
	f := newFixture(t)
	c := f.open(t.TempDir())

	cases := map[string]struct {
		tx   *types.Tx
		want error
	}{
		"overdraft": {f.tx(f.alice, f.bob, 101*types.Unit, 0), ledger.ErrInsufficient},
		"unfunded":  {f.tx(f.bob, f.alice, 1, 0), ledger.ErrInsufficient},
		"too ahead": {f.tx(f.alice, f.bob, 1, MaxNonceAhead), ledger.ErrNonceTooHigh},
	}
	lowFee := f.tx(f.alice, f.bob, 1, 0)
	lowFee.Fee = minFee - 1
	lowFee.Sign(f.alice.priv)
	cases["low fee"] = struct {
		tx   *types.Tx
		want error
	}{lowFee, ledger.ErrFeeTooLow}

	for name, tc := range cases {
		if _, err := c.Submit(tc.tx); !errors.Is(err, tc.want) {
			t.Errorf("%s: got %v, want %v", name, err, tc.want)
		}
	}

	tx := f.tx(f.alice, f.bob, 1, 0)
	mustSubmit(t, c, tx)
	if _, err := c.Submit(tx); !errors.Is(err, ErrDuplicate) {
		t.Errorf("duplicate: %v", err)
	}
	if _, err := c.Submit(f.tx(f.alice, f.bob, 2, 0)); err == nil {
		t.Error("second tx with same nonce accepted")
	}
	f.produce(c)
	if _, err := c.Submit(tx); !errors.Is(err, ErrDuplicate) {
		t.Errorf("replay of committed tx: %v", err)
	}
}

func TestPendingFundsLimit(t *testing.T) {
	f := newFixture(t)
	c := f.open(t.TempDir())
	mustSubmit(t, c, f.tx(f.alice, f.bob, 60*types.Unit, 0))
	// Alone this is affordable, but not on top of the pending 60 ORP.
	if _, err := c.Submit(f.tx(f.alice, f.bob, 50*types.Unit, 1)); !errors.Is(err, ledger.ErrInsufficient) {
		t.Errorf("got %v, want insufficient funds", err)
	}
}

func TestOutOfOrderNonces(t *testing.T) {
	f := newFixture(t)
	c := f.open(t.TempDir())
	for _, n := range []uint64{2, 0, 1} {
		mustSubmit(t, c, f.tx(f.alice, f.bob, types.Unit, n))
	}
	if _, next := c.Account(f.alice.addr); next != 3 {
		t.Errorf("next nonce = %d, want 3", next)
	}
	b := f.produce(c)
	if len(b.Txs) != 3 {
		t.Fatalf("block has %d txs, want 3", len(b.Txs))
	}
	for i, tx := range b.Txs {
		if tx.Nonce != uint64(i) {
			t.Errorf("tx %d has nonce %d", i, tx.Nonce)
		}
	}
}

func TestRestartReplaysState(t *testing.T) {
	f := newFixture(t)
	dir := t.TempDir()
	c := f.open(dir)
	for i := range 3 {
		mustSubmit(t, c, f.tx(f.alice, f.bob, types.Unit, uint64(i)))
		f.produce(c)
	}
	want := c.Status()
	c.Close()

	c2 := f.open(dir)
	got := c2.Status()
	if got.Height != 3 || got.StateRoot != want.StateRoot || got.TipHash != want.TipHash {
		t.Fatalf("after restart %+v, want %+v", got, want)
	}
	mustSubmit(t, c2, f.tx(f.alice, f.bob, types.Unit, 3))
	if b := f.produce(c2); b.Header.Height != 4 {
		t.Errorf("height %d after restart", b.Header.Height)
	}
}

func TestTornWriteRecovered(t *testing.T) {
	f := newFixture(t)
	dir := t.TempDir()
	c := f.open(dir)
	mustSubmit(t, c, f.tx(f.alice, f.bob, types.Unit, 0))
	f.produce(c)
	c.Close()

	fh, _ := os.OpenFile(filepath.Join(dir, blockLogName), os.O_APPEND|os.O_WRONLY, 0)
	fh.WriteString(`{"header":{"chain_id":"test-1","hei`)
	fh.Close()

	c2 := f.open(dir)
	if c2.Height() != 1 {
		t.Fatalf("height %d", c2.Height())
	}
	mustSubmit(t, c2, f.tx(f.alice, f.bob, types.Unit, 1))
	f.produce(c2)
	c2.Close()
	if c3 := f.open(dir); c3.Height() != 2 {
		t.Fatalf("height %d after recovery and append", c3.Height())
	}
}

func TestReplicaVerifiesBlocks(t *testing.T) {
	f := newFixture(t)
	producer := f.open(t.TempDir())
	replica := f.open(t.TempDir())

	mustSubmit(t, producer, f.tx(f.alice, f.bob, 5*types.Unit, 0))
	good := f.produce(producer)

	clone := func() *types.Block {
		b := *good
		b.Txs = append([]*types.Tx(nil), good.Txs...)
		return &b
	}
	bad := map[string]*types.Block{}

	b := clone()
	b.Header.StateRoot[0] ^= 1
	b.Sign(f.proposer.priv)
	bad["wrong state root"] = b

	b = clone()
	b.Header.Height = 2
	b.Sign(f.proposer.priv)
	bad["height gap"] = b

	b = clone()
	b.Sign(newActor(t).priv)
	bad["bad signature"] = b

	rogue := newActor(t)
	b = clone()
	b.Header.Proposer = rogue.addr
	b.Sign(rogue.priv)
	bad["unauthorized proposer"] = b

	b = clone()
	forged := *good.Txs[0]
	forged.Amount = 50 * types.Unit
	b.Txs = []*types.Tx{&forged}
	b.Header.TxRoot = types.TxRoot(b.Txs)
	b.Sign(f.proposer.priv)
	bad["tampered tx"] = b

	for name, blk := range bad {
		if err := replica.AddBlock(blk); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	if err := replica.AddBlock(good); err != nil {
		t.Fatalf("good block rejected: %v", err)
	}
	if replica.Status().StateRoot != producer.Status().StateRoot {
		t.Error("replica state diverged")
	}
	if err := replica.AddBlock(good); err == nil {
		t.Error("same block accepted twice")
	}
}

func TestGenesisValidation(t *testing.T) {
	a := newActor(t)
	bad := []*Genesis{
		{ChainID: "", Proposer: a.addr},
		{ChainID: "x", Proposer: "nothex"},
		{ChainID: "x", Proposer: a.addr, Allocations: []Allocation{{a.addr, 1}, {a.addr, 2}}},
		{ChainID: "x", Proposer: a.addr, Allocations: []Allocation{{a.addr, ^uint64(0)}, {newActor(t).addr, 1}}},
	}
	for i, g := range bad {
		if g.Validate() == nil {
			t.Errorf("genesis %d accepted", i)
		}
	}
}
