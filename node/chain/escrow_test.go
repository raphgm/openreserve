package chain

import (
	"errors"
	"testing"

	"github.com/openreserve/node/ledger"
	"github.com/openreserve/node/types"
)

type escrowEnv struct {
	*fixture
	c     *Chain
	nonce map[types.Address]uint64
	t     *testing.T
}

func (e *escrowEnv) op(a actor, fee types.Amount, x *types.EscrowOp) error {
	tx := &types.Tx{ChainID: e.g.ChainID, From: a.addr, Fee: fee, Nonce: e.nonce[a.addr], Escrow: x}
	tx.Sign(a.priv)
	if _, err := e.c.Submit(tx); err != nil {
		return err
	}
	e.nonce[a.addr]++
	e.produce(e.c)
	return nil
}

func (e *escrowEnv) create(buyer, seller, arbiter actor, milestones ...types.Amount) types.Hash {
	e.t.Helper()
	x := &types.EscrowOp{Op: types.EscrowCreate, Seller: seller.addr, Arbiter: arbiter.addr, Milestones: milestones,
		ShipBy: e.now + 10_000, ReviewSecs: 3600, Ref: "PN-8291-X"}
	tx := &types.Tx{ChainID: e.g.ChainID, From: buyer.addr, Fee: minFee, Nonce: e.nonce[buyer.addr], Escrow: x}
	tx.Sign(buyer.priv)
	if _, err := e.c.Submit(tx); err != nil {
		e.t.Fatal(err)
	}
	e.nonce[buyer.addr]++
	e.produce(e.c)
	return tx.ID()
}

func newEscrowEnv(t *testing.T) (*escrowEnv, actor) {
	f := newFixture(t)
	arb := newActor(t)
	return &escrowEnv{fixture: f, c: f.open(t.TempDir()), nonce: map[types.Address]uint64{}, t: t}, arb
}

func (e *escrowEnv) bal(a actor) types.Amount { acc, _ := e.c.Account(a.addr); return acc.Balance }

func TestEscrowMilestones(t *testing.T) {
	e, arb := newEscrowEnv(t)
	buyer, seller := e.alice, e.bob
	id := e.create(buyer, seller, arb, 30*types.Unit, 70*types.Unit-2*minFee)
	x := e.c.Escrow(id)
	if x.Status != ledger.EscrowFunded || x.Balance != 100*types.Unit-2*minFee || x.Ref != "PN-8291-X" {
		t.Fatalf("after create: %+v", x)
	}
	id2 := func(op string) *types.EscrowOp { return &types.EscrowOp{Op: op, ID: id} }

	// Only the right party can act; nobody else can touch the funds.
	for _, bad := range []struct {
		who actor
		op  string
	}{{buyer, types.EscrowDispatch}, {seller, types.EscrowRelease}, {arb, types.EscrowRelease}, {seller, types.EscrowClaim}, {arb, types.EscrowResolve}} {
		if err := e.op(bad.who, 0, id2(bad.op)); !errors.Is(err, ledger.ErrEscrow) {
			t.Errorf("%s by wrong party: %v", bad.op, err)
		}
	}

	// Seller dispatches with no balance at all: escrow steps are fee-free.
	if err := e.op(seller, 0, &types.EscrowOp{Op: types.EscrowDispatch, ID: id}); err != nil {
		t.Fatal(err)
	}
	if x := e.c.Escrow(id); x.Status != ledger.EscrowDispatched {
		t.Fatalf("status %s", x.Status)
	}
	if err := e.op(buyer, 0, id2(types.EscrowRelease)); err != nil {
		t.Fatal(err)
	}
	if e.bal(seller) != 30*types.Unit {
		t.Fatalf("seller after first milestone: %d", e.bal(seller))
	}
	if err := e.op(buyer, 0, id2(types.EscrowRelease)); err != nil {
		t.Fatal(err)
	}
	x = e.c.Escrow(id)
	if x.Status != ledger.EscrowCompleted || x.Balance != 0 || e.bal(seller) != 100*types.Unit-2*minFee {
		t.Fatalf("after final release: %+v seller=%d", x, e.bal(seller))
	}
	if err := e.op(buyer, 0, id2(types.EscrowRelease)); !errors.Is(err, ledger.ErrEscrow) {
		t.Errorf("release after completion: %v", err)
	}
}

