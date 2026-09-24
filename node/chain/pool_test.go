package chain

import (
	"errors"
	"testing"

	"github.com/openreserve/node/ledger"
	"github.com/openreserve/node/types"
)

func (f *fixture) poolTx(from actor, nonce uint64, op *types.PoolOp) *types.Tx {
	tx := &types.Tx{ChainID: f.g.ChainID, From: from.addr, Fee: minFee, Nonce: nonce, Pool: op}
	tx.Sign(from.priv)
	return tx
}

func sumBalances(c *Chain) types.Amount {
	var total types.Amount
	c.mu.RLock()
	defer c.mu.RUnlock()
	for _, a := range c.state.Accounts {
		total += a.Balance
	}
	for _, p := range c.state.Pools {
		total += p.Balance
	}
	return total
}

func TestPoolLifecycle(t *testing.T) {
	f := newFixture(t)
	carol := newActor(t)
	f.g.Allocations = []Allocation{
		{Address: f.alice.addr, Amount: 100 * types.Unit},
		{Address: f.bob.addr, Amount: 100 * types.Unit},
		{Address: carol.addr, Amount: 100 * types.Unit},
	}
	dir := t.TempDir()
	c := f.open(dir)
	nonce := map[types.Address]uint64{}
	next := func(a actor) uint64 { n := nonce[a.addr]; nonce[a.addr]++; return n }
	submit := func(tx *types.Tx) error { _, err := c.Submit(tx); return err }
	must := func(tx *types.Tx) {
		t.Helper()
		if err := submit(tx); err != nil {
			t.Fatalf("submit: %v", err)
		}
		f.produce(c)
	}

	contribution := 10 * types.Unit
	create := f.poolTx(f.alice, next(f.alice), &types.PoolOp{
		Op: types.PoolCreate, Name: "Friday ajo",
		Members: []types.Address{f.alice.addr, f.bob.addr, carol.addr}, Contribution: contribution,
	})
	must(create)
	id := create.ID()
	if p := c.Pool(id); p == nil || p.Status != ledger.PoolForming {
		t.Fatalf("pool after create: %+v", p)
	}

	// Contributing before everyone joins is refused.
	if err := submit(f.poolTx(f.alice, nonce[f.alice.addr], &types.PoolOp{Op: types.PoolContribute, ID: id})); !errors.Is(err, ledger.ErrPool) {
		t.Errorf("contribute while forming: %v", err)
	}
	outsider := newActor(t)
	if err := submit(f.poolTx(outsider, 0, &types.PoolOp{Op: types.PoolJoin, ID: id})); err == nil {
		t.Error("outsider joined")
	}

	must(f.poolTx(f.bob, next(f.bob), &types.PoolOp{Op: types.PoolJoin, ID: id}))
	must(f.poolTx(carol, next(carol), &types.PoolOp{Op: types.PoolJoin, ID: id}))
	if p := c.Pool(id); p.Status != ledger.PoolActive || p.Round != 0 {
		t.Fatalf("pool after joins: %+v", p)
	}

	members := []actor{f.alice, f.bob, carol}
	for round, recipient := range members {
		// The recipient cannot claim before everyone has paid.
		for i, m := range members {
			if i == 1 {
				err := submit(f.poolTx(recipient, nonce[recipient.addr], &types.PoolOp{Op: types.PoolClaim, ID: id}))
				if !errors.Is(err, ledger.ErrPool) {
					t.Fatalf("round %d early claim: %v", round, err)
				}
			}
			must(f.poolTx(m, next(m), &types.PoolOp{Op: types.PoolContribute, ID: id}))
		}
		p := c.Pool(id)
		if p.Balance != 3*contribution {
			t.Fatalf("round %d pool balance %d", round, p.Balance)
		}
		// Double contribution and claiming out of turn are refused.
		if err := submit(f.poolTx(f.alice, nonce[f.alice.addr], &types.PoolOp{Op: types.PoolContribute, ID: id})); !errors.Is(err, ledger.ErrPool) {
			t.Errorf("round %d double contribution: %v", round, err)
		}
		wrong := members[(round+1)%3]
		if err := submit(f.poolTx(wrong, nonce[wrong.addr], &types.PoolOp{Op: types.PoolClaim, ID: id})); !errors.Is(err, ledger.ErrPool) {
			t.Errorf("round %d out-of-turn claim: %v", round, err)
		}
		before, _ := c.Account(recipient.addr)
		must(f.poolTx(recipient, next(recipient), &types.PoolOp{Op: types.PoolClaim, ID: id}))
		after, _ := c.Account(recipient.addr)
		if after.Balance != before.Balance+3*contribution-minFee {
			t.Errorf("round %d payout: %d -> %d", round, before.Balance, after.Balance)
		}
		if sumBalances(c) != c.Status().Supply {
			t.Fatalf("round %d: balances %d != supply %d", round, sumBalances(c), c.Status().Supply)
		}
	}

	p := c.Pool(id)
	if p.Status != ledger.PoolDone || p.Balance != 0 {
		t.Fatalf("final pool %+v", p)
	}
	for i, m := range members {
		if !p.Claimed[i] {
			t.Errorf("member %d not marked claimed", i)
		}
		acc, _ := c.Account(m.addr)
		// Everyone paid 3 contributions and got one pot of 3: net zero, minus fees.
		if acc.Balance >= 100*types.Unit {
			t.Errorf("member %d balance %d should be 100 ORP minus fees", i, acc.Balance)
		}
	}
	// 1 create + 2 joins + 9 contributions + 3 claims.
	if h := c.PoolHistory(id); len(h) != 15 {
		t.Errorf("pool history has %d entries, want 15", len(h))
	}
	if ps := c.PoolsOf(carol.addr); len(ps) != 1 {
		t.Errorf("carol is in %d pools", len(ps))
	}

	// Pools survive a restart: state is rebuilt by replay.
	want := c.Status().StateRoot
	c.Close()
	if got := f.open(dir).Status().StateRoot; got != want {
		t.Error("state root changed after replay")
	}
}

func TestPoolCreateValidation(t *testing.T) {
	f := newFixture(t)
	c := f.open(t.TempDir())
	cases := map[string]*types.PoolOp{
		"one member":         {Op: types.PoolCreate, Name: "x", Members: []types.Address{f.alice.addr}, Contribution: 1},
		"creator not member": {Op: types.PoolCreate, Name: "x", Members: []types.Address{f.bob.addr, newActor(t).addr}, Contribution: 1},
		"duplicate member":   {Op: types.PoolCreate, Name: "x", Members: []types.Address{f.alice.addr, f.alice.addr}, Contribution: 1},
		"zero contribution":  {Op: types.PoolCreate, Name: "x", Members: []types.Address{f.alice.addr, f.bob.addr}},
		"no name":            {Op: types.PoolCreate, Members: []types.Address{f.alice.addr, f.bob.addr}, Contribution: 1},
		"unknown op":         {Op: "steal", ID: types.Hash{1}},
		"join without id":    {Op: types.PoolJoin},
	}
	for name, op := range cases {
		if _, err := c.Submit(f.poolTx(f.alice, 0, op)); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	withAmount := f.poolTx(f.alice, 0, &types.PoolOp{Op: types.PoolJoin, ID: types.Hash{1}})
	withAmount.Amount = 5
	withAmount.Sign(f.alice.priv)
	if _, err := c.Submit(withAmount); err == nil {
		t.Error("pool op with amount accepted")
	}
}
