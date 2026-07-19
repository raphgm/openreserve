package core

import (
	"crypto/ed25519"
	"fmt"
)

// MultiSigIdentity represents an M-of-N corporate or high-security account.
// It requires signatures from multiple different Signers (which could be a mix
// of SoftwareSigners and HardwareSigners) to authorize a transaction.
type MultiSigIdentity struct {
	Name          string
	RequiredSigs  int
	Owners        []ed25519.PublicKey
}

func NewMultiSigIdentity(name string, required int, owners []ed25519.PublicKey) *MultiSigIdentity {
	return &MultiSigIdentity{
		Name:         name,
		RequiredSigs: required,
		Owners:       owners,
	}
}

// Authorize attempts to collect enough signatures from a provided list of signers
func (ms *MultiSigIdentity) Authorize(payload []byte, availableSigners []Signer) ([][]byte, error) {
	fmt.Printf("\\n[MULTISIG] Authorizing transaction for '%s' (Requires %d signatures)\\n", ms.Name, ms.RequiredSigs)
	
	signatures := make([][]byte, 0)
	
	for _, signer := range availableSigners {
		// Verify this signer is actually an owner
		isOwner := false
		pubKey := signer.GetPublicKey()
		
		for _, owner := range ms.Owners {
			if string(owner) == string(pubKey) {
				isOwner = true
				break
			}
		}

		if !isOwner {
			fmt.Println(" - Rejected signer: Not an authorized owner.")
			continue
		}

		// Request signature
		sig, err := signer.Sign(payload)
		if err != nil {
			return nil, err
		}
		
		signatures = append(signatures, sig)
		fmt.Printf(" - Signature %d collected successfully.\\n", len(signatures))

		// Stop if we have enough
		if len(signatures) >= ms.RequiredSigs {
			fmt.Println("[MULTISIG] Quorum reached. Transaction fully authorized!")
			return signatures, nil
		}
	}

	return nil, fmt.Errorf("authorization failed: collected %d/%d signatures", len(signatures), ms.RequiredSigs)
}
