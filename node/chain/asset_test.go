package chain

import (
	"errors"
	"testing"

	"github.com/openreserve/node/ledger"
	"github.com/openreserve/node/types"
)

func assetSum(c *Chain, sym string) types.Amount {
	c.mu.RLock()
	defer c.mu.RUnlock()
	var total types.Amount
	for _, a := range c.state.Accounts {
		total += a.Assets[sym]
	}
	for _, p := range c.state.Pools {
		if p.Asset == sym {
			total += p.Balance
		}
	}
	return total
}

func TestIssuedAsset(t *testing.T) {
	f := newFixture(t)
	issuer := newActor(t)
	const fee = 10_000 // 0.01 NGN
	f.g.Assets = []ledger.AssetDef{{Symbol: "NGN", Name: "Nigerian naira", Issuer: issuer.addr, MinFee: fee, Decimals: 2}}
	c := f.open(t.TempDir())
	nonce := map[types.Address]uint64{}
	sign := func(a actor, tx *types.Tx) *types.Tx {
		tx.ChainID, tx.From, tx.Nonce = f.g.ChainID, a.addr, nonce[a.addr]
		tx.Sign(a.priv)
		return tx
	}
	ok := func(a actor, tx *types.Tx) {
		t.Helper()
		if _, err := c.Submit(sign(a, tx)); err != nil {
			t.Fatalf("submit: %v", err)
		}
		nonce[a.addr]++
		f.produce(c)
	}
	fails := func(a actor, tx *types.Tx, want error) {
		t.Helper()
		_, err := c.Submit(sign(a, tx))
		if err == nil || (want != nil && !errors.Is(err, want)) {
			t.Errorf("got %v, want %v", err, want)
		}
	}
	invariant := func() {
		t.Helper()
		if got, want := assetSum(c, "NGN"), c.Status().Assets[0].Supply; got != want {
			t.Fatalf("NGN balances %d != supply %d", got, want)
		}
	}

	// Only the issuer can mint; mints are free.
	fails(f.alice, &types.Tx{Kind: types.KindMint, Asset: "NGN", To: f.alice.addr, Amount: 1}, ledger.ErrAsset)
	fails(issuer, &types.Tx{Kind: types.KindMint, Asset: "USD", To: f.alice.addr, Amount: 1}, ledger.ErrAsset)
	ok(issuer, &types.Tx{Kind: types.KindMint, Asset: "NGN", To: f.alice.addr, Amount: 5000 * types.Unit, Memo: "paystack:ref1"})
	if b := c.Status().Assets[0].Supply; b != 5000*types.Unit {
		t.Fatalf("supply after mint %d", b)
	}
	invariant()

	// Transfers pay their fee in NGN to the issuer; alice needs no ORP.
	ok(f.alice, &types.Tx{Asset: "NGN", To: f.bob.addr, Amount: 1000 * types.Unit, Fee: fee})
	c.mu.RLock()
	aliceNGN, bobNGN, issuerNGN := c.state.Balance(f.alice.addr, "NGN"), c.state.Balance(f.bob.addr, "NGN"), c.state.Balance(issuer.addr, "NGN")
	c.mu.RUnlock()
	if aliceNGN != 4000*types.Unit-fee || bobNGN != 1000*types.Unit || issuerNGN != fee {
		t.Fatalf("balances alice=%d bob=%d issuer=%d", aliceNGN, bobNGN, issuerNGN)
	}
	invariant()
	fails(f.bob, &types.Tx{Asset: "NGN", To: f.alice.addr, Amount: 1000 * types.Unit, Fee: fee}, ledger.ErrInsufficient)

	// Burning (a withdrawal) destroys units.
	ok(f.bob, &types.Tx{Kind: types.KindBurn, Asset: "NGN", Amount: 500 * types.Unit, Fee: fee, Memo: "wd:abc"})
	if s := c.Status().Assets[0].Supply; s != 4500*types.Unit {
		t.Fatalf("supply after burn %d", s)
	}
	invariant()

	// A naira savings pool: contributions and claims move NGN.
	create := sign(f.alice, &types.Tx{Asset: "NGN", Fee: fee, Pool: &types.PoolOp{
		Op: types.PoolCreate, Name: "Naira ajo", Members: []types.Address{f.alice.addr, f.bob.addr}, Contribution: 100 * types.Unit,
	}})
	if _, err := c.Submit(create); err != nil {
		t.Fatal(err)
	}
	nonce[f.alice.addr]++
	f.produce(c)
	id := create.ID()
	fails(f.bob, &types.Tx{Fee: minFee, Pool: &types.PoolOp{Op: types.PoolJoin, ID: id}}, nil) // wrong asset (bob has no ORP either)
	ok(f.bob, &types.Tx{Asset: "NGN", Fee: fee, Pool: &types.PoolOp{Op: types.PoolJoin, ID: id}})
	ok(f.alice, &types.Tx{Asset: "NGN", Fee: fee, Pool: &types.PoolOp{Op: types.PoolContribute, ID: id}})
	ok(f.bob, &types.Tx{Asset: "NGN", Fee: fee, Pool: &types.PoolOp{Op: types.PoolContribute, ID: id}})
	invariant()
	ok(f.alice, &types.Tx{Asset: "NGN", Fee: fee, Pool: &types.PoolOp{Op: types.PoolClaim, ID: id}})
	if p := c.Pool(id); p.Balance != 0 || p.Round != 1 {
		t.Fatalf("pool after claim %+v", p)
	}
	invariant()

	// ORP accounting is untouched by NGN activity.
	if st := c.Status(); st.Supply != 100*types.Unit {
		t.Errorf("ORP supply changed: %d", st.Supply)
	}
}

func TestAssetStatelessRules(t *testing.T) {
	f := newFixture(t)
	bad := map[string]*types.Tx{
		"mint ORP":       {Kind: types.KindMint, To: f.bob.addr, Amount: 1},
		"mint with fee":  {Kind: types.KindMint, Asset: "NGN", To: f.bob.addr, Amount: 1, Fee: 1},
		"burn with to":   {Kind: types.KindBurn, Asset: "NGN", To: f.bob.addr, Amount: 1},
		"lowercase sym":  {Asset: "ngn", To: f.bob.addr, Amount: 1},
		"explicit ORP":   {Asset: "ORP", To: f.bob.addr, Amount: 1},
		"unknown kind":   {Kind: "steal", Asset: "NGN", To: f.bob.addr, Amount: 1},
		"pool with kind": {Kind: types.KindMint, Asset: "NGN", Pool: &types.PoolOp{Op: types.PoolJoin, ID: types.Hash{1}}},
	}
	for name, tx := range bad {
		tx.ChainID, tx.From = f.g.ChainID, f.alice.addr
		tx.Sign(f.alice.priv)
		if tx.CheckStateless(f.g.ChainID) == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	// v1 encoding is unchanged for plain ORP transfers.
	plain := &types.Tx{ChainID: "c", From: f.alice.addr, To: f.bob.addr, Amount: 1}
	if string(plain.SignBytes()[4:21]) != "openreserve/tx/v1" {
		t.Error("plain transfer no longer uses v1 encoding")
	}
}
