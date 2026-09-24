// Package ledger holds account and savings-pool state and the rules for
// applying transactions to it.
package ledger

import (
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"maps"
	"slices"
	"sort"
	"time"

	"github.com/openreserve/node/types"
)

// Account is the state of a single address. Balance is ORP; Assets holds
// balances of issued assets such as NGN.
type Account struct {
	Balance types.Amount            `json:"balance"`
	Nonce   uint64                  `json:"nonce"`
	Assets  map[string]types.Amount `json:"assets,omitempty"`
}

func (a Account) clone() Account {
	if a.Assets != nil {
		a.Assets = maps.Clone(a.Assets)
	}
	return a
}

// AssetDef describes an issued asset. Only Issuer can mint it; the issuer
// is expected to hold an equal amount of the real currency in reserve
// (e.g. naira collected through Paystack).
type AssetDef struct {
	Symbol   string        `json:"symbol"`
	Name     string        `json:"name"`
	Issuer   types.Address `json:"issuer"`
	MinFee   types.Amount  `json:"min_fee"`
	Decimals int           `json:"decimals"` // display precision, e.g. 2 for naira
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
	Asset        string          `json:"asset,omitempty"` // "" = ORP
	Creator      types.Address   `json:"creator"`
	Members      []types.Address `json:"members"` // payout order
	Contribution types.Amount    `json:"contribution"`
	Joined       []bool          `json:"joined"`
	Status       string          `json:"status"`
	Round        int             `json:"round"`   // 0-based; Members[Round] receives this round
	Paid         []bool          `json:"paid"`    // contributions received this round
	Claimed      []bool          `json:"claimed"` // members who already received their pot
	Balance      types.Amount    `json:"balance"` // contributions collected this round
	// Schedule and default protection (zero when the pool has none).
	RoundSecs int64          `json:"round_secs,omitempty"`
	Deposit   types.Amount   `json:"deposit,omitempty"`
	StartedAt int64          `json:"started_at,omitempty"` // when every member had joined (unix ms)
	Deposits  []types.Amount `json:"deposits,omitempty"`   // deposit still held per member
	Defaults  []int          `json:"defaults,omitempty"`   // missed payments per member
	PaidOut   []types.Amount `json:"paid_out,omitempty"`   // what each member received
}

// Scheduled reports whether rounds have due dates.
func (p *Pool) Scheduled() bool { return p.RoundSecs > 0 }

// DueAt is when round r's contributions are due (unix ms).
func (p *Pool) DueAt(r int) int64 { return p.StartedAt + int64(r+1)*p.RoundSecs*1000 }

// Held is the total of deposits still held.
func (p *Pool) Held() types.Amount {
	var t types.Amount
	for _, d := range p.Deposits {
		t += d
	}
	return t
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
	c.Deposits = slices.Clone(p.Deposits)
	c.Defaults = slices.Clone(p.Defaults)
	c.PaidOut = slices.Clone(p.PaidOut)
	return &c
}

// Escrow status values.
const (
	EscrowFunded     = "funded"
	EscrowDispatched = "dispatched"
	EscrowDisputed   = "disputed"
	EscrowCompleted  = "completed" // every milestone went to the seller
	EscrowRefunded   = "refunded"  // the buyer got the remainder back
	EscrowResolved   = "resolved"  // the arbiter split the remainder
)

// Escrow is funds locked between a buyer and a seller. Like a pool, its
// balance is held by the ledger and moves only by the escrow rules.
type Escrow struct {
	ID           types.Hash     `json:"id"`
	Ref          string         `json:"ref,omitempty"`
	Asset        string         `json:"asset,omitempty"`
	Buyer        types.Address  `json:"buyer"`
	Seller       types.Address  `json:"seller"`
	Arbiter      types.Address  `json:"arbiter"`
	Milestones   []types.Amount `json:"milestones"`
	Released     int            `json:"released"` // milestones paid to the seller, in order
	Balance      types.Amount   `json:"balance"`
	Status       string         `json:"status"`
	CreatedAt    int64          `json:"created_at"` // block time, unix ms
	ShipBy       int64          `json:"ship_by"`
	ReviewSecs   int64          `json:"review_secs"`
	DispatchedAt int64          `json:"dispatched_at,omitempty"`
	Tracking     string         `json:"tracking,omitempty"`
	PaidSeller   types.Amount   `json:"paid_seller"`
	PaidBuyer    types.Amount   `json:"paid_buyer"`
}

