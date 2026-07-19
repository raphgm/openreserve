package bridge

import (
	"crypto/ed25519"
	"encoding/hex"
	"fmt"
)

// RelayerNode represents an authorized external server that constantly monitors
// Ethereum/Solana for ORP Lock events and submits proofs to OpenReserve.
type RelayerNode struct {
	Name      string
	PublicKey ed25519.PublicKey
}

// RelayerNetwork manages the set of authorized oracle relayers
type RelayerNetwork struct {
	Authorized map[string]*RelayerNode
}

func NewRelayerNetwork() *RelayerNetwork {
	return &RelayerNetwork{
		Authorized: make(map[string]*RelayerNode),
	}
}

// AddRelayer authorizes a new node to submit cross-chain proofs
func (rn *RelayerNetwork) AddRelayer(name string, pubKey ed25519.PublicKey) {
	pubKeyHex := hex.EncodeToString(pubKey)
	rn.Authorized[pubKeyHex] = &RelayerNode{
		Name:      name,
		PublicKey: pubKey,
	}
}

// VerifyProof checks if the cryptographic signature on a cross-chain event
// actually came from one of our authorized Relayers.
func (rn *RelayerNetwork) VerifyProof(pubKey ed25519.PublicKey, message, signature []byte) bool {
	pubKeyHex := hex.EncodeToString(pubKey)
	if _, exists := rn.Authorized[pubKeyHex]; !exists {
		return false
	}
	return ed25519.Verify(pubKey, message, signature)
}
