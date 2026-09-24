package ledger

import (
	"fmt"
	"maps"
	"slices"

	"github.com/openreserve/node/types"
)

// Guardianship is an account's social-recovery setup.
type Guardianship struct {
	Guardians []types.Address `json:"guardians"`
	Threshold int             `json:"threshold"`
	DelaySecs int64           `json:"delay_secs"`
	Pending   *Recovery       `json:"pending,omitempty"`
}

// Recovery is an in-progress move of an account to a new key.
type Recovery struct {
	NewOwner  types.Address   `json:"new_owner"`
	Approvals []types.Address `json:"approvals"`
	StartedAt int64           `json:"started_at"`
	ReadyAt   int64           `json:"ready_at,omitempty"` // when enough guardians approved + delay
}

func (g *Guardianship) clone() *Guardianship {
	c := *g
	c.Guardians = slices.Clone(g.Guardians)
	if g.Pending != nil {
		p := *g.Pending
		p.Approvals = slices.Clone(g.Pending.Approvals)
		c.Pending = &p
	}
	return &c
}

// Guardianship returns a copy of an account's recovery setup, or nil.
func (s *State) Guardianship(a types.Address) *Guardianship {
	if g, ok := s.Guards[a]; ok {
		return g.clone()
	}
	return nil
}

// GuardedBy lists accounts that name g as a guardian.
func (s *State) GuardedBy(g types.Address) []types.Address {
	var out []types.Address
	for a, gs := range s.Guards {
		if slices.Contains(gs.Guardians, g) {
			out = append(out, a)
		}
	}
	slices.Sort(out)
	return out
}

func (s *State) guardOp(tx *types.Tx) (func(), error) {
	op := tx.Guard
	fail := func(msg string, a ...any) (func(), error) {
		return nil, fmt.Errorf("%w: "+msg, append([]any{ErrGuard}, a...)...)
	}

	if op.Op == types.GuardSet {
		if cur := s.Guards[tx.From]; cur != nil && cur.Pending != nil {
			return fail("a recovery is in progress; cancel it first")
		}
		if to, moved := s.RecoveredTo[tx.From]; moved {
			return fail("this account was recovered to %s", to)
		}
		return func() {
			s.Guards[tx.From] = &Guardianship{Guardians: slices.Clone(op.Guardians), Threshold: op.Threshold, DelaySecs: op.DelaySecs}
		}, nil
	}

	g := s.Guards[op.Account]
	if g == nil {
		return fail("this account has no guardians")
	}
	isGuardian := slices.Contains(g.Guardians, tx.From)

	switch op.Op {
	case types.GuardStart:
		switch {
		case !isGuardian:
			return fail("only a guardian can start a recovery")
		case g.Pending != nil:
			return fail("a recovery is already in progress")
		}
		return func() {
			g.Pending = &Recovery{NewOwner: op.NewOwner, Approvals: []types.Address{tx.From}, StartedAt: s.Now}
			if g.Threshold <= 1 {
				g.Pending.ReadyAt = s.Now + g.DelaySecs*1000
			}
		}, nil

	case types.GuardApprove:
		switch {
		case !isGuardian:
			return fail("only a guardian can approve")
		case g.Pending == nil:
			return fail("there is no recovery to approve")
		case g.Pending.NewOwner != op.NewOwner:
			return fail("the pending recovery is to a different key")
		case slices.Contains(g.Pending.Approvals, tx.From):
			return fail("you already approved")
		}
		return func() {
			g.Pending.Approvals = append(g.Pending.Approvals, tx.From)
			if len(g.Pending.Approvals) >= g.Threshold && g.Pending.ReadyAt == 0 {
				// The safety window starts once enough guardians agree.
				g.Pending.ReadyAt = s.Now + g.DelaySecs*1000
			}
		}, nil

	case types.GuardCancel:
		switch {
		case tx.From != op.Account:
			return fail("only the account's own key can cancel a recovery")
		case g.Pending == nil:
			return fail("there is no recovery to cancel")
		}
		return func() { g.Pending = nil }, nil

	case types.GuardFinish:
		p := g.Pending
		switch {
		case p == nil:
			return fail("there is no recovery to finish")
		case len(p.Approvals) < g.Threshold:
			return fail("waiting for guardians: %d of %d approved", len(p.Approvals), g.Threshold)
		case s.Now < p.ReadyAt:
			return fail("the safety delay has not passed")
		}
		return func() { s.moveAccount(op.Account, p.NewOwner, g) }, nil
	}
	return nil, fmt.Errorf("unknown recovery op %q", op.Op)
}

// moveAccount transfers everything old holds to new: balances, ajo
// memberships, escrow roles and the guardian setup.
func (s *State) moveAccount(old, new types.Address, g *Guardianship) {
	src := s.Accounts[old]
	if src.Balance > 0 {
		s.credit(new, "", src.Balance)
	}
	for _, sym := range slices.Sorted(maps.Keys(src.Assets)) {
		s.credit(new, sym, src.Assets[sym])
	}
	s.Accounts[old] = Account{Nonce: src.Nonce}

	for _, p := range s.Pools {
		if slices.Contains(p.Members, new) {
			continue // cannot hold two seats in one pool
		}
		for i, m := range p.Members {
			if m == old {
				p.Members[i] = new
			}
		}
		if p.Creator == old {
			p.Creator = new
		}
	}
	for _, e := range s.Escrows {
		if e.Buyer == old {
			e.Buyer = new
		}
		if e.Seller == old {
			e.Seller = new
		}
		if e.Arbiter == old {
			e.Arbiter = new
		}
	}
	s.Guards[new] = &Guardianship{Guardians: slices.Clone(g.Guardians), Threshold: g.Threshold, DelaySecs: g.DelaySecs}
	delete(s.Guards, old)
	s.RecoveredTo[old] = new
}
