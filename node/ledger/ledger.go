// Package ledger holds account and savings-pool state and the rules for
// applying transactions to it.
package ledger

import (
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"slices"
	"sort"

	"github.com/openreserve/node/types"
)

// Account is the state of a single address.
type Account struct {
	Balance types.Amount `json:"balance"`
	Nonce   uint64       `json:"nonce"`
}

// Pool status values.
const (
	PoolForming = "forming" // waiting for every member to join
	PoolActive  = "active"
	PoolDone    = "done"
)

// Pool is an on-chain rotating savings pool. Its balance is held by the
// ledger itself: no key can spend it, only the claim rule.
type Pool struct {
	ID           types.Hash      `json:"id"`
	Name         string          `json:"name"`
	Creator      types.Address   `json:"creator"`
	Members      []types.Address `json:"members"` // payout order
	Contribution types.Amount    `json:"contribution"`
	Joined       []bool          `json:"joined"`
	Status       string          `json:"status"`
	Round        int             `json:"round"`   // 0-based; Members[Round] receives this round
	Paid         []bool          `json:"paid"`    // contributions received this round
	Claimed      []bool          `json:"claimed"` // members who already received their pot
	Balance      types.Amount    `json:"balance"`
}

// Pot is what the round's recipient receives.
func (p *Pool) Pot() types.Amount { return p.Contribution * types.Amount(len(p.Members)) }

func (p *Pool) index(a types.Address) int { return slices.Index(p.Members, a) }

func (p *Pool) clone() *Pool {
	c := *p
	c.Members = slices.Clone(p.Members)
	c.Joined = slices.Clone(p.Joined)
	c.Paid = slices.Clone(p.Paid)
	c.Claimed = slices.Clone(p.Claimed)
	return &c
}

// State is the full ledger. It is not safe for concurrent use; the chain
// package serializes access.
type State struct {
	Accounts map[types.Address]Account
	Pools    map[types.Hash]*Pool
	Supply   types.Amount // all ORP in accounts and pools; fees are burned from it
	Burned   types.Amount
	MinFee   types.Amount
}

// Errors returned by Apply. ErrNonceTooHigh lets the mempool keep a tx that
// may become valid once earlier txs from the same sender land.
var (
	ErrNonceTooLow  = errors.New("nonce too low")
	ErrNonceTooHigh = errors.New("nonce too high")
	ErrInsufficient = errors.New("insufficient funds")
	ErrFeeTooLow    = errors.New("fee below minimum")
	ErrOverflow     = errors.New("balance overflow")
	ErrPool         = errors.New("pool rule")
)

// New creates an empty state.
func New(minFee types.Amount) *State {
	return &State{Accounts: map[types.Address]Account{}, Pools: map[types.Hash]*Pool{}, MinFee: minFee}
}

// Get returns an account; unknown addresses have zero balance and nonce.
func (s *State) Get(a types.Address) Account { return s.Accounts[a] }

// Pool returns a copy of a pool, or nil.
func (s *State) Pool(id types.Hash) *Pool {
	if p, ok := s.Pools[id]; ok {
		return p.clone()
	}
	return nil
}

// Credit adds funds outside of a transaction (genesis allocation only).
func (s *State) Credit(a types.Address, amt types.Amount) error {
	acc := s.Accounts[a]
	if acc.Balance+amt < acc.Balance || s.Supply+amt < s.Supply {
		return ErrOverflow
	}
	acc.Balance += amt
	s.Supply += amt
	s.Accounts[a] = acc
	return nil
}

// Cost is what a tx will debit from its sender: amount plus fee, or the
// contribution plus fee for a pool contribution.
func (s *State) Cost(tx *types.Tx) types.Amount {
	cost := tx.Amount + tx.Fee
	if tx.Pool != nil && tx.Pool.Op == types.PoolContribute {
		if p := s.Pools[tx.Pool.ID]; p != nil {
			cost = p.Contribution + tx.Fee
		}
	}
	return cost
}

// Check validates a tx against current state without modifying it. The tx
// must already have passed CheckStateless.
func (s *State) Check(tx *types.Tx) error { return s.apply(tx, false) }

// Apply validates and executes a tx.
func (s *State) Apply(tx *types.Tx) error { return s.apply(tx, true) }

func (s *State) apply(tx *types.Tx, commit bool) error {
	if tx.Fee < s.MinFee {
		return fmt.Errorf("%w: need %s ORP", ErrFeeTooLow, types.FormatAmount(s.MinFee))
	}
	from := s.Accounts[tx.From]
	switch {
	case tx.Nonce < from.Nonce:
		return fmt.Errorf("%w: account nonce is %d", ErrNonceTooLow, from.Nonce)
	case tx.Nonce > from.Nonce:
		return fmt.Errorf("%w: account nonce is %d", ErrNonceTooHigh, from.Nonce)
	}
	cost := s.Cost(tx)
	if from.Balance < cost {
		return fmt.Errorf("%w: balance %s ORP", ErrInsufficient, types.FormatAmount(from.Balance))
	}

	var effect func()
	var err error
	if tx.Pool == nil {
		effect, err = s.transfer(tx)
	} else {
		effect, err = s.poolOp(tx)
	}
	if err != nil || !commit {
		return err
	}

	from.Balance -= cost
	from.Nonce++
	s.Accounts[tx.From] = from
	s.Supply -= tx.Fee
	s.Burned += tx.Fee
	effect()
	return nil
}

