package chain

import (
	"errors"
	"slices"
	"testing"

	"github.com/openreserve/node/ledger"
	"github.com/openreserve/node/types"
)

// circleEnv sets up three funded members and helpers to drive a circle.
func circleEnv(t *testing.T) (*fixture, *Chain, []actor, func(actor, *types.PoolOp) error) {
	f := newFixture(t)
	carol := newActor(t)
	members := []actor{f.alice, f.bob, carol}
	for _, m := range members {
		f.g.Allocations = append(f.g.Allocations, Allocation{Address: m.addr, Amount: 1000 * types.Unit})
	}
	f.g.Allocations = f.g.Allocations[len(f.g.Allocations)-3:]
	c := f.open(t.TempDir())
	nonce := map[types.Address]uint64{}
	send := func(a actor, op *types.PoolOp) error {
		_, err := c.Submit(f.poolTx(a, nonce[a.addr], op))
		if err == nil {
			nonce[a.addr]++
			f.produce(c)
		}
		return err
	}
	return f, c, members, send
}

func startCircle(t *testing.T, c *Chain, ms []actor, send func(actor, *types.PoolOp) error, op *types.PoolOp) types.Hash {
	t.Helper()
	op.Op, op.Name = types.PoolCreate, "circle"
	op.Members = []types.Address{ms[0].addr, ms[1].addr, ms[2].addr}
	if err := send(ms[0], op); err != nil {
		t.Fatal(err)
	}
	var id types.Hash
	for _, p := range c.PoolsOf(ms[0].addr) {
		id = p.ID
	}
	for _, m := range ms[1:] {
		if err := send(m, &types.PoolOp{Op: types.PoolJoin, ID: id}); err != nil {
			t.Fatal(err)
		}
	}
	return id
}

func checkSupply(t *testing.T, c *Chain) {
	t.Helper()
	if sumBalances(c) != c.Status().Supply {
		t.Fatalf("money created or lost: balances %d != supply %d", sumBalances(c), c.Status().Supply)
	}
}

func TestCircleInstalmentsAndInsurance(t *testing.T) {
	_, c, ms, send := circleEnv(t)
	each := 30 * types.Unit
	id := startCircle(t, c, ms, send, &types.PoolOp{Contribution: each, InsuranceBps: 1000})

	// Bob pays in two instalments.
	if err := send(ms[1], &types.PoolOp{Op: types.PoolContribute, ID: id, Contribution: 10 * types.Unit}); err != nil {
		t.Fatal(err)
	}
	if p := c.Pool(id); p.Paid[1] || p.PaidAmt[1] != 10*types.Unit {
		t.Fatalf("after first instalment: paid=%v amt=%v", p.Paid, p.PaidAmt)
	}
	if err := send(ms[1], &types.PoolOp{Op: types.PoolContribute, ID: id, Contribution: 25 * types.Unit}); !errors.Is(err, ledger.ErrPool) {
		t.Fatalf("overpaying accepted: %v", err)
	}
	if err := send(ms[1], &types.PoolOp{Op: types.PoolContribute, ID: id}); err != nil { // the rest
		t.Fatal(err)
	}
	send(ms[0], &types.PoolOp{Op: types.PoolContribute, ID: id})
	send(ms[2], &types.PoolOp{Op: types.PoolContribute, ID: id})
	before, _ := c.Account(ms[0].addr)
	if err := send(ms[0], &types.PoolOp{Op: types.PoolClaim, ID: id}); err != nil {
		t.Fatal(err)
	}
	after, _ := c.Account(ms[0].addr)
	// Pot 90, 10% to insurance: alice receives 81 (minus the claim fee).
	if got := after.Balance - before.Balance + minFee; got != 81*types.Unit {
		t.Fatalf("payout %d, want 81", got)
	}
	if p := c.Pool(id); p.Insurance != 9*types.Unit || p.Round != 1 {
		t.Fatalf("insurance %d round %d", p.Insurance, p.Round)
	}
	checkSupply(t, c)
}

