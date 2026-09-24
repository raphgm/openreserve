package core

import (
	"bytes"
	"testing"
)

func TestEd25519SignVerify(t *testing.T) {
	pub, priv, err := GenerateKeyPair()
	if err != nil {
		t.Fatalf("Failed to generate keys: %v", err)
	}

	data := []byte("hello openreserve")
	sig := SignData(priv, data)

	if !VerifySignature(pub, data, sig) {
		t.Error("Signature verification failed for valid signature")
	}

	// Test invalid signature
	if VerifySignature(pub, []byte("wrong data"), sig) {
		t.Error("Signature verification succeeded for invalid data")
	}
}

func TestMerkleRoot(t *testing.T) {
	// Simple test to ensure Merkle Root generation is deterministic
	hash1 := []byte("hash1")
	hash2 := []byte("hash2")
	hash3 := []byte("hash3")

	root1 := CalculateMerkleRoot([][]byte{hash1, hash2, hash3})
	root2 := CalculateMerkleRoot([][]byte{hash1, hash2, hash3})

	if !bytes.Equal(root1, root2) {
		t.Error("Merkle roots should be deterministic")
	}
}
