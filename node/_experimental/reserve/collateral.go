package reserve

import "sync"

// AssetType categorizes the different types of collateral backing ORP
type AssetType string

const (
	AssetUSD             AssetType = "USD"
	AssetGold            AssetType = "GOLD"
	AssetBTC             AssetType = "BTC"
	AssetTreasury        AssetType = "US_TREASURY"
	AssetAfricanCurrency AssetType = "AFRICAN_CURRENCY"
)

// AssetBalance tracks the amount and estimated USD value of a specific collateral
type AssetBalance struct {
	Amount   float64
	USDValue float64 // The oracle-provided USD value of this asset pool
}

// Vault manages all locked collateral assets
type Vault struct {
	mu     sync.RWMutex
	assets map[AssetType]*AssetBalance
}

func NewVault() *Vault {
	return &Vault{
		assets: make(map[AssetType]*AssetBalance),
	}
}

// UpdateAsset updates the amount and USD value of a reserve asset based on Oracle feeds
func (v *Vault) UpdateAsset(asset AssetType, amount, usdValue float64) {
	v.mu.Lock()
	defer v.mu.Unlock()
	
	v.assets[asset] = &AssetBalance{
		Amount:   amount,
		USDValue: usdValue,
	}
}

// TotalReserveValue returns the combined USD value of all assets locked in the reserve
func (v *Vault) TotalReserveValue() float64 {
	v.mu.RLock()
	defer v.mu.RUnlock()
	
	total := 0.0
	for _, balance := range v.assets {
		total += balance.USDValue
	}
	return total
}
