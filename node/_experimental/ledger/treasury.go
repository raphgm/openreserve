package ledger

import "sync"

// Treasury manages the global economics of the OpenReserve protocol
type Treasury struct {
	mu           sync.RWMutex
	TotalBurned  float64
	TotalRewards float64
}

// NewTreasury initializes the treasury
func NewTreasury() *Treasury {
	return &Treasury{
		TotalBurned:  0.0,
		TotalRewards: 0.0,
	}
}

// BurnFee permanently removes the fee from circulating supply
func (t *Treasury) BurnFee(fee float64) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.TotalBurned += fee
}

// DistributeRewards tracks newly minted inflation rewards
func (t *Treasury) DistributeRewards(amount float64) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.TotalRewards += amount
}
