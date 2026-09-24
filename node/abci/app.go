// Package abci connects the OpenReserve ledger to CometBFT. CometBFT runs
// consensus and peer-to-peer networking among validators; this Application
// executes the blocks they agree on against the existing chain, so every
// API, index and snapshot keeps working. Validators also agree on the state
// root (the app hash) after each block, so a node that computes a different
// state is detected immediately.
package abci

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sync"

	abcitypes "github.com/cometbft/cometbft/abci/types"

	"github.com/openreserve/node/chain"
	"github.com/openreserve/node/ledger"
	"github.com/openreserve/node/types"
)

const (
	codeOK       = 0
	codeBadTx    = 1
	codeRejected = 2
	maxTxBytes   = 64 << 10
)

type Application struct {
	abcitypes.BaseApplication
	c *chain.Chain

	mu      sync.Mutex
	pending *decided // executed in FinalizeBlock, persisted in Commit
}

type decided struct {
	block *types.Block
	state *ledger.State
}

func New(c *chain.Chain) *Application { return &Application{c: c} }

// DecodeTx parses a transaction as submitted to the network (JSON).
func DecodeTx(b []byte) (*types.Tx, error) {
	if len(b) > maxTxBytes {
		return nil, fmt.Errorf("transaction too large")
	}
	var tx types.Tx
	if err := json.Unmarshal(b, &tx); err != nil {
		return nil, fmt.Errorf("malformed transaction: %w", err)
	}
	return &tx, nil
}

// Info tells CometBFT how far this app has committed, so on restart it
// replays only the blocks the app has not stored yet.
func (a *Application) Info(context.Context, *abcitypes.RequestInfo) (*abcitypes.ResponseInfo, error) {
	h := a.c.Height()
	resp := &abcitypes.ResponseInfo{Data: "openreserve", Version: "1", LastBlockHeight: int64(h)}
	if h > 0 {
		root := a.c.StateRoot()
		resp.LastBlockAppHash = root[:]
	}
	return resp, nil
}

func (a *Application) CheckTx(_ context.Context, req *abcitypes.RequestCheckTx) (*abcitypes.ResponseCheckTx, error) {
	tx, err := DecodeTx(req.Tx)
	if err != nil {
		return &abcitypes.ResponseCheckTx{Code: codeBadTx, Log: err.Error()}, nil
	}
	if err := a.c.CheckPending(tx); err != nil {
		return &abcitypes.ResponseCheckTx{Code: codeRejected, Log: err.Error()}, nil
	}
	return &abcitypes.ResponseCheckTx{Code: codeOK}, nil
}

// PrepareProposal keeps the mempool order (senders' nonces stay in order)
// within the size limit CometBFT gives.
func (a *Application) PrepareProposal(_ context.Context, req *abcitypes.RequestPrepareProposal) (*abcitypes.ResponsePrepareProposal, error) {
	var out [][]byte
	var size int64
	for _, tx := range req.Txs {
		if size+int64(len(tx)) > req.MaxTxBytes || len(out) == chain.MaxBlockTxs {
			break
		}
		out = append(out, tx)
		size += int64(len(tx))
	}
	return &abcitypes.ResponsePrepareProposal{Txs: out}, nil
}

// ProcessProposal accepts any proposal: invalid txs are skipped
// deterministically at execution rather than failing the round.
func (a *Application) ProcessProposal(context.Context, *abcitypes.RequestProcessProposal) (*abcitypes.ResponseProcessProposal, error) {
	return &abcitypes.ResponseProcessProposal{Status: abcitypes.ResponseProcessProposal_ACCEPT}, nil
}

func (a *Application) FinalizeBlock(_ context.Context, req *abcitypes.RequestFinalizeBlock) (*abcitypes.ResponseFinalizeBlock, error) {
	txs := make([]*types.Tx, len(req.Txs))
	decodeErrs := make([]error, len(req.Txs))
	for i, raw := range req.Txs {
		txs[i], decodeErrs[i] = DecodeTx(raw)
	}
	b, next, errs := a.c.ExecuteDecided(uint64(req.Height), req.Time.UnixMilli(), hex.EncodeToString(req.ProposerAddress), txs)
	results := make([]*abcitypes.ExecTxResult, len(req.Txs))
	for i := range req.Txs {
		switch {
		case decodeErrs[i] != nil:
			results[i] = &abcitypes.ExecTxResult{Code: codeBadTx, Log: decodeErrs[i].Error()}
		case errs[i] != nil:
			results[i] = &abcitypes.ExecTxResult{Code: codeRejected, Log: errs[i].Error()}
		default:
			id := txs[i].ID()
			results[i] = &abcitypes.ExecTxResult{Code: codeOK, Data: id[:]}
		}
	}
	a.mu.Lock()
	a.pending = &decided{block: b, state: next}
	a.mu.Unlock()
	root := b.Header.StateRoot
	return &abcitypes.ResponseFinalizeBlock{TxResults: results, AppHash: root[:]}, nil
}

func (a *Application) Commit(context.Context, *abcitypes.RequestCommit) (*abcitypes.ResponseCommit, error) {
	a.mu.Lock()
	p := a.pending
	a.pending = nil
	a.mu.Unlock()
	if p == nil {
		return &abcitypes.ResponseCommit{}, nil
	}
	if err := a.c.CommitDecided(p.block, p.state); err != nil {
		// The state CometBFT agreed on could not be stored: stop rather than diverge.
		return nil, fmt.Errorf("commit block %d: %w", p.block.Header.Height, err)
	}
	return &abcitypes.ResponseCommit{}, nil
}