func TestEscrowDisputeResolve(t *testing.T) {
	e, arb := newEscrowEnv(t)
	buyer, seller := e.alice, e.bob
	id := e.create(buyer, seller, arb, 50*types.Unit)
	before := e.bal(buyer)
	if err := e.op(buyer, 0, &types.EscrowOp{Op: types.EscrowDispute, ID: id}); err != nil {
		t.Fatal(err)
	}
	if err := e.op(buyer, 0, &types.EscrowOp{Op: types.EscrowRelease, ID: id}); !errors.Is(err, ledger.ErrEscrow) {
		t.Errorf("release during dispute: %v", err)
	}
	if err := e.op(arb, 0, &types.EscrowOp{Op: types.EscrowResolve, ID: id, ToSeller: 60 * types.Unit}); !errors.Is(err, ledger.ErrEscrow) {
		t.Errorf("over-allocation: %v", err)
	}
	if err := e.op(arb, 0, &types.EscrowOp{Op: types.EscrowResolve, ID: id, ToSeller: 20 * types.Unit}); err != nil {
		t.Fatal(err)
	}
	x := e.c.Escrow(id)
	if x.Status != ledger.EscrowResolved || e.bal(seller) != 20*types.Unit || e.bal(buyer) != before+30*types.Unit {
		t.Fatalf("resolve: %+v seller=%d buyer=%d", x, e.bal(seller), e.bal(buyer))
	}
	if e.bal(arb) != 0 {
		t.Error("arbiter must never receive funds")
	}
}

func TestEscrowTimeouts(t *testing.T) {
	e, arb := newEscrowEnv(t)
	buyer, seller := e.alice, e.bob

	// Not dispatched: the buyer can reclaim only after the ship-by deadline.
	id := e.create(buyer, seller, arb, 10*types.Unit)
	if err := e.op(buyer, 0, &types.EscrowOp{Op: types.EscrowRefund, ID: id}); !errors.Is(err, ledger.ErrEscrow) {
		t.Errorf("early reclaim: %v", err)
	}
	e.now += 20_000 // past ship_by
	if err := e.op(buyer, 0, &types.EscrowOp{Op: types.EscrowRefund, ID: id}); err != nil {
		t.Fatal(err)
	}
	if x := e.c.Escrow(id); x.Status != ledger.EscrowRefunded || x.PaidBuyer != 10*types.Unit {
		t.Fatalf("reclaim: %+v", x)
	}

	// Dispatched but the buyer goes silent: the seller claims after review.
	id = e.create(buyer, seller, arb, 10*types.Unit)
	e.op(seller, 0, &types.EscrowOp{Op: types.EscrowDispatch, ID: id})
	if err := e.op(seller, 0, &types.EscrowOp{Op: types.EscrowClaim, ID: id}); !errors.Is(err, ledger.ErrEscrow) {
		t.Errorf("claim during review: %v", err)
	}
	e.now += 3600 * 1000
	if err := e.op(seller, 0, &types.EscrowOp{Op: types.EscrowClaim, ID: id}); err != nil {
		t.Fatal(err)
	}
	if x := e.c.Escrow(id); x.Status != ledger.EscrowCompleted || e.bal(seller) != 10*types.Unit {
		t.Fatalf("claim: %+v", x)
	}
	if n := len(e.c.EscrowHistory(id)); n != 3 {
		t.Errorf("history has %d entries, want 3", n)
	}
	if sumBalances(e.c) != e.c.Status().Supply {
		t.Error("escrows leaked or created ORP")
	}
}
