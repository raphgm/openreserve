package reserve

import "fmt"

// Engine combines the Vault and calculates protocol solvency
type Engine struct {
	Vault       *Vault
	TotalSupply float64 // The total circulating supply of ORP
}

func NewEngine(totalSupply float64) *Engine {
	return &Engine{
		Vault:       NewVault(),
		TotalSupply: totalSupply,
	}
}

// CalculateRatio computes the Collateralization Ratio (TVL / Total Supply)
// If the ratio is >= 1.0, the network is fully solvent.
func (e *Engine) CalculateRatio() float64 {
	if e.TotalSupply == 0 {
		return 1.0 // Prevent division by zero, theoretically fully backed if 0 supply
	}
	
	tvl := e.Vault.TotalReserveValue()
	return tvl / e.TotalSupply
}

// IsSolvent returns true if the network has enough USD equivalent collateral
// to back every circulating ORP 1-to-1.
func (e *Engine) IsSolvent() bool {
	return e.CalculateRatio() >= 1.0
}

// GenerateAuditReport creates a summary of the current reserve status
func (e *Engine) GenerateAuditReport() string {
	ratio := e.CalculateRatio() * 100
	status := "SOLVENT"
	if ratio < 100 {
		status = "INSOLVENT"
	}

	return fmt.Sprintf("Audit Report:\\nStatus: %s\\nTotal Reserve (USD): $%.2f\\nTotal ORP Supply: %.2f\\nCollateral Ratio: %.2f%%", 
		status, e.Vault.TotalReserveValue(), e.TotalSupply, ratio)
}
