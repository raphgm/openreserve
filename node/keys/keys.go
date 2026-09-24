// Package keys stores Ed25519 keys on disk.
package keys

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"

	"github.com/openreserve/node/types"
)

type keyFile struct {
	Address types.Address `json:"address"`
	Seed    string        `json:"seed"` // 32-byte Ed25519 seed, hex
}

// Generate creates a new key and writes it to path with 0600 permissions.
// It refuses to overwrite an existing file so keys are never lost by accident.
func Generate(path string) (ed25519.PrivateKey, error) {
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	kf := keyFile{
		Address: types.AddressFromPubKey(priv.Public().(ed25519.PublicKey)),
		Seed:    hex.EncodeToString(priv.Seed()),
	}
	data, _ := json.MarshalIndent(kf, "", "  ")
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return nil, err
	}
	if _, err := f.Write(append(data, '\n')); err != nil {
		f.Close()
		return nil, err
	}
	return priv, f.Close()
}

// Load reads a key written by Generate.
func Load(path string) (ed25519.PrivateKey, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var kf keyFile
	if err := json.Unmarshal(data, &kf); err != nil {
		return nil, fmt.Errorf("key file %s: %w", path, err)
	}
	seed, err := hex.DecodeString(kf.Seed)
	if err != nil || len(seed) != ed25519.SeedSize {
		return nil, fmt.Errorf("key file %s: bad seed", path)
	}
	priv := ed25519.NewKeyFromSeed(seed)
	if types.AddressFromPubKey(priv.Public().(ed25519.PublicKey)) != kf.Address {
		return nil, errors.New("key file address does not match seed")
	}
	return priv, nil
}

// Address returns the address of a private key.
func Address(priv ed25519.PrivateKey) types.Address {
	return types.AddressFromPubKey(priv.Public().(ed25519.PublicKey))
}
