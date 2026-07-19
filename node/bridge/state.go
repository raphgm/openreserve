package bridge

import (
	"crypto/ed25519"
	"encoding/hex"
	"fmt"
	"sync"
	"time"
)

// SupportedChain identifies an external blockchain
type SupportedChain string

const (
	ChainEthereum SupportedChain = "ETHEREUM"
	ChainSolana   SupportedChain = "SOLANA"
)

// TransferStatus tracks the state of a bridge crossing
type TransferStatus string

const (
	StatusPending  TransferStatus = "PENDING"
	StatusComplete TransferStatus = "COMPLETE"
	StatusRejected TransferStatus = "REJECTED"
)

// CrossChainTransfer represents a movement of value between OpenReserve and an external chain
type CrossChainTransfer struct {
	TxHash      string         // The external chain's transaction hash (e.g., 0x...)
	SourceChain SupportedChain
	TargetChain SupportedChain
	Receiver    string
	Amount      float64
	Status      TransferStatus
	Timestamp   time.Time
}

// State manages the global bridge state and prevents replay attacks
type State struct {
	mu        sync.RWMutex
	Transfers map[string]*CrossChainTransfer // Map of TxHash to Transfer
	
	// Security: Rate Limiting
	DailyVolume   float64
	DailyLimit    float64
	LastResetTime time.Time
}

func NewState() *State {
	return &State{
		Transfers:     make(map[string]*CrossChainTransfer),
		DailyVolume:   0.0,
		DailyLimit:    1000000.0, // Hardcoded 1M ORP daily limit to prevent catastrophic bridge hacks
		LastResetTime: time.Now(),
	}
}

// RecordTransfer logs a new transfer. Fails if the TxHash was already processed (Replay Attack) or if it exceeds limits.
func (s *State) RecordTransfer(txHash string, src, target SupportedChain, receiver string, amount float64) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// 1. Replay Protection
	if _, exists := s.Transfers[txHash]; exists {
		return fmt.Errorf("replay attack prevented: txHash %s already processed", txHash)
	}

	// 2. Rate Limit Check
	if time.Since(s.LastResetTime) > 24*time.Hour {
		s.DailyVolume = 0
		s.LastResetTime = time.Now()
	}

	if s.DailyVolume+amount > s.DailyLimit {
		return fmt.Errorf("security halt: bridge daily transfer limit (%.2f ORP) exceeded", s.DailyLimit)
	}

	s.Transfers[txHash] = &CrossChainTransfer{
		TxHash:      txHash,
		SourceChain: src,
		TargetChain: target,
		Receiver:    receiver,
		Amount:      amount,
		Status:      StatusComplete,
		Timestamp:   time.Now(),
	}
	
	s.DailyVolume += amount
	return nil
}
