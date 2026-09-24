package vm

import (
	"crypto/ed25519"
	"encoding/json"
	"fmt"
)

// EscrowPayload is the JSON data passed to the Escrow contract
type EscrowPayload struct {
	Action      string `json:"action"`       // "deposit" or "release"
	ArbiterSig  []byte `json:"arbiter_sig"`  // Signature authorizing the release
	Destination string `json:"destination"`  // Where funds go on release
}

// NativeEscrow is a hardcoded smart contract bypassing the bytecode VM for performance
type NativeEscrow struct {
	Contract *Contract
	Arbiter  ed25519.PublicKey // The trusted third party who can authorize the release
	Seller   string
	Buyer    string
	Amount   float64
}

// Execute handles the logic for a Native Escrow
func (ne *NativeEscrow) Execute(payloadJSON []byte) error {
	var payload EscrowPayload
	if err := json.Unmarshal(payloadJSON, &payload); err != nil {
		return fmt.Errorf("invalid escrow payload")
	}

	if payload.Action == "deposit" {
		// Logic to hold funds
		ne.Contract.SetState("status", []byte("LOCKED"))
		fmt.Printf("Escrow: %.2f ORP Locked between Buyer %s and Seller %s\\n", ne.Amount, ne.Buyer, ne.Seller)
		return nil
	}

	if payload.Action == "release" {
		// 1. Check if it's already released
		status, _ := ne.Contract.GetState("status")
		if string(status) == "RELEASED" {
			return fmt.Errorf("escrow already released")
		}

		// 2. Verify Arbiter Signature
		msg := []byte(fmt.Sprintf("RELEASE:%s:%.2f", payload.Destination, ne.Amount))
		if !ed25519.Verify(ne.Arbiter, msg, payload.ArbiterSig) {
			return fmt.Errorf("invalid arbiter signature")
		}

		// 3. Release Funds
		ne.Contract.SetState("status", []byte("RELEASED"))
		ne.Contract.Balance -= ne.Amount
		// Note: The global Ledger State Engine would need to catch this and credit payload.Destination
		
		fmt.Printf("Escrow: %.2f ORP successfully released to %s\\n", ne.Amount, payload.Destination)
		return nil
	}

	return fmt.Errorf("unknown escrow action")
}
