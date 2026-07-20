package bridge

import (
	"crypto/ed25519"
	"fmt"

	"github.com/openreserve/node/ledger"
)

// EthereumBridge handles EVM specific cross-chain logic
type EthereumBridge struct {
	State       *State
	Relayers    *RelayerNetwork
	LedgerState *ledger.State
}

func NewEthereumBridge(state *State, relayers *RelayerNetwork, ls *ledger.State) *EthereumBridge {
	return &EthereumBridge{
		State:       state,
		Relayers:    relayers,
		LedgerState: ls,
	}
}

// ProcessLockAndMint is called when a Relayer submits proof that a user locked ERC-20 ORP on Ethereum.
// It verifies the proof and mints native ORP to the user's OpenReserve address.
func (eb *EthereumBridge) ProcessLockAndMint(ethTxHash string, receiver string, amount float64, relayerPubKey ed25519.PublicKey, signature []byte) error {
	
	// 1. Reconstruct the message the relayer should have signed
	msg := []byte(fmt.Sprintf("MINT:%s:%s:%.2f", ethTxHash, receiver, amount))

	// 2. Verify the Relayer signature
	if !eb.Relayers.VerifyProof(relayerPubKey, msg, signature) {
		return fmt.Errorf("bridge error: invalid relayer proof")
	}

	// 3. Record the transfer in the Bridge State (handles Replay Protection and Rate Limits)
	if err := eb.State.RecordTransfer(ethTxHash, ChainEthereum, "OPENRESERVE", receiver, amount); err != nil {
		return err // Could be a replay attack or daily limit exceeded
	}

	// 4. Mint native ORP on the Ledger
	// In a real implementation, we would call a specific Mint function on the Ledger
	// For Phase 14, we will use the GenesisMint mechanism to create the funds out of thin air,
	// because the equivalent ORP was just locked on Ethereum.
	acc := eb.LedgerState.GetAccount(receiver)
	acc.AddBalance(amount)

	fmt.Printf("BRIDGE: MINTED %.2f ORP to %s (Proof: Ethereum Tx %s)\\n", amount, receiver, ethTxHash)
	return nil
}
