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
	"strings"

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
	return priv, Write(path, priv)
}

// Write saves a key to a new file with 0600 permissions.
func Write(path string, priv ed25519.PrivateKey) error {
	kf := keyFile{Address: Address(priv), Seed: SeedHex(priv)}
	data, _ := json.MarshalIndent(kf, "", "  ")
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	if _, err := f.Write(append(data, '\n')); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// Load reads a key written by Generate. It refuses files that other users
// can read: a leaked proposer or faucet key cannot be revoked.
func Load(path string) (ed25519.PrivateKey, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if info.Mode().Perm()&0o077 != 0 {
		return nil, fmt.Errorf("key file %s is readable by other users (mode %04o); run: chmod 600 %s", path, info.Mode().Perm(), path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var kf keyFile
	if err := json.Unmarshal(data, &kf); err != nil {
		return nil, fmt.Errorf("key file %s: %w", path, err)
	}
	priv, err := FromSeedHex(kf.Seed)
	if err != nil {
		return nil, fmt.Errorf("key file %s: %w", path, err)
	}
	if Address(priv) != kf.Address {
		return nil, errors.New("key file address does not match seed")
	}
	return priv, nil
}

// FromSeedHex builds a key from a hex-encoded 32-byte seed.
func FromSeedHex(s string) (ed25519.PrivateKey, error) {
	seed, err := hex.DecodeString(strings.TrimSpace(s))
	if err != nil || len(seed) != ed25519.SeedSize {
		return nil, errors.New("seed must be 64 hex characters")
	}
	return ed25519.NewKeyFromSeed(seed), nil
}

// Resolve loads a key from the environment variable envVar if it is set
// (how container platforms and secret managers inject secrets), otherwise
// from path. It returns nil if neither is given.
func Resolve(path, envVar string) (ed25519.PrivateKey, error) {
	if v := os.Getenv(envVar); v != "" {
		k, err := FromSeedHex(v)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", envVar, err)
		}
		return k, nil
	}
	if path == "" {
		return nil, nil
	}
	return Load(path)
}

// SeedHex returns the hex seed of a key, for export.
func SeedHex(priv ed25519.PrivateKey) string { return hex.EncodeToString(priv.Seed()) }

// Address returns the address of a private key.
func Address(priv ed25519.PrivateKey) types.Address {
	return types.AddressFromPubKey(priv.Public().(ed25519.PublicKey))
}
