package chain

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"

	"github.com/openreserve/node/ledger"
	"github.com/openreserve/node/types"
)

// Genesis defines the initial state and the parameters every node must agree on.
type Genesis struct {
	ChainID string `json:"chain_id"`
	Time    int64  `json:"time"` // unix milliseconds
	// Consensus is "" for a single authorized proposer, or "cometbft" when a
	// CometBFT validator set decides blocks (then Proposer is unused).
	Consensus string `json:"consensus,omitempty"`
	// Proposer is the single authority allowed to produce blocks when
	// Consensus is "".
	Proposer    types.Address `json:"proposer,omitempty"`
	MinFee      types.Amount  `json:"min_fee"`
	Allocations []Allocation  `json:"allocations"`
	// Assets are issued currencies (e.g. NGN backed by naira collected via
	// Paystack). Only each asset's issuer can mint it.
	Assets []ledger.AssetDef `json:"assets,omitempty"`
}

// Allocation credits an address at genesis.
type Allocation struct {
	Address types.Address `json:"address"`
	Amount  types.Amount  `json:"amount"`
}

// ConsensusCometBFT marks a chain whose blocks are decided by CometBFT.
const ConsensusCometBFT = "cometbft"

// BFT reports whether blocks come from CometBFT consensus.
func (g *Genesis) BFT() bool { return g.Consensus == ConsensusCometBFT }

// LoadGenesis reads and validates a genesis file.
func LoadGenesis(path string) (*Genesis, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var g Genesis
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&g); err != nil {
		return nil, fmt.Errorf("parse genesis: %w", err)
	}
	return &g, g.Validate()
}

// Validate checks genesis for obvious mistakes.
func (g *Genesis) Validate() error {
	if g.ChainID == "" {
		return errors.New("genesis: chain_id required")
	}
	switch g.Consensus {
	case "":
		if err := g.Proposer.Validate(); err != nil {
			return fmt.Errorf("genesis proposer: %w", err)
		}
	case ConsensusCometBFT:
	default:
		return fmt.Errorf("genesis: unknown consensus %q", g.Consensus)
	}
	seen := map[types.Address]bool{}
	for _, a := range g.Allocations {
		if err := a.Address.Validate(); err != nil {
			return fmt.Errorf("genesis allocation: %w", err)
		}
		if seen[a.Address] {
			return fmt.Errorf("genesis: duplicate allocation for %s", a.Address)
		}
		seen[a.Address] = true
	}
	_, err := g.State()
	return err
}

// State builds the initial ledger state.
func (g *Genesis) State() (*ledger.State, error) {
	s := ledger.New(g.MinFee)
	for _, a := range g.Assets {
		if a.Symbol == "" || !types.ValidAsset(a.Symbol) {
			return nil, fmt.Errorf("genesis asset: invalid symbol %q", a.Symbol)
		}
		if _, dup := s.AssetDefs[a.Symbol]; dup {
			return nil, fmt.Errorf("genesis asset %s defined twice", a.Symbol)
		}
		if err := a.Issuer.Validate(); err != nil {
			return nil, fmt.Errorf("genesis asset %s issuer: %w", a.Symbol, err)
		}
		s.AssetDefs[a.Symbol] = a
	}
	for _, a := range g.Allocations {
		if err := s.Credit(a.Address, a.Amount); err != nil {
			return nil, fmt.Errorf("genesis allocation %s: %w", a.Address, err)
		}
	}
	return s, nil
}

// Hash commits to the whole genesis document; it is block 1's prev_hash,
// so nodes started from different genesis files can never share a chain.
func (g *Genesis) Hash() types.Hash {
	b, _ := json.Marshal(g)
	return sha256.Sum256(b)
}
