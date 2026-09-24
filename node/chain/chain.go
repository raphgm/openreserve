// Package chain ties together the ledger state, the block log and the
// mempool, and enforces the rules for producing and accepting blocks.
package chain

import (
	"crypto/ed25519"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/openreserve/node/ledger"
	"github.com/openreserve/node/types"
)

const (
	MaxBlockTxs   = 1000
	MaxMempoolTxs = 10_000
	MaxNonceAhead = 16 // how far past the account nonce a pending tx may be
	MempoolTTL    = 10 * time.Minute
	blockLogName  = "blocks.jsonl"
)

// ErrDuplicate is returned when a tx is already pending or committed.
var ErrDuplicate = errors.New("duplicate transaction")

type pendingTx struct {
	tx    *types.Tx
	added time.Time
}

// TxLocation says where a committed tx lives.
type TxLocation struct {
	Height uint64 `json:"height"`
	Index  int    `json:"index"`
}

// Chain is the node's view of the ledger. All methods are safe for concurrent use.
type Chain struct {
	mu       sync.RWMutex
	genesis  *Genesis
	state    *ledger.State
	blocks   []*types.Block // blocks[i] has height i+1
	txIndex  map[types.Hash]TxLocation
	byAddr   map[types.Address][]TxLocation // txs touching each address, oldest first
	byPool   map[types.Hash][]TxLocation    // txs for each pool, oldest first
	byEscrow map[types.Hash][]TxLocation    // txs for each escrow, oldest first
	mempool  map[types.Hash]pendingTx
	log      *blockLog
	notify   chan struct{} // closed and replaced whenever a block is committed
	// Clock returns the current time in unix ms; tests replace it.
	Clock func() int64
	// SnapshotEvery sets how often the state is snapshotted (0 disables).
	SnapshotEvery int
	// ReplayedFrom is the height startup replay began after (0 = genesis).
	ReplayedFrom uint64
	dataDir      string
}

// Open loads the chain from dataDir, replaying and re-verifying every block.
func Open(dataDir string, g *Genesis) (*Chain, error) {
	st, err := g.State()
	if err != nil {
		return nil, err
	}
	log, stored, err := openBlockLog(filepath.Join(dataDir, blockLogName))
	if err != nil {
		return nil, err
	}
	c := &Chain{
		genesis:  g,
		state:    st,
		txIndex:  map[types.Hash]TxLocation{},
		byAddr:   map[types.Address][]TxLocation{},
		byPool:   map[types.Hash][]TxLocation{},
		byEscrow: map[types.Hash][]TxLocation{},
		mempool:  map[types.Hash]pendingTx{},
		notify:   make(chan struct{}),
		Clock:    func() int64 { return time.Now().UnixMilli() },
		dataDir:  dataDir, SnapshotEvery: DefaultSnapshotEvery,
	}
	// Start from a verified snapshot when there is one: blocks up to it are
	// only indexed, not re-executed.
	snap, err := loadSnapshot(dataDir, stored)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ignoring snapshot, replaying from genesis: %v\n", err)
	}
	for _, b := range stored {
		if snap != nil && b.Header.Height <= snap.Height {
			c.blocks = append(c.blocks, b)
			c.index(b)
			if b.Header.Height == snap.Height {
				c.state, c.ReplayedFrom = snap.State, snap.Height
			}
			continue
		}
		next, err := c.verify(b)
		if err != nil {
			log.close()
			return nil, fmt.Errorf("replay block %d: %w", b.Header.Height, err)
		}
		c.commit(b, next)
	}
	c.log = log
	return c, nil
}

// Close releases the block log.
func (c *Chain) Close() error { return c.log.close() }

// Genesis returns the chain's genesis document.
func (c *Chain) Genesis() *Genesis { return c.genesis }

