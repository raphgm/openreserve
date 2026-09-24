package consensus

import (
	"crypto/ed25519"
	"encoding/hex"
	"fmt"
	"math/rand"
	"sort"
)

// Validator represents a node allowed to participate in consensus
type Validator struct {
	Address   string
	PublicKey ed25519.PublicKey
	Stake     float64
}

// ValidatorSet manages the active list of validators
type ValidatorSet struct {
	Validators map[string]*Validator
	TotalStake float64
}

func NewValidatorSet() *ValidatorSet {
	return &ValidatorSet{
		Validators: make(map[string]*Validator),
		TotalStake: 0,
	}
}

// AddValidator registers a new validator and updates the total network stake
func (vs *ValidatorSet) AddValidator(address string, pubKey ed25519.PublicKey, stake float64) {
	vs.Validators[address] = &Validator{
		Address:   address,
		PublicKey: pubKey,
		Stake:     stake,
	}
	vs.TotalStake += stake
}

// SelectLeader deterministically selects a proposer based on stake weight.
// In a real protocol, this would use a verifiable random function (VRF) tied to the previous block hash.
func (vs *ValidatorSet) SelectLeader(seed int64) *Validator {
	if len(vs.Validators) == 0 {
		return nil
	}

	// Sort validators by address for deterministic ordering
	var addresses []string
	for addr := range vs.Validators {
		addresses = append(addresses, addr)
	}
	sort.Strings(addresses)

	// A basic deterministic weighted selection
	r := rand.New(rand.NewSource(seed))
	target := r.Float64() * vs.TotalStake

	var cumulative float64
	for _, addr := range addresses {
		v := vs.Validators[addr]
		cumulative += v.Stake
		if cumulative >= target {
			return v
		}
	}

	// Fallback to the last validator
	return vs.Validators[addresses[len(addresses)-1]]
}