// Each rule validates first and returns the state change as a closure, so
// Check and Apply share one code path and a rejected tx changes nothing.

func (s *State) transfer(tx *types.Tx) (func(), error) {
	to := s.Accounts[tx.To]
	if to.Balance+tx.Amount < to.Balance {
		return nil, ErrOverflow
	}
	return func() {
		to := s.Accounts[tx.To]
		to.Balance += tx.Amount
		s.Accounts[tx.To] = to
	}, nil
}

func (s *State) poolOp(tx *types.Tx) (func(), error) {
	op := tx.Pool
	if op.Op == types.PoolCreate {
		id := tx.ID()
		if _, exists := s.Pools[id]; exists {
			return nil, fmt.Errorf("%w: pool already exists", ErrPool)
		}
		return func() {
			n := len(op.Members)
			p := &Pool{
				ID: id, Name: op.Name, Creator: tx.From, Members: slices.Clone(op.Members),
				Contribution: op.Contribution, Joined: make([]bool, n), Status: PoolForming,
				Paid: make([]bool, n), Claimed: make([]bool, n),
			}
			p.Joined[p.index(tx.From)] = true
			s.Pools[id] = p
		}, nil
	}

	p := s.Pools[op.ID]
	if p == nil {
		return nil, fmt.Errorf("%w: no pool %s", ErrPool, op.ID)
	}
	i := p.index(tx.From)
	if i < 0 {
		return nil, fmt.Errorf("%w: not a member of this pool", ErrPool)
	}

	switch op.Op {
	case types.PoolJoin:
		if p.Status != PoolForming || p.Joined[i] {
			return nil, fmt.Errorf("%w: already joined", ErrPool)
		}
		return func() {
			p.Joined[i] = true
			if !slices.Contains(p.Joined, false) {
				p.Status = PoolActive
			}
		}, nil

	case types.PoolContribute:
		if p.Status != PoolActive {
			return nil, fmt.Errorf("%w: pool is %s", ErrPool, p.Status)
		}
		if p.Paid[i] {
			return nil, fmt.Errorf("%w: already contributed this round", ErrPool)
		}
		return func() {
			p.Paid[i] = true
			p.Balance += p.Contribution
		}, nil

	case types.PoolClaim:
		if p.Status != PoolActive {
			return nil, fmt.Errorf("%w: pool is %s", ErrPool, p.Status)
		}
		if i != p.Round {
			return nil, fmt.Errorf("%w: it is %s's turn", ErrPool, p.Members[p.Round])
		}
		if slices.Contains(p.Paid, false) {
			return nil, fmt.Errorf("%w: waiting for all contributions this round", ErrPool)
		}
		pot := p.Pot()
		if to := s.Accounts[tx.From]; to.Balance+pot < to.Balance {
			return nil, ErrOverflow
		}
		return func() {
			acc := s.Accounts[tx.From]
			acc.Balance += pot
			s.Accounts[tx.From] = acc
			p.Balance -= pot
			p.Claimed[i] = true
			p.Round++
			clear(p.Paid)
			if p.Round == len(p.Members) {
				p.Status = PoolDone
			}
		}, nil
	}
	return nil, fmt.Errorf("unknown pool op %q", op.Op)
}

// Clone returns an independent copy, used to build and verify blocks
// without touching the committed state.
func (s *State) Clone() *State {
	c := *s
	c.Accounts = make(map[types.Address]Account, len(s.Accounts))
	for k, v := range s.Accounts {
		c.Accounts[k] = v
	}
	c.Pools = make(map[types.Hash]*Pool, len(s.Pools))
	for k, v := range s.Pools {
		c.Pools[k] = v.clone()
	}
	return &c
}

// Root commits to the full state: every account, every pool and the supply
// counters. It is a flat hash over sorted keys, which is fine at this scale;
// a Merkle tree would be needed for light-client proofs.
func (s *State) Root() types.Hash {
	h := sha256.New()
	var buf [8]byte
	u64 := func(v uint64) { binary.BigEndian.PutUint64(buf[:], v); h.Write(buf[:]) }
	str := func(v string) { u64(uint64(len(v))); h.Write([]byte(v)) }
	bools := func(bs []bool) {
		for _, b := range bs {
			if b {
				h.Write([]byte{1})
			} else {
				h.Write([]byte{0})
			}
		}
	}

	addrs := make([]string, 0, len(s.Accounts))
	for a := range s.Accounts {
		addrs = append(addrs, string(a))
	}
	sort.Strings(addrs)
	for _, a := range addrs {
		acc := s.Accounts[types.Address(a)]
		h.Write([]byte(a))
		u64(acc.Balance)
		u64(acc.Nonce)
	}

	ids := make([]types.Hash, 0, len(s.Pools))
	for id := range s.Pools {
		ids = append(ids, id)
	}
	slices.SortFunc(ids, func(a, b types.Hash) int { return slices.Compare(a[:], b[:]) })
	for _, id := range ids {
		p := s.Pools[id]
		h.Write(id[:])
		str(p.Name)
		str(string(p.Creator))
		u64(uint64(len(p.Members)))
		for _, m := range p.Members {
			str(string(m))
		}
		u64(p.Contribution)
		bools(p.Joined)
		str(p.Status)
		u64(uint64(p.Round))
		bools(p.Paid)
		bools(p.Claimed)
		u64(p.Balance)
	}

	u64(s.Supply)
	u64(s.Burned)
	var out types.Hash
	copy(out[:], h.Sum(nil))
	return out
}