// Submit validates a tx and adds it to the mempool.
func (c *Chain) Submit(tx *types.Tx) (types.Hash, error) {
	if err := tx.CheckStateless(c.genesis.ChainID); err != nil {
		return types.Hash{}, err
	}
	id := tx.ID()
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, ok := c.mempool[id]; ok {
		return id, ErrDuplicate
	}
	if _, ok := c.txIndex[id]; ok {
		return id, ErrDuplicate
	}
	if len(c.mempool) >= MaxMempoolTxs {
		return id, errors.New("mempool full, retry later")
	}
	acc := c.state.Get(tx.From)
	if tx.Nonce >= acc.Nonce+MaxNonceAhead {
		return id, fmt.Errorf("%w: account nonce is %d", ledger.ErrNonceTooHigh, acc.Nonce)
	}
	c.state.Now = max(c.Clock(), c.tipLocked().Time+1)
	if err := c.state.Check(tx); err != nil && !errors.Is(err, ledger.ErrNonceTooHigh) {
		return id, err
	}
	// Future-nonce txs skip the balance check above, so require the sender to
	// hold enough committed funds for everything it has pending. This keeps
	// unfunded keys from filling the mempool.
	need := c.state.Cost(tx)
	for _, p := range c.mempool {
		if p.tx.From != tx.From {
			continue
		}
		if p.tx.Nonce == tx.Nonce {
			return id, fmt.Errorf("a pending tx already uses nonce %d", tx.Nonce)
		}
		if p.tx.Asset == tx.Asset {
			need += c.state.Cost(p.tx)
		}
	}
	if tx.Pool == nil || tx.Pool.Op != types.PoolClaim { // claims are funded by the pot
		if have := c.state.Balance(tx.From, tx.Asset); need > have {
			return id, fmt.Errorf("%w: pending txs need %s %s", ledger.ErrInsufficient, types.FormatAmount(need), types.AssetName(tx.Asset))
		}
	}
	c.mempool[id] = pendingTx{tx: tx, added: time.Now()}
	return id, nil
}

// Produce builds, signs and commits the next block from the mempool. It
// returns nil if there is nothing to include. Only the genesis proposer's
// key produces blocks other nodes will accept.
func (c *Chain) Produce(key ed25519.PrivateKey, nowMs int64) (*types.Block, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	pending := make([]*types.Tx, 0, len(c.mempool))
	for id, p := range c.mempool {
		if time.Since(p.added) > MempoolTTL {
			delete(c.mempool, id)
			continue
		}
		pending = append(pending, p.tx)
	}
	sort.Slice(pending, func(i, j int) bool {
		if pending[i].From != pending[j].From {
			return pending[i].From < pending[j].From
		}
		return pending[i].Nonce < pending[j].Nonce
	})

	tip := c.tipLocked()
	if nowMs <= tip.Time {
		nowMs = tip.Time + 1
	}
	next := c.state.Clone()
	next.Now = nowMs
	var txs []*types.Tx
	for _, tx := range pending {
		if len(txs) == MaxBlockTxs {
			break
		}
		err := next.Apply(tx)
		switch {
		case err == nil:
			txs = append(txs, tx)
		case errors.Is(err, ledger.ErrNonceTooHigh):
			// Waiting on an earlier nonce; keep it.
		default:
			delete(c.mempool, tx.ID()) // can no longer succeed as submitted
		}
	}
	if len(txs) == 0 {
		return nil, nil
	}

	b := &types.Block{
		Header: types.Header{
			ChainID:   c.genesis.ChainID,
			Height:    tip.Height + 1,
			PrevHash:  tip.Hash,
			Time:      nowMs,
			TxRoot:    types.TxRoot(txs),
			StateRoot: next.Root(),
			Proposer:  types.AddressFromPubKey(key.Public().(ed25519.PublicKey)),
		},
		Txs: txs,
	}
	b.Sign(key)
	if _, err := c.verify(b); err != nil {
		return nil, fmt.Errorf("produced invalid block: %w", err)
	}
	if err := c.log.append(b); err != nil {
		return nil, err
	}
	c.commit(b, next)
	return b, nil
}

// AddBlock verifies a block from another node and commits it.
func (c *Chain) AddBlock(b *types.Block) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	next, err := c.verify(b)
	if err != nil {
		return err
	}
	if err := c.log.append(b); err != nil {
		return err
	}
	c.commit(b, next)
	return nil
}

type tipInfo struct {
	Height uint64
	Hash   types.Hash
	Time   int64
}

