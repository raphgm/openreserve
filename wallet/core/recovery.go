package core

import (
	"crypto/ed25519"
	"fmt"

	"github.com/tyler-smith/go-bip39"
)

// GenerateMnemonic generates a new 12-word BIP39 recovery phrase
func GenerateMnemonic() (string, error) {
	// 128 bits of entropy = 12 words
	entropy, err := bip39.NewEntropy(128)
	if err != nil {
		return "", err
	}

	mnemonic, err := bip39.NewMnemonic(entropy)
	if err != nil {
		return "", err
	}

	return mnemonic, nil
}

// RecoverKeyPair restores a wallet KeyPair from a 12-word mnemonic phrase
func RecoverKeyPair(mnemonic string) (*KeyPair, error) {
	if !bip39.IsMnemonicValid(mnemonic) {
		return nil, fmt.Errorf("invalid mnemonic phrase")
	}

	// Generate a seed from the mnemonic
	seed := bip39.NewSeed(mnemonic, "") // Empty password for simplicity

	// Ed25519 requires exactly 32 bytes of seed data to generate a deterministic private key
	if len(seed) < 32 {
		return nil, fmt.Errorf("seed length too short")
	}
	
	priv := ed25519.NewKeyFromSeed(seed[:32])
	pub := priv.Public().(ed25519.PublicKey)

	return &KeyPair{
		PrivateKey: priv,
		PublicKey:  pub,
		Address:    GenerateAddress(pub),
	}, nil
}
