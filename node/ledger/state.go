package ledger

import (
	"crypto/ed25519"
	"encoding/hex"
	"fmt"
	"sync"

	"github.com/openreserve/node/core"
	"github.com/openreserve/node/genesis"
)

// State is the global ledger database
type State struct {
	mu       sync.RWMutex
	accounts map[string]*Account
	treasury *Treasury
}

// NewState initializes an empty, in-memory ledger
func NewState() *State {
	return &State{
		accounts: make(map[string]*Account),
		treasury: NewTreasury(),
	}
}

// GetAccount retrieves an account by address, creating a blank one if it doesn't exist
func (s *State) GetAccount(address string) *Account {
	s.mu.RLock()
	acc, exists := s.accounts[address]
	s.mu.RUnlock()

	if !exists {
		s.mu.Lock()
		acc = NewAccount(address)
		s.accounts[address] = acc
		s.mu.Unlock()
	}
	return acc
}

// ApplyTransaction executes a verified transaction against the state
func (s *State) ApplyTransaction(tx *core.Transaction, pubKey ed25519.PublicKey) error {
	// 1. Verify Signature
	if !tx.Verify(pubKey) {
		return fmt.Errorf("transaction signature is invalid")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	sender := s.GetAccount(tx.Sender)
	receiver := s.GetAccount(tx.Receiver)

	// 2. Verify Nonce
	if tx.Nonce != sender.Nonce {
		return fmt.Errorf("invalid nonce: expected %d, got %d", sender.Nonce, tx.Nonce)
	}

	// 3. Verify Balance (Amount + Fee)
	totalCost := tx.Amount + tx.Fee
	if sender.Balance < totalCost {
		return fmt.Errorf("insufficient funds: required %.2f ORP, got %.2f ORP", totalCost, sender.Balance)
	}

	// 4. Execute State Transition
	sender.SubBalance(totalCost)
	receiver.AddBalance(tx.Amount)
	sender.IncrementNonce()

	// 5. Burn Fee (Deflationary mechanism)
	if tx.Fee > 0 {
		s.treasury.BurnFee(tx.Fee)
	}

	return nil
}

// InitFromGenesis loads the official Mainnet block 0 parameters
func (s *State) InitFromGenesis(gb *genesis.GenesisBlock) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Ensure this is only called once at Block 0
	if len(s.accounts) > 0 {
		return fmt.Errorf("ledger already initialized, cannot load genesis block")
	}

	var minted float64 = 0
	for _, alloc := range gb.Allocations {
		// Bypass GetAccount locking since we already hold the master lock
		acc := NewAccount(alloc.Address)
		acc.AddBalance(alloc.Amount)
		s.accounts[alloc.Address] = acc
		minted += alloc.Amount
	}

	if minted != gb.TotalSupply {
		return fmt.Errorf("genesis validation failed: allocated %.2f != supply %.2f", minted, gb.TotalSupply)
	}

	fmt.Printf("[MAINNET] Genesis Block 0 Loaded. Chain ID: %s | Total Supply: %.2f ORP\\n", gb.ChainID, gb.TotalSupply)
	return nil
}