func (c *Chain) tipLocked() tipInfo {
	if len(c.blocks) == 0 {
		return tipInfo{0, c.genesis.Hash(), c.genesis.Time}
	}
	h := c.blocks[len(c.blocks)-1].Header
	return tipInfo{h.Height, h.Hash(), h.Time}
}

// verify checks b against the current tip and returns the resulting state.
func (c *Chain) verify(b *types.Block) (*ledger.State, error) {
	h := b.Header
	tip := c.tipLocked()
	bft := c.genesis.BFT()
	switch {
	case h.ChainID != c.genesis.ChainID:
		return nil, fmt.Errorf("wrong chain_id %q", h.ChainID)
	case h.Height != tip.Height+1:
		return nil, fmt.Errorf("height %d does not follow tip %d", h.Height, tip.Height)
	case h.PrevHash != tip.Hash:
		return nil, errors.New("prev_hash does not match tip")
	case h.Time < tip.Time || (!bft && h.Time == tip.Time):
		return nil, errors.New("block time must increase")
	case !bft && h.Proposer != c.genesis.Proposer:
		return nil, fmt.Errorf("proposer %s is not authorized", h.Proposer)
	case len(b.Txs) > MaxBlockTxs || (!bft && len(b.Txs) == 0):
		return nil, fmt.Errorf("block must contain 1..%d txs", MaxBlockTxs)
	case h.TxRoot != types.TxRoot(b.Txs):
		return nil, errors.New("tx_root mismatch")
	}
	// Under CometBFT the validator set already agreed on the block (2/3+ of
	// voting power signed it); there is no single proposer signature.
	if !bft {
		if err := b.VerifySig(); err != nil {
			return nil, err
		}
	}
	next := c.state.Clone()
	next.Now = h.Time
	for i, tx := range b.Txs {
		if err := tx.CheckStateless(c.genesis.ChainID); err != nil {
			return nil, fmt.Errorf("tx %d: %w", i, err)
		}
		if err := next.Apply(tx); err != nil {
			return nil, fmt.Errorf("tx %d: %w", i, err)
		}
	}
	if next.Root() != h.StateRoot {
		return nil, errors.New("state_root mismatch")
	}
	return next, nil
}

func (c *Chain) commit(b *types.Block, next *ledger.State) {
	c.state = next
	c.blocks = append(c.blocks, b)
	c.index(b)
	// Drop pending txs whose nonce was consumed by this block.
	for id, p := range c.mempool {
		if p.tx.Nonce < c.state.Get(p.tx.From).Nonce {
			delete(c.mempool, id)
		}
	}
	if err := c.maybeSnapshot(); err != nil {
		fmt.Fprintf(os.Stderr, "snapshot at height %d failed: %v\n", b.Header.Height, err)
	}
	close(c.notify)
	c.notify = make(chan struct{})
}

// index records a committed block's txs in the lookup indexes.
func (c *Chain) index(b *types.Block) {
	for i, tx := range b.Txs {
		id := tx.ID()
		loc := TxLocation{Height: b.Header.Height, Index: i}
		c.txIndex[id] = loc
		c.byAddr[tx.From] = append(c.byAddr[tx.From], loc)
		if tx.To != "" {
			c.byAddr[tx.To] = append(c.byAddr[tx.To], loc)
		}
		if x := tx.Escrow; x != nil {
			eid := x.ID
			if x.Op == types.EscrowCreate {
				eid = id
				c.byAddr[x.Seller] = append(c.byAddr[x.Seller], loc)
				c.byAddr[x.Arbiter] = append(c.byAddr[x.Arbiter], loc)
			}
			c.byEscrow[eid] = append(c.byEscrow[eid], loc)
		}
		if g := tx.Guard; g != nil && g.Account != "" {
			if g.Account != tx.From {
				c.byAddr[g.Account] = append(c.byAddr[g.Account], loc)
			}
			if g.NewOwner != "" {
				c.byAddr[g.NewOwner] = append(c.byAddr[g.NewOwner], loc)
			}
		}
		if tx.Pool != nil {
			pid := tx.Pool.ID
			if tx.Pool.Op == types.PoolCreate {
				pid = id
			}
			c.byPool[pid] = append(c.byPool[pid], loc)
		}
		delete(c.mempool, id)
	}
}