func TestCircleBidding(t *testing.T) {
	_, c, ms, send := circleEnv(t)
	each := 10 * types.Unit
	id := startCircle(t, c, ms, send, &types.PoolOp{Contribution: each, Mode: types.PoolBidding})
	if err := send(ms[2], &types.PoolOp{Op: types.PoolBid, ID: id, Contribution: 20 * types.Unit}); !errors.Is(err, ledger.ErrPool) {
		t.Fatalf("bid over half the pot accepted: %v", err)
	}
	if err := send(ms[2], &types.PoolOp{Op: types.PoolBid, ID: id, Contribution: 4 * types.Unit}); err != nil {
		t.Fatal(err)
	}
	if p := c.Pool(id); p.Recipient() != 2 {
		t.Fatalf("recipient %d, want carol", p.Recipient())
	}
	for _, m := range ms {
		send(m, &types.PoolOp{Op: types.PoolContribute, ID: id})
	}
	if err := send(ms[0], &types.PoolOp{Op: types.PoolClaim, ID: id}); !errors.Is(err, ledger.ErrPool) {
		t.Fatalf("non-winner claimed: %v", err)
	}
	a0, _ := c.Account(ms[0].addr)
	c0, _ := c.Account(ms[2].addr)
	if err := send(ms[2], &types.PoolOp{Op: types.PoolClaim, ID: id}); err != nil {
		t.Fatal(err)
	}
	a1, _ := c.Account(ms[0].addr)
	c1, _ := c.Account(ms[2].addr)
	if a1.Balance-a0.Balance != 2*types.Unit || c1.Balance-c0.Balance+minFee != 26*types.Unit {
		t.Fatalf("discount split: alice +%d carol +%d", a1.Balance-a0.Balance, c1.Balance-c0.Balance+minFee)
	}
	checkSupply(t, c)
}

func TestCircleLotteryAndSwap(t *testing.T) {
	_, c, ms, send := circleEnv(t)
	id := startCircle(t, c, ms, send, &types.PoolOp{Contribution: types.Unit, Mode: types.PoolLottery})
	p := c.Pool(id)
	got := slices.Clone(p.Members)
	slices.SortFunc(got, func(a, b types.Address) int { return compareAddr(a, b) })
	want := []types.Address{ms[0].addr, ms[1].addr, ms[2].addr}
	slices.SortFunc(want, func(a, b types.Address) int { return compareAddr(a, b) })
	if !slices.Equal(got, want) {
		t.Fatalf("lottery lost a member: %v", p.Members)
	}
	// The two members after round 0 swap turns: both must ask.
	x, y := p.Members[1], p.Members[2]
	who := func(a types.Address) actor {
		for _, m := range ms {
			if m.addr == a {
				return m
			}
		}
		t.Fatal("unknown member")
		return actor{}
	}
	if err := send(who(x), &types.PoolOp{Op: types.PoolSwap, ID: id, Other: y}); err != nil {
		t.Fatal(err)
	}
	if c.Pool(id).Members[1] != x {
		t.Fatal("swapped on one request")
	}
	if err := send(who(y), &types.PoolOp{Op: types.PoolSwap, ID: id, Other: x}); err != nil {
		t.Fatal(err)
	}
	if q := c.Pool(id); q.Members[1] != y || q.Members[2] != x {
		t.Fatalf("not swapped: %v", q.Members)
	}
	if err := send(who(p.Members[0]), &types.PoolOp{Op: types.PoolSwap, ID: id, Other: x}); !errors.Is(err, ledger.ErrPool) {
		t.Fatalf("current turn swapped: %v", err)
	}
}

func TestCircleGoal(t *testing.T) {
	_, c, ms, send := circleEnv(t)
	id := startCircle(t, c, ms, send, &types.PoolOp{Contribution: types.Unit, Mode: types.PoolGoal, Target: 100 * types.Unit})
	if err := send(ms[0], &types.PoolOp{Op: types.PoolWithdraw, ID: id}); !errors.Is(err, ledger.ErrPool) {
		t.Fatalf("withdrew before goal: %v", err)
	}
	send(ms[0], &types.PoolOp{Op: types.PoolContribute, ID: id, Contribution: 60 * types.Unit})
	send(ms[1], &types.PoolOp{Op: types.PoolContribute, ID: id, Contribution: 45 * types.Unit})
	if p := c.Pool(id); p.Status != ledger.PoolDone || p.Balance != 105*types.Unit {
		t.Fatalf("goal not reached: %s %d", p.Status, p.Balance)
	}
	before, _ := c.Account(ms[1].addr)
	if err := send(ms[1], &types.PoolOp{Op: types.PoolWithdraw, ID: id}); err != nil {
		t.Fatal(err)
	}
	after, _ := c.Account(ms[1].addr)
	if after.Balance-before.Balance+minFee != 45*types.Unit {
		t.Fatalf("withdrew %d", after.Balance-before.Balance+minFee)
	}
	checkSupply(t, c)
}

func compareAddr(a, b types.Address) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}
