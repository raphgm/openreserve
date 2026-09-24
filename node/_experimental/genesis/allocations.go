package genesis

// Allocation represents an initial distribution of ORP
type Allocation struct {
	Name    string
	Address string
	Amount  float64
}

// GetInitialAllocations defines exactly who holds the 1,000,000,000 ORP at Block 0.
func GetInitialAllocations() []Allocation {
	return []Allocation{
		{
			Name:    "Core Team & Founders (Time-locked)",
			Address: "orp1_team_vault_7d9e",
			Amount:  200_000_000.0, // 20%
		},
		{
			Name:    "Protocol Treasury (DAO Controlled)",
			Address: "orp1_treasury_dao_4b2c",
			Amount:  400_000_000.0, // 40%
		},
		{
			Name:    "Public Sale & Community Distribution",
			Address: "orp1_public_pool_9a1f",
			Amount:  400_000_000.0, // 40%
		},
	}
}
