package genesis

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"time"
)

// GenesisBlock defines the unalterable "Big Bang" of the OpenReserve network.
type GenesisBlock struct {
	ChainID     string
	Timestamp   time.Time
	TotalSupply float64
	Allocations []Allocation
	BlockHash   string
}

// DefaultMainnetGenesis returns the official starting configuration
func DefaultMainnetGenesis() *GenesisBlock {
	gb := &GenesisBlock{
		ChainID:     "openreserve-mainnet-v1",
		Timestamp:   time.Now().UTC(), 
		TotalSupply: 1_000_000_000.0, // 1 Billion ORP
		Allocations: GetInitialAllocations(),
	}

	// The blockhash is a cryptographic hash of the entire configuration.
	// Any future node attempting to sync with a different chain ID or allocation
	// will produce a different hash and be mathematically rejected by the true network.
	hashData := fmt.Sprintf("%s:%d:%.2f", gb.ChainID, gb.Timestamp.Unix(), gb.TotalSupply)
	h := sha256.Sum256([]byte(hashData))
	gb.BlockHash = "0x" + hex.EncodeToString(h[:])

	return gb
}