// Status summarizes the chain.
type Status struct {
	ChainID     string        `json:"chain_id"`
	Height      uint64        `json:"height"`
	TipHash     types.Hash    `json:"tip_hash"`
	TipTime     int64         `json:"tip_time"`
	StateRoot   types.Hash    `json:"state_root"`
	Supply      types.Amount  `json:"supply"`
	Burned      types.Amount  `json:"burned"`
	MinFee      types.Amount  `json:"min_fee"`
	Assets      []AssetStatus `json:"assets"`
	Accounts    int           `json:"accounts"`
	MempoolSize int           `json:"mempool_size"`
	Proposer    types.Address `json:"proposer"`
}

func (c *Chain) Status() Status {
	c.mu.RLock()
	defer c.mu.RUnlock()
	tip := c.tipLocked()
	return Status{
		ChainID: c.genesis.ChainID, Height: tip.Height, TipHash: tip.Hash, TipTime: tip.Time,
		StateRoot: c.state.Root(), Supply: c.state.Supply, Burned: c.state.Burned,
		MinFee: c.state.MinFee, Accounts: len(c.state.Accounts), MempoolSize: len(c.mempool),
		Assets:   c.assetStatus(),
		Proposer: c.genesis.Proposer,
	}
}

// Height returns the current tip height.
func (c *Chain) Height() uint64 {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return uint64(len(c.blocks))
}

// Account returns committed state for an address plus the next nonce the
// sender should use, accounting for its pending txs.
func (c *Chain) Account(a types.Address) (acc ledger.Account, nextNonce uint64) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	acc = c.state.Get(a)
	nextNonce = acc.Nonce
	for {
		found := false
		for _, p := range c.mempool {
			if p.tx.From == a && p.tx.Nonce == nextNonce {
				found = true
				break
			}
		}
		if !found {
			return acc, nextNonce
		}
		nextNonce++
	}
}

// Block returns the block at height (1-based), or nil.
func (c *Chain) Block(height uint64) *types.Block {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if height == 0 || height > uint64(len(c.blocks)) {
		return nil
	}
	return c.blocks[height-1]
}

// Blocks returns up to limit blocks starting at height from.
func (c *Chain) Blocks(from uint64, limit int) []*types.Block {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if from == 0 {
		from = 1
	}
	var out []*types.Block
	for h := from; h <= uint64(len(c.blocks)) && len(out) < limit; h++ {
		out = append(out, c.blocks[h-1])
	}
	return out
}

// Tx looks up a tx by id. Pending txs return a nil location.
func (c *Chain) Tx(id types.Hash) (*types.Tx, *TxLocation) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if loc, ok := c.txIndex[id]; ok {
		return c.blocks[loc.Height-1].Txs[loc.Index], &loc
	}
	if p, ok := c.mempool[id]; ok {
		return p.tx, nil
	}
	return nil, nil
}

// History returns committed txs touching an address, newest first. It
// reads from a per-address index, so cost is independent of chain length.
func (c *Chain) History(a types.Address, limit int) []HistoryEntry {
	c.mu.RLock()
	defer c.mu.RUnlock()
	locs := c.byAddr[a]
	out := []HistoryEntry{}
	for i := len(locs) - 1; i >= 0 && len(out) < limit; i-- {
		b := c.blocks[locs[i].Height-1]
		tx := b.Txs[locs[i].Index]
		out = append(out, HistoryEntry{Tx: tx, ID: tx.ID(), Height: b.Header.Height, Time: b.Header.Time})
	}
	return out
}

// HistoryEntry is a committed tx with its position.
type HistoryEntry struct {
	ID     types.Hash `json:"id"`
	Height uint64     `json:"height"`
	Time   int64      `json:"time"`
	Tx     *types.Tx  `json:"tx"`
}

// Updated returns a channel closed on the next committed block.
func (c *Chain) Updated() <-chan struct{} {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.notify
}

// Pool returns a pool's current state, or nil.
func (c *Chain) Pool(id types.Hash) *ledger.Pool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.state.Pool(id)
}

