package core

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
)

// KeyPair represents a user's wallet keys and ORP address
type KeyPair struct {
	PrivateKey ed25519.PrivateKey
	PublicKey  ed25519.PublicKey
	Address    string
}

// GenerateKeyPair creates a fresh, random wallet
func GenerateKeyPair() (*KeyPair, error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("failed to generate key pair: %w", err)
	}

	return &KeyPair{
		PrivateKey: priv,
		PublicKey:  pub,
		Address:    GenerateAddress(pub),
	}, nil
}

// GenerateAddress derives an ORP address from a public key.
// Format: "orp1" + hex(sha256(pubKey)[:20])
func GenerateAddress(pubKey ed25519.PublicKey) string {
	h := sha256.New()
	h.Write(pubKey)
	hash := h.Sum(nil)
	
	// Take the first 20 bytes (160 bits) like standard crypto addresses
	addressHex := hex.EncodeToString(hash[:20])
	return "orp1" + addressHex
}

// SignPayload uses the wallet's private key to sign arbitrary data
func (kp *KeyPair) SignPayload(payload []byte) []byte {
	return ed25519.Sign(kp.PrivateKey, payload)
}
