package ledger

// Account represents a user or smart contract on the OpenReserve network
type Account struct {
	Address string
	Balance float64 // Stored in ORP
	Nonce   uint64  // Tracks transaction count to prevent replay attacks
}

// NewAccount initializes a blank account
func NewAccount(address string) *Account {
	return &Account{
		Address: address,
		Balance: 0.0,
		Nonce:   0,
	}
}

// AddBalance safely increases the account balance
func (a *Account) AddBalance(amount float64) {
	a.Balance += amount
}

// SubBalance safely decreases the account balance
// Returns false if insufficient funds
func (a *Account) SubBalance(amount float64) bool {
	if a.Balance < amount {
		return false
	}
	a.Balance -= amount
	return true
}

// IncrementNonce bumps the account nonce
func (a *Account) IncrementNonce() {
	a.Nonce++
}