// PoolHistory returns every committed tx for a pool, oldest first: the full
// audit trail of joins, contributions and claims.
func (c *Chain) PoolHistory(id types.Hash) []HistoryEntry {
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := []HistoryEntry{}
	for _, loc := range c.byPool[id] {
		b := c.blocks[loc.Height-1]
		tx := b.Txs[loc.Index]
		out = append(out, HistoryEntry{Tx: tx, ID: tx.ID(), Height: b.Header.Height, Time: b.Header.Time})
	}
	return out
}

// PoolsOf returns the pools an address is a member of.
func (c *Chain) PoolsOf(a types.Address) []*ledger.Pool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := []*ledger.Pool{}
	for _, p := range c.state.Pools {
		if slices.Contains(p.Members, a) {
			out = append(out, c.state.Pool(p.ID))
		}
	}
	slices.SortFunc(out, func(x, y *ledger.Pool) int { return strings.Compare(x.Name, y.Name) })
	return out
}

// AssetStatus is an issued asset's definition and current supply. For a
// backed asset, Supply must never exceed the issuer's real-money reserve.
type AssetStatus struct {
	ledger.AssetDef
	Supply types.Amount `json:"supply"`
}

func (c *Chain) assetStatus() []AssetStatus {
	out := []AssetStatus{}
	for _, sym := range slices.Sorted(maps.Keys(c.state.AssetDefs)) {
		out = append(out, AssetStatus{AssetDef: c.state.AssetDefs[sym], Supply: c.state.AssetSupply[sym]})
	}
	return out
}

// Escrow returns an escrow's state, or nil.
func (c *Chain) Escrow(id types.Hash) *ledger.Escrow {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.state.Escrow(id)
}

// EscrowHistory returns every committed tx for an escrow, oldest first.
func (c *Chain) EscrowHistory(id types.Hash) []HistoryEntry {
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := []HistoryEntry{}
	for _, loc := range c.byEscrow[id] {
		b := c.blocks[loc.Height-1]
		tx := b.Txs[loc.Index]
		out = append(out, HistoryEntry{Tx: tx, ID: tx.ID(), Height: b.Header.Height, Time: b.Header.Time})
	}
	return out
}

// EscrowsOf returns escrows where a is buyer, seller or arbiter, newest first.
func (c *Chain) EscrowsOf(a types.Address) []*ledger.Escrow {
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := []*ledger.Escrow{}
	for _, e := range c.state.Escrows {
		if e.Buyer == a || e.Seller == a || e.Arbiter == a {
			out = append(out, e.Clone())
		}
	}
	slices.SortFunc(out, func(x, y *ledger.Escrow) int { return int(y.CreatedAt - x.CreatedAt) })
	return out
}

// Counts summarizes pools and escrows for metrics.
type Counts struct {
	TxsTotal       int
	Pools          int
	PoolsActive    int
	Escrows        int
	EscrowsOpen    int
	EscrowsDispute int
	EscrowLocked   map[string]types.Amount // by asset ("" = ORP)
}

func (c *Chain) Counts() Counts {
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := Counts{TxsTotal: len(c.txIndex), Pools: len(c.state.Pools), Escrows: len(c.state.Escrows), EscrowLocked: map[string]types.Amount{}}
	for _, p := range c.state.Pools {
		if p.Status == ledger.PoolActive {
			out.PoolsActive++
		}
	}
	for _, e := range c.state.Escrows {
		if e.Open() {
			out.EscrowsOpen++
			out.EscrowLocked[e.Asset] += e.Balance
		}
		if e.Status == ledger.EscrowDisputed {
			out.EscrowsDispute++
		}
	}
	return out
}