// Open reports whether funds are still locked.
func (e *Escrow) Open() bool {
	return e.Status == EscrowFunded || e.Status == EscrowDispatched || e.Status == EscrowDisputed
}

// Clone returns an independent copy.
func (e *Escrow) Clone() *Escrow { return e.clone() }

func (e *Escrow) clone() *Escrow {
	c := *e
	c.Milestones = slices.Clone(e.Milestones)
	return &c
}

// State is the full ledger. It is not safe for concurrent use; the chain
// package serializes access.
type State struct {
	Accounts    map[types.Address]Account
	Pools       map[types.Hash]*Pool
	Supply      types.Amount // all ORP in accounts and pools; fees are burned from it
	Burned      types.Amount
	MinFee      types.Amount
	AssetDefs   map[string]AssetDef     // fixed at genesis
	AssetSupply map[string]types.Amount // units of each issued asset in existence
	Escrows     map[types.Hash]*Escrow
	Guards      map[types.Address]*Guardianship // social-recovery setups
	RecoveredTo map[types.Address]types.Address // old key -> new key
	// Now is the time rules are evaluated at (unix ms): the block's time
	// while executing a block, the tip's time when checking the mempool.
	Now int64
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
	ErrAsset        = errors.New("asset rule")
	ErrEscrow       = errors.New("escrow rule")
	ErrGuard        = errors.New("recovery rule")
)

// New creates an empty state.
func New(minFee types.Amount) *State {
	return &State{
		Accounts: map[types.Address]Account{}, Pools: map[types.Hash]*Pool{}, MinFee: minFee,
		AssetDefs: map[string]AssetDef{}, AssetSupply: map[string]types.Amount{},
		Escrows: map[types.Hash]*Escrow{},
		Guards:  map[types.Address]*Guardianship{}, RecoveredTo: map[types.Address]types.Address{},
	}
}

// Normalize replaces nil maps after decoding a snapshot.
func (s *State) Normalize() {
	if s.Accounts == nil {
		s.Accounts = map[types.Address]Account{}
	}
	if s.Pools == nil {
		s.Pools = map[types.Hash]*Pool{}
	}
	if s.AssetDefs == nil {
		s.AssetDefs = map[string]AssetDef{}
	}
	if s.AssetSupply == nil {
		s.AssetSupply = map[string]types.Amount{}
	}
	if s.Escrows == nil {
		s.Escrows = map[types.Hash]*Escrow{}
	}
	if s.Guards == nil {
		s.Guards = map[types.Address]*Guardianship{}
	}
	if s.RecoveredTo == nil {
		s.RecoveredTo = map[types.Address]types.Address{}
	}
}

// Escrow returns a copy of an escrow, or nil.
func (s *State) Escrow(id types.Hash) *Escrow {
	if e, ok := s.Escrows[id]; ok {
		return e.clone()
	}
	return nil
}

// Balance returns an address's balance of an asset ("" = ORP).
func (s *State) Balance(a types.Address, asset string) types.Amount {
	acc := s.Accounts[a]
	if asset == "" {
		return acc.Balance
	}
	return acc.Assets[asset]
}

func (s *State) setBalance(a types.Address, asset string, v types.Amount) {
	acc := s.Accounts[a].clone()
	if asset == "" {
		acc.Balance = v
	} else {
		if acc.Assets == nil {
			acc.Assets = map[string]types.Amount{}
		}
		if v == 0 {
			delete(acc.Assets, asset)
		} else {
			acc.Assets[asset] = v
		}
		if len(acc.Assets) == 0 {
			acc.Assets = nil
		}
	}
	s.Accounts[a] = acc
}

func (s *State) credit(a types.Address, asset string, v types.Amount) {
	s.setBalance(a, asset, s.Balance(a, asset)+v)
}

