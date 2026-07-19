package core

// Signer defines the interface for generating cryptographic signatures.
// This allows the wallet SDK to easily swap between hot memory keys
// and cold hardware storage devices.
type Signer interface {
	Sign(message []byte) ([]byte, error)
	GetPublicKey() []byte
}

// SoftwareSigner is the standard memory-based signer used in Phase 10
type SoftwareSigner struct {
	KeyPair *KeyPair
}

func NewSoftwareSigner(kp *KeyPair) *SoftwareSigner {
	return &SoftwareSigner{KeyPair: kp}
}

func (ss *SoftwareSigner) Sign(message []byte) ([]byte, error) {
	// Directly accesses the private key in memory
	return ss.KeyPair.SignPayload(message), nil
}

func (ss *SoftwareSigner) GetPublicKey() []byte {
	return ss.KeyPair.PublicKey
}
