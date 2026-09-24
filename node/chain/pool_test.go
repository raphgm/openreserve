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
		if p.Asset == "" {
			total += p.Balance + p.Held()
		}
	}
	for _, x := range c.state.Escrows {
		if x.Asset == "" {
			total += x.Balance
		}
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

// Scheduled ajo: one member stops paying. The round's member still collects
// after the due date, the defaulter's deposit covers them, and the default
// is on record for everyone to see.
func TestPoolDefaultCoveredByDeposit(t *testing.T) {
	f := newFixture(t)
	carol := newActor(t)
	f.g.Allocations = []Allocation{
		{Address: f.alice.addr, Amount: 100 * types.Unit},
		{Address: f.bob.addr, Amount: 100 * types.Unit},
		{Address: carol.addr, Amount: 100 * types.Unit},
	}
	c := f.open(t.TempDir())
	nonce := map[types.Address]uint64{}
	op := func(a actor, o *types.PoolOp) error {
		_, err := c.Submit(f.poolTx(a, nonce[a.addr], o))
		if err == nil {
			nonce[a.addr]++
			f.produce(c)
		}
		return err
	}
	must := func(a actor, o *types.PoolOp) {
		t.Helper()
		if err := op(a, o); err != nil {
			t.Fatal(err)
		}
	}
	week := int64(7 * 24 * 3600)
	create := f.poolTx(f.alice, 0, &types.PoolOp{Op: types.PoolCreate, Name: "Market women ajo",
		Members: []types.Address{f.alice.addr, f.bob.addr, carol.addr}, Contribution: 10 * types.Unit,
		RoundSecs: week, Deposit: 10 * types.Unit})
	if _, err := c.Submit(create); err != nil {
		t.Fatal(err)
	}
	nonce[f.alice.addr]++
	f.produce(c)
	id := create.ID()
	must(f.bob, &types.PoolOp{Op: types.PoolJoin, ID: id})
	must(carol, &types.PoolOp{Op: types.PoolJoin, ID: id})
	p := c.Pool(id)
	if p.Status != ledger.PoolActive || p.Held() != 30*types.Unit || p.StartedAt == 0 {
		t.Fatalf("after joins: %+v", p)
	}

	// Round 1 (alice receives): bob pays, carol does not.
	must(f.alice, &types.PoolOp{Op: types.PoolContribute, ID: id})
	must(f.bob, &types.PoolOp{Op: types.PoolContribute, ID: id})
	if err := op(f.alice, &types.PoolOp{Op: types.PoolClaim, ID: id}); !errors.Is(err, ledger.ErrPool) {
		t.Fatalf("claim before due date: %v", err)
	}
	f.now = p.DueAt(0) // the due date passes
	before, _ := c.Account(f.alice.addr)
	must(f.alice, &types.PoolOp{Op: types.PoolClaim, ID: id})
	after, _ := c.Account(f.alice.addr)
	p = c.Pool(id)
	if after.Balance != before.Balance+30*types.Unit-minFee {
		t.Errorf("alice got %d, want the full 30 ORP pot", after.Balance-before.Balance+minFee)
	}
	if p.Defaults[2] != 1 || p.Deposits[2] != 0 || p.PaidOut[0] != 30*types.Unit {
		t.Fatalf("after covered default: defaults=%v deposits=%v paidout=%v", p.Defaults, p.Deposits, p.PaidOut)
	}
	if sumBalances(c) != c.Status().Supply {
		t.Fatal("supply mismatch after default")
	}

	// Round 2 (bob receives): carol defaults again, with no deposit left:
	// bob gets what was actually paid.
	must(f.alice, &types.PoolOp{Op: types.PoolContribute, ID: id})
	must(f.bob, &types.PoolOp{Op: types.PoolContribute, ID: id})
	f.now = p.DueAt(1)
	must(f.bob, &types.PoolOp{Op: types.PoolClaim, ID: id})
	if p = c.Pool(id); p.PaidOut[1] != 20*types.Unit || p.Defaults[2] != 2 {
		t.Fatalf("round 2: paidout=%v defaults=%v", p.PaidOut, p.Defaults)
	}

	// Round 3 (carol receives): everyone pays; pool ends and alice's and
	// bob's unused deposits come back.
	aliceBefore, _ := c.Account(f.alice.addr)
	must(f.alice, &types.PoolOp{Op: types.PoolContribute, ID: id})
	must(f.bob, &types.PoolOp{Op: types.PoolContribute, ID: id})
	must(carol, &types.PoolOp{Op: types.PoolContribute, ID: id})
	must(carol, &types.PoolOp{Op: types.PoolClaim, ID: id})
	p = c.Pool(id)
	aliceAfter, _ := c.Account(f.alice.addr)
	if p.Status != ledger.PoolDone || p.Held() != 0 || p.Balance != 0 {
		t.Fatalf("final pool %+v", p)
	}
	if aliceAfter.Balance != aliceBefore.Balance-10*types.Unit-minFee+10*types.Unit {
		t.Errorf("alice's deposit not returned: %d -> %d", aliceBefore.Balance, aliceAfter.Balance)
	}
	if sumBalances(c) != c.Status().Supply {
		t.Fatal("supply mismatch at end")
	}
}

func TestPoolScheduleValidation(t *testing.T) {
	f := newFixture(t)
	c := f.open(t.TempDir())
	members := []types.Address{f.alice.addr, f.bob.addr}
	bad := map[string]*types.PoolOp{
		"deposit without schedule": {Op: types.PoolCreate, Name: "x", Members: members, Contribution: types.Unit, Deposit: types.Unit},
		"round too short":          {Op: types.PoolCreate, Name: "x", Members: members, Contribution: types.Unit, RoundSecs: 60},
		"deposit over a pot":       {Op: types.PoolCreate, Name: "x", Members: members, Contribution: types.Unit, RoundSecs: 86400, Deposit: 3 * types.Unit},
	}
	for name, o := range bad {
		if _, err := c.Submit(f.poolTx(f.alice, 0, o)); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}
