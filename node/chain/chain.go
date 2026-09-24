// Package chain ties together the ledger state, the block log and the
// mempool, and enforces the rules for producing and accepting blocks.
package chain

import (
	"crypto/ed25519"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
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
	mu      sync.RWMutex
	genesis *Genesis
	state   *ledger.State
	blocks  []*types.Block // blocks[i] has height i+1
	txIndex map[types.Hash]TxLocation
	mempool map[types.Hash]pendingTx
	log     *blockLog
	notify  chan struct{} // closed and replaced whenever a block is committed
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
		genesis: g,
		state:   st,
		txIndex: map[types.Hash]TxLocation{},
		mempool: map[types.Hash]pendingTx{},
		notify:  make(chan struct{}),
	}
	for _, b := range stored {
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
	if err := c.state.Check(tx); err != nil && !errors.Is(err, ledger.ErrNonceTooHigh) {
		return id, err
	}
	// Future-nonce txs skip the balance check above, so require the sender to
	// hold enough committed funds for everything it has pending. This keeps
	// unfunded keys from filling the mempool.
	need := tx.Amount + tx.Fee
	for _, p := range c.mempool {
		if p.tx.From != tx.From {
			continue
		}
		if p.tx.Nonce == tx.Nonce {
			return id, fmt.Errorf("a pending tx already uses nonce %d", tx.Nonce)
		}
		need += p.tx.Amount + p.tx.Fee
	}
	if need > acc.Balance {
		return id, fmt.Errorf("%w: pending txs need %s ORP", ledger.ErrInsufficient, types.FormatAmount(need))
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

	next := c.state.Clone()
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

	tip := c.tipLocked()
	if nowMs <= tip.Time {
		nowMs = tip.Time + 1
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
	switch {
	case h.ChainID != c.genesis.ChainID:
		return nil, fmt.Errorf("wrong chain_id %q", h.ChainID)
	case h.Height != tip.Height+1:
		return nil, fmt.Errorf("height %d does not follow tip %d", h.Height, tip.Height)
	case h.PrevHash != tip.Hash:
		return nil, errors.New("prev_hash does not match tip")
	case h.Time <= tip.Time:
		return nil, errors.New("block time must increase")
	case h.Proposer != c.genesis.Proposer:
		return nil, fmt.Errorf("proposer %s is not authorized", h.Proposer)
	case len(b.Txs) == 0 || len(b.Txs) > MaxBlockTxs:
		return nil, fmt.Errorf("block must contain 1..%d txs", MaxBlockTxs)
	case h.TxRoot != types.TxRoot(b.Txs):
		return nil, errors.New("tx_root mismatch")
	}
	if err := b.VerifySig(); err != nil {
		return nil, err
	}
	next := c.state.Clone()
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
	for i, tx := range b.Txs {
		id := tx.ID()
		c.txIndex[id] = TxLocation{Height: b.Header.Height, Index: i}
		delete(c.mempool, id)
	}
	// Drop pending txs whose nonce was consumed by this block.
	for id, p := range c.mempool {
		if p.tx.Nonce < c.state.Get(p.tx.From).Nonce {
			delete(c.mempool, id)
		}
	}
	close(c.notify)
	c.notify = make(chan struct{})
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

// History returns committed txs touching an address, newest first.
func (c *Chain) History(a types.Address, limit int) []HistoryEntry {
	c.mu.RLock()
	defer c.mu.RUnlock()
	var out []HistoryEntry
	for i := len(c.blocks) - 1; i >= 0 && len(out) < limit; i-- {
		b := c.blocks[i]
		for j := len(b.Txs) - 1; j >= 0 && len(out) < limit; j-- {
			if tx := b.Txs[j]; tx.From == a || tx.To == a {
				out = append(out, HistoryEntry{Tx: tx, ID: tx.ID(), Height: b.Header.Height, Time: b.Header.Time})
			}
		}
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
