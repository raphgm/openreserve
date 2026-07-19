package core

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"fmt"
)

// GenerateKeyPair creates a new Ed25519 public/private key pair
func GenerateKeyPair() (ed25519.PublicKey, ed25519.PrivateKey, error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to generate key pair: %w", err)
	}
	return pub, priv, nil
}

// SignData signs an arbitrary byte slice using an Ed25519 private key
func SignData(priv ed25519.PrivateKey, data []byte) []byte {
	return ed25519.Sign(priv, data)
}

// VerifySignature verifies an Ed25519 signature against a public key and data
func VerifySignature(pub ed25519.PublicKey, data, sig []byte) bool {
	return ed25519.Verify(pub, data, sig)
}

// CalculateMerkleRoot computes a simple Merkle Root from a list of hashes.
// Note: This is a basic implementation. A production blockchain would use a more
// optimized or sparse Merkle tree depending on state requirements.
func CalculateMerkleRoot(hashes [][]byte) []byte {
	if len(hashes) == 0 {
		return []byte{}
	}

	if len(hashes) == 1 {
		return hashes[0]
	}

	// If the number of hashes is odd, duplicate the last one
	if len(hashes)%2 != 0 {
		hashes = append(hashes, hashes[len(hashes)-1])
	}

	var nextLevel [][]byte
	for i := 0; i < len(hashes); i += 2 {
		h := sha256.New()
		h.Write(append(hashes[i], hashes[i+1]...))
		nextLevel = append(nextLevel, h.Sum(nil))
	}

	return CalculateMerkleRoot(nextLevel)
}