// MinFeeFor returns the minimum fee for txs in an asset.
func (s *State) MinFeeFor(asset string) types.Amount {
	if asset == "" {
		return s.MinFee
	}
	return s.AssetDefs[asset].MinFee
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

// Cost is what a tx will debit from its sender, in tx.Asset: amount plus
// fee, the contribution plus fee for a pool contribution, and nothing for a
// mint.
func (s *State) Cost(tx *types.Tx) types.Amount {
	switch {
	case tx.Kind == types.KindMint:
		return 0
	case tx.Escrow != nil && tx.Escrow.Op == types.EscrowCreate:
		return tx.Escrow.Total() + tx.Fee
	case tx.Pool != nil && tx.Pool.Op == types.PoolContribute:
		if p := s.Pools[tx.Pool.ID]; p != nil {
			return p.Contribution + tx.Fee
		}
	case tx.Pool != nil && tx.Pool.Op == types.PoolCreate:
		return tx.Pool.Deposit + tx.Fee
	case tx.Pool != nil && tx.Pool.Op == types.PoolJoin:
		if p := s.Pools[tx.Pool.ID]; p != nil {
			return p.Deposit + tx.Fee
		}
	}
	return tx.Amount + tx.Fee
}

// Check validates a tx against current state without modifying it. The tx
// must already have passed CheckStateless.
func (s *State) Check(tx *types.Tx) error { return s.apply(tx, false) }

// Apply validates and executes a tx.
func (s *State) Apply(tx *types.Tx) error { return s.apply(tx, true) }

func (s *State) apply(tx *types.Tx, commit bool) error {
	if tx.Asset != "" {
		if _, ok := s.AssetDefs[tx.Asset]; !ok {
			return fmt.Errorf("%w: unknown asset %s", ErrAsset, tx.Asset)
		}
	}
	// Escrow steps after create are free, so a seller with no balance can
	// still record dispatch and an arbiter can still resolve.
	feeFree := tx.Kind == types.KindMint || (tx.Escrow != nil && tx.Escrow.Op != types.EscrowCreate) ||
		(tx.Guard != nil && tx.Guard.Op != types.GuardSet) // guardians need no balance to help
	if !feeFree && tx.Fee < s.MinFeeFor(tx.Asset) {
		return fmt.Errorf("%w: need %s %s", ErrFeeTooLow, types.FormatAmount(s.MinFeeFor(tx.Asset)), types.AssetName(tx.Asset))
	}
	from := s.Accounts[tx.From]
	switch {
	case tx.Nonce < from.Nonce:
		return fmt.Errorf("%w: account nonce is %d", ErrNonceTooLow, from.Nonce)
	case tx.Nonce > from.Nonce:
		return fmt.Errorf("%w: account nonce is %d", ErrNonceTooHigh, from.Nonce)
	}
	cost := s.Cost(tx)
	available := s.Balance(tx.From, tx.Asset)
	if tx.Pool != nil && tx.Pool.Op == types.PoolClaim {
		// The claimer may have spent everything on contributions; let the
		// fee come out of the pot they are receiving.
		if p := s.Pools[tx.Pool.ID]; p != nil && p.Asset == tx.Asset {
			available += p.Balance
		}
	}
	if available < cost {
		return fmt.Errorf("%w: balance %s %s", ErrInsufficient, types.FormatAmount(s.Balance(tx.From, tx.Asset)), types.AssetName(tx.Asset))
	}

	var effect func()
	var err error
	switch {
	case tx.Guard != nil:
		effect, err = s.guardOp(tx)
	case tx.Escrow != nil:
		effect, err = s.escrowOp(tx)
	case tx.Pool != nil:
		effect, err = s.poolOp(tx)
	case tx.Kind == types.KindMint:
		effect, err = s.mint(tx)
	case tx.Kind == types.KindBurn:
		effect, err = s.burn(tx)
	default:
		effect, err = s.transfer(tx)
	}
	if err != nil || !commit {
		return err
	}

	// Credits first (a claim's pot may fund its own fee), then the debit.
	effect()
	s.setBalance(tx.From, tx.Asset, s.Balance(tx.From, tx.Asset)-cost)
	acc := s.Accounts[tx.From].clone()
	acc.Nonce++
	s.Accounts[tx.From] = acc
	if tx.Fee > 0 {
		if tx.Asset == "" {
			s.Supply -= tx.Fee
			s.Burned += tx.Fee
		} else {
			s.credit(s.AssetDefs[tx.Asset].Issuer, tx.Asset, tx.Fee)
		}
	}
	return nil
}

// Each rule validates first and returns the state change as a closure, so
// Check and Apply share one code path and a rejected tx changes nothing.

func (s *State) transfer(tx *types.Tx) (func(), error) {
	if b := s.Balance(tx.To, tx.Asset); b+tx.Amount < b {
		return nil, ErrOverflow
	}
	return func() { s.credit(tx.To, tx.Asset, tx.Amount) }, nil
}

func (s *State) mint(tx *types.Tx) (func(), error) {
	def := s.AssetDefs[tx.Asset]
	if tx.From != def.Issuer {
		return nil, fmt.Errorf("%w: only the %s issuer can mint", ErrAsset, tx.Asset)
	}
	if sup := s.AssetSupply[tx.Asset]; sup+tx.Amount < sup {
		return nil, ErrOverflow
	}
	return func() {
		s.credit(tx.To, tx.Asset, tx.Amount)
		s.AssetSupply[tx.Asset] += tx.Amount
	}, nil
}

// burn destroys the sender's units (the debit happens via Cost).
func (s *State) burn(tx *types.Tx) (func(), error) {
	return func() { s.AssetSupply[tx.Asset] -= tx.Amount }, nil
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
				ID: id, Name: op.Name, Asset: tx.Asset, Creator: tx.From, Members: slices.Clone(op.Members),
				Contribution: op.Contribution, Joined: make([]bool, n), Status: PoolForming,
				Paid: make([]bool, n), Claimed: make([]bool, n),
				RoundSecs: op.RoundSecs, Deposit: op.Deposit,
			}
			if p.Scheduled() {
				p.Deposits, p.Defaults, p.PaidOut = make([]types.Amount, n), make([]int, n), make([]types.Amount, n)
			}
			i := p.index(tx.From)
			p.Joined[i] = true
			if p.Deposits != nil {
				p.Deposits[i] = p.Deposit
			}
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
	if tx.Asset != p.Asset {
		return nil, fmt.Errorf("%w: this pool uses %s", ErrPool, types.AssetName(p.Asset))
	}

	switch op.Op {
	case types.PoolJoin:
		if p.Status != PoolForming || p.Joined[i] {
			return nil, fmt.Errorf("%w: already joined", ErrPool)
		}
		return func() {
			p.Joined[i] = true
			if p.Deposits != nil {
				p.Deposits[i] = p.Deposit
			}
			if !slices.Contains(p.Joined, false) {
				p.Status = PoolActive
				p.StartedAt = s.Now
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
		allPaid := !slices.Contains(p.Paid, false)
		if !allPaid && !(p.Scheduled() && s.Now >= p.DueAt(p.Round)) {
			if p.Scheduled() {
				return nil, fmt.Errorf("%w: waiting for all contributions (due %s)", ErrPool, time.UnixMilli(p.DueAt(p.Round)).UTC().Format(time.RFC3339))
			}
			return nil, fmt.Errorf("%w: waiting for all contributions this round", ErrPool)
		}
		if b := s.Balance(tx.From, p.Asset); b+p.Pot() < b {
			return nil, ErrOverflow
		}
		return func() {
			// Past the due date: record each missed payment and cover it from
			// the member's deposit when there is enough left.
			for j, paid := range p.Paid {
				if paid {
					continue
				}
				p.Defaults[j]++
				if p.Deposits[j] >= p.Contribution {
					p.Deposits[j] -= p.Contribution
					p.Balance += p.Contribution
				}
			}
			pot := p.Balance
			s.credit(tx.From, p.Asset, pot)
			p.Balance = 0
			if p.PaidOut != nil {
				p.PaidOut[i] = pot
			}
			p.Claimed[i] = true
			p.Round++
			clear(p.Paid)
			if p.Round == len(p.Members) {
				p.Status = PoolDone
				// Return whatever deposit each member still has.
				for j, d := range p.Deposits {
					if d > 0 {
						s.credit(p.Members[j], p.Asset, d)
						p.Deposits[j] = 0
					}
				}
			}
		}, nil
	}
	return nil, fmt.Errorf("unknown pool op %q", op.Op)
}

func (s *State) escrowOp(tx *types.Tx) (func(), error) {
	op := tx.Escrow
	if op.Op == types.EscrowCreate {
		id := tx.ID()
		if _, exists := s.Escrows[id]; exists {
			return nil, fmt.Errorf("%w: escrow already exists", ErrEscrow)
		}
		if op.ShipBy <= s.Now {
			return nil, fmt.Errorf("%w: ship-by deadline is in the past", ErrEscrow)
		}
		return func() {
			s.Escrows[id] = &Escrow{
				ID: id, Ref: op.Ref, Asset: tx.Asset, Buyer: tx.From, Seller: op.Seller, Arbiter: op.Arbiter,
				Milestones: slices.Clone(op.Milestones), Balance: op.Total(), Status: EscrowFunded,
				CreatedAt: s.Now, ShipBy: op.ShipBy, ReviewSecs: op.ReviewSecs,
			}
		}, nil
	}

	e := s.Escrows[op.ID]
	if e == nil {
		return nil, fmt.Errorf("%w: no escrow %s", ErrEscrow, op.ID)
	}
	if tx.Asset != e.Asset {
		return nil, fmt.Errorf("%w: this escrow uses %s", ErrEscrow, types.AssetName(e.Asset))
	}
	if !e.Open() {
		return nil, fmt.Errorf("%w: escrow is %s", ErrEscrow, e.Status)
	}
	who := tx.From
	fail := func(msg string) (func(), error) { return nil, fmt.Errorf("%w: %s", ErrEscrow, msg) }
	pay := func(to types.Address, amt types.Amount) {
		if amt == 0 {
			return
		}
		s.credit(to, e.Asset, amt)
		e.Balance -= amt
		if to == e.Seller {
			e.PaidSeller += amt
		} else {
			e.PaidBuyer += amt
		}
	}
	remaining := e.Balance

	switch op.Op {
	case types.EscrowDispatch:
		if who != e.Seller {
			return fail("only the seller can mark dispatch")
		}
		if e.Status != EscrowFunded {
			return fail("already " + e.Status)
		}
		return func() {
			e.Status, e.DispatchedAt, e.Tracking = EscrowDispatched, s.Now, tx.Memo
		}, nil

	case types.EscrowRelease:
		if who != e.Buyer {
			return fail("only the buyer can release funds")
		}
		if e.Status == EscrowDisputed {
			return fail("escrow is under dispute; the arbiter decides")
		}
		return func() {
			pay(e.Seller, e.Milestones[e.Released])
			e.Released++
			if e.Released == len(e.Milestones) {
				e.Status = EscrowCompleted
			}
		}, nil

	case types.EscrowDispute:
		if who != e.Buyer && who != e.Seller {
			return fail("only the buyer or seller can open a dispute")
		}
		if e.Status == EscrowDisputed {
			return fail("already disputed")
		}
		return func() { e.Status = EscrowDisputed }, nil

	case types.EscrowResolve:
		if who != e.Arbiter {
			return fail("only the arbiter can resolve")
		}
		if e.Status != EscrowDisputed {
			return fail("the arbiter can only act on a disputed escrow")
		}
		if op.ToSeller > remaining {
			return fail("to_seller exceeds the escrow balance")
		}
		return func() {
			pay(e.Seller, op.ToSeller)
			pay(e.Buyer, remaining-op.ToSeller)
			e.Status = EscrowResolved
		}, nil

	case types.EscrowRefund:
		switch {
		case who == e.Seller: // a seller may always give the money back
		case who == e.Buyer && e.Status == EscrowFunded && s.Now > e.ShipBy:
		case who == e.Buyer:
			return fail("the buyer can reclaim only if nothing was dispatched by the ship-by deadline")
		default:
			return fail("only the seller, or the buyer after the deadline, can refund")
		}
		return func() {
			pay(e.Buyer, remaining)
			e.Status = EscrowRefunded
		}, nil

	case types.EscrowClaim:
		if who != e.Seller {
			return fail("only the seller can claim")
		}
		if e.Status != EscrowDispatched || s.Now < e.DispatchedAt+e.ReviewSecs*1000 {
			return fail("the buyer's review period has not ended")
		}
		return func() {
			pay(e.Seller, remaining)
			e.Released = len(e.Milestones)
			e.Status = EscrowCompleted
		}, nil
	}
	return nil, fmt.Errorf("unknown escrow op %q", op.Op)
}

// Clone returns an independent copy, used to build and verify blocks
// without touching the committed state.
func (s *State) Clone() *State {
	c := *s
	c.Accounts = make(map[types.Address]Account, len(s.Accounts))
	for k, v := range s.Accounts {
		c.Accounts[k] = v.clone()
	}
	c.AssetSupply = maps.Clone(s.AssetSupply)
	c.Escrows = make(map[types.Hash]*Escrow, len(s.Escrows))
	for k, v := range s.Escrows {
		c.Escrows[k] = v.clone()
	}
	c.Guards = make(map[types.Address]*Guardianship, len(s.Guards))
	for k, v := range s.Guards {
		c.Guards[k] = v.clone()
	}
	c.RecoveredTo = maps.Clone(s.RecoveredTo)
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
		for _, sym := range slices.Sorted(maps.Keys(acc.Assets)) {
			str(sym)
			u64(acc.Assets[sym])
		}
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
		if p.Asset != "" {
			str(p.Asset)
		}
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
		if p.Scheduled() {
			h.Write([]byte("schedule"))
			u64(uint64(p.RoundSecs))
			u64(p.Deposit)
			u64(uint64(p.StartedAt))
			for j := range p.Members {
				u64(p.Deposits[j])
				u64(uint64(p.Defaults[j]))
				u64(p.PaidOut[j])
			}
		}
	}

	eids := slices.SortedFunc(maps.Keys(s.Escrows), func(a, b types.Hash) int { return slices.Compare(a[:], b[:]) })
	for _, id := range eids {
		e := s.Escrows[id]
		h.Write([]byte("escrow"))
		h.Write(id[:])
		str(e.Ref)
		str(e.Asset)
		str(string(e.Buyer))
		str(string(e.Seller))
		str(string(e.Arbiter))
		u64(uint64(len(e.Milestones)))
		for _, m := range e.Milestones {
			u64(m)
		}
		u64(uint64(e.Released))
		u64(e.Balance)
		str(e.Status)
		u64(uint64(e.CreatedAt))
		u64(uint64(e.ShipBy))
		u64(uint64(e.ReviewSecs))
		u64(uint64(e.DispatchedAt))
		str(e.Tracking)
		u64(e.PaidSeller)
		u64(e.PaidBuyer)
	}

	for _, a := range slices.Sorted(maps.Keys(s.Guards)) {
		g := s.Guards[a]
		h.Write([]byte("guard"))
		str(string(a))
		for _, x := range g.Guardians {
			str(string(x))
		}
		u64(uint64(g.Threshold))
		u64(uint64(g.DelaySecs))
		if p := g.Pending; p != nil {
			str(string(p.NewOwner))
			for _, x := range p.Approvals {
				str(string(x))
			}
			u64(uint64(p.StartedAt))
			u64(uint64(p.ReadyAt))
		}
	}
	for _, a := range slices.Sorted(maps.Keys(s.RecoveredTo)) {
		h.Write([]byte("moved"))
		str(string(a))
		str(string(s.RecoveredTo[a]))
	}

	u64(s.Supply)
	u64(s.Burned)
	for _, sym := range slices.Sorted(maps.Keys(s.AssetSupply)) {
		if s.AssetSupply[sym] != 0 {
			str(sym)
			u64(s.AssetSupply[sym])
		}
	}
	var out types.Hash
	copy(out[:], h.Sum(nil))
	return out
}