// ExecuteDecided runs the txs of a block decided by CometBFT at the given
// height and time. Unlike Produce it never rejects the block: txs that fail
// (e.g. a nonce already used) are skipped, identically on every node, and
// their errors are returned by position. The result is committed with
// CommitDecided once CometBFT commits.
func (c *Chain) ExecuteDecided(height uint64, timeMs int64, proposer string, txs []*types.Tx) (*types.Block, *ledger.State, []error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	tip := c.tipLocked()
	if timeMs < tip.Time {
		timeMs = tip.Time
	}
	next := c.state.Clone()
	next.Now = timeMs
	errs := make([]error, len(txs))
	var ok []*types.Tx
	for i, tx := range txs {
		switch {
		case tx == nil:
			errs[i] = errors.New("malformed transaction")
		case len(ok) == MaxBlockTxs:
			errs[i] = errors.New("block full")
		default:
			if err := tx.CheckStateless(c.genesis.ChainID); err != nil {
				errs[i] = err
			} else if err := next.Apply(tx); err != nil {
				errs[i] = err
			} else {
				ok = append(ok, tx)
			}
		}
	}
	b := &types.Block{
		Header: types.Header{
			ChainID: c.genesis.ChainID, Height: height, PrevHash: tip.Hash, Time: timeMs,
			TxRoot: types.TxRoot(ok), StateRoot: next.Root(), Proposer: types.Address(proposer),
		},
		Txs: ok,
	}
	if b.Txs == nil {
		b.Txs = []*types.Tx{}
	}
	return b, next, errs
}

// CommitDecided persists a block produced by ExecuteDecided.
func (c *Chain) CommitDecided(b *types.Block, next *ledger.State) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if b.Header.Height != uint64(len(c.blocks))+1 {
		return fmt.Errorf("commit height %d does not follow %d", b.Header.Height, len(c.blocks))
	}
	if err := c.log.append(b); err != nil {
		return err
	}
	c.commit(b, next)
	return nil
}

// CheckPending validates a tx for the shared mempool without adding it:
// future nonces (up to MaxNonceAhead) are allowed.
func (c *Chain) CheckPending(tx *types.Tx) error {
	if err := tx.CheckStateless(c.genesis.ChainID); err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	acc := c.state.Get(tx.From)
	if tx.Nonce >= acc.Nonce+MaxNonceAhead {
		return fmt.Errorf("%w: account nonce is %d", ledger.ErrNonceTooHigh, acc.Nonce)
	}
	c.state.Now = max(c.Clock(), c.tipLocked().Time+1)
	if err := c.state.Check(tx); err != nil && !errors.Is(err, ledger.ErrNonceTooHigh) {
		return err
	}
	return nil
}

// Forget drops a pending tx (e.g. the shared mempool refused it).
func (c *Chain) Forget(id types.Hash) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.mempool, id)
}

// StateRoot returns the committed state root.
func (c *Chain) StateRoot() types.Hash {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.state.Root()
}

// RecoveryInfo is an account's social-recovery status.
type RecoveryInfo struct {
	Guardianship *ledger.Guardianship `json:"guardianship"`
	RecoveredTo  types.Address        `json:"recovered_to,omitempty"`
	Guarding     []types.Address      `json:"guarding"` // accounts that name this address as guardian
	Requests     []GuardRequest       `json:"requests"` // pending recoveries this address can act on as guardian
}

// GuardRequest is a pending recovery a guardian may approve.
type GuardRequest struct {
	Account   types.Address `json:"account"`
	NewOwner  types.Address `json:"new_owner"`
	Approvals int           `json:"approvals"`
	Threshold int           `json:"threshold"`
	Approved  bool          `json:"approved"`
	ReadyAt   int64         `json:"ready_at,omitempty"`
}

func (c *Chain) Recovery(a types.Address) RecoveryInfo {
	c.mu.RLock()
	defer c.mu.RUnlock()
	info := RecoveryInfo{Guardianship: c.state.Guardianship(a), RecoveredTo: c.state.RecoveredTo[a], Guarding: []types.Address{}, Requests: []GuardRequest{}}
	for _, acct := range c.state.GuardedBy(a) {
		info.Guarding = append(info.Guarding, acct)
		if g := c.state.Guards[acct]; g.Pending != nil {
			info.Requests = append(info.Requests, GuardRequest{
				Account: acct, NewOwner: g.Pending.NewOwner, Approvals: len(g.Pending.Approvals), Threshold: g.Threshold,
				Approved: slices.Contains(g.Pending.Approvals, a), ReadyAt: g.Pending.ReadyAt,
			})
		}
	}
	return info
}
