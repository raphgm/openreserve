package vm

import "fmt"

// ContractState represents the internal key-value storage of a smart contract
type ContractState map[string][]byte

// Contract represents an instantiated smart contract on the ledger
type Contract struct {
	Address  string
	Creator  string
	Bytecode []byte // The compiled logic (or OP codes)
	State    ContractState
	Balance  float64 // Contracts can hold ORP
}

// NewContract initializes a new contract deployment
func NewContract(address, creator string, bytecode []byte) *Contract {
	return &Contract{
		Address:  address,
		Creator:  creator,
		Bytecode: bytecode,
		State:    make(ContractState),
		Balance:  0.0,
	}
}

// SetState securely updates the internal storage of the contract
func (c *Contract) SetState(key string, value []byte) {
	c.State[key] = value
}

// GetState reads from the internal storage
func (c *Contract) GetState(key string) ([]byte, error) {
	val, exists := c.State[key]
	if !exists {
		return nil, fmt.Errorf("state key '%s' not found", key)
	}
	return val, nil
}
