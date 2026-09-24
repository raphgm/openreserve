package chain

import (
	"errors"
	"testing"

	"github.com/openreserve/node/ledger"
	"github.com/openreserve/node/types"
)

func TestGuardianRecovery(t *testing.T) {
	f := newFixture(t)
	g1, g2, g3, thief := newActor(t), newActor(t), newActor(t), newActor(t)
	issuer := newActor(t)
	f.g.Assets = []ledger.AssetDef{{Symbol: "NGN", Name: "Naira", Issuer: issuer.addr, MinFee: 10_000, Decimals: 2}}
	c := f.open(t.TempDir())
	nonce := map[types.Address]uint64{}
	send := func(a actor, tx *types.Tx) error {
		tx.ChainID, tx.From, tx.Nonce = f.g.ChainID, a.addr, nonce[a.addr]
		tx.Sign(a.priv)
		if _, err := c.Submit(tx); err != nil {
			return err
		}
		nonce[a.addr]++
		f.produce(c)
		return nil
	}
	must := func(a actor, tx *types.Tx) {
		t.Helper()
		if err := send(a, tx); err != nil {
			t.Fatal(err)
		}
	}
	guard := func(op *types.GuardOp) *types.Tx { return &types.Tx{Guard: op} }
	alice, newPhone := f.alice, newActor(t)

	// Alice holds ORP and naira, is in an ajo and is the buyer in an escrow.
	must(issuer, &types.Tx{Kind: types.KindMint, Asset: "NGN", To: alice.addr, Amount: 500 * types.Unit})
	must(alice, &types.Tx{Fee: minFee, Pool: &types.PoolOp{Op: types.PoolCreate, Name: "ajo", Members: []types.Address{alice.addr, f.bob.addr}, Contribution: types.Unit}})
	must(alice, &types.Tx{Fee: minFee, Escrow: &types.EscrowOp{Op: types.EscrowCreate, Seller: f.bob.addr, Arbiter: g3.addr, Milestones: []types.Amount{5 * types.Unit}, ShipBy: f.now + 100_000, ReviewSecs: 3600}})

	// Alice names three guardians; any two can recover after a 1-hour delay.
	set := guard(&types.GuardOp{Op: types.GuardSet, Guardians: []types.Address{g1.addr, g2.addr, g3.addr}, Threshold: 2, DelaySecs: 3600})
	set.Fee = minFee // setting guardians costs the normal fee; recovery steps are free
	must(alice, set)

	// Non-guardians can't start; a lone guardian can't finish.
	if err := send(thief, guard(&types.GuardOp{Op: types.GuardStart, Account: alice.addr, NewOwner: thief.addr})); !errors.Is(err, ledger.ErrGuard) {
		t.Fatalf("stranger started recovery: %v", err)
	}
	must(g1, guard(&types.GuardOp{Op: types.GuardStart, Account: alice.addr, NewOwner: thief.addr}))
	if err := send(g1, guard(&types.GuardOp{Op: types.GuardFinish, Account: alice.addr})); !errors.Is(err, ledger.ErrGuard) {
		t.Fatalf("finished with one approval: %v", err)
	}
	// Alice still has her phone: she cancels the bad recovery.
	must(alice, guard(&types.GuardOp{Op: types.GuardCancel, Account: alice.addr}))
	if c.Recovery(alice.addr).Guardianship.Pending != nil {
		t.Fatal("cancel did not clear the recovery")
	}

	// Later alice really loses her phone: guardians move her to a new key.
	must(g1, guard(&types.GuardOp{Op: types.GuardStart, Account: alice.addr, NewOwner: newPhone.addr}))
	if err := send(g3, guard(&types.GuardOp{Op: types.GuardApprove, Account: alice.addr, NewOwner: thief.addr})); !errors.Is(err, ledger.ErrGuard) {
		t.Fatalf("approved a different key: %v", err)
	}
	must(g2, guard(&types.GuardOp{Op: types.GuardApprove, Account: alice.addr, NewOwner: newPhone.addr}))
	req := c.Recovery(g2.addr).Requests
	if len(req) != 1 || req[0].Approvals != 2 || !req[0].Approved {
		t.Fatalf("guardian view: %+v", req)
	}
	// Too early: the safety delay protects the owner.
	if err := send(newPhone, guard(&types.GuardOp{Op: types.GuardFinish, Account: alice.addr})); !errors.Is(err, ledger.ErrGuard) {
		t.Fatalf("finished before delay: %v", err)
	}
	orpBefore, _ := c.Account(alice.addr)
	f.now += 3600 * 1000
	must(newPhone, guard(&types.GuardOp{Op: types.GuardFinish, Account: alice.addr}))

	// Everything moved to the new key.
	newAcc, _ := c.Account(newPhone.addr)
	oldAcc, _ := c.Account(alice.addr)
	if newAcc.Balance != orpBefore.Balance || newAcc.Assets["NGN"] != 500*types.Unit || oldAcc.Balance != 0 || len(oldAcc.Assets) != 0 {
		t.Fatalf("balances: new=%+v old=%+v", newAcc, oldAcc)
	}
	if ps := c.PoolsOf(newPhone.addr); len(ps) != 1 || ps[0].Creator != newPhone.addr {
		t.Fatalf("ajo membership not moved: %+v", ps)
	}
	if es := c.EscrowsOf(newPhone.addr); len(es) != 1 || es[0].Buyer != newPhone.addr {
		t.Fatalf("escrow role not moved: %+v", es)
	}
	info := c.Recovery(alice.addr)
	if info.RecoveredTo != newPhone.addr || c.Recovery(newPhone.addr).Guardianship == nil {
		t.Fatalf("recovery record: %+v", info)
	}
	// The new key can use the money; the old key cannot.
	must(newPhone, &types.Tx{To: f.bob.addr, Amount: types.Unit, Fee: minFee})
	if err := send(alice, &types.Tx{To: f.bob.addr, Amount: types.Unit, Fee: minFee}); err == nil {
		t.Fatal("old key still spends")
	}
	if sumBalances(c) != c.Status().Supply {
		t.Fatal("ORP supply mismatch after recovery")
	}
}
