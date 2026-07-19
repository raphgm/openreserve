package reserve

import (
	"crypto/ed25519"
	"encoding/hex"
	"fmt"
)

// Custodian represents an entity authorized to mint and burn ORP
// based on real-world asset deposits and withdrawals.
type Custodian struct {
	Name      string
	PublicKey ed25519.PublicKey
}

// MintingController handles the secure issuance and destruction of ORP
type MintingController struct {
	Engine     *Engine
	Custodians map[string]*Custodian // Map of hex-encoded public keys to Custodians
}

func NewMintingController(engine *Engine) *MintingController {
	return &MintingController{
		Engine:     engine,
		Custodians: make(map[string]*Custodian),
	}
}

// AddCustodian authorizes a new entity to mint/burn ORP
func (mc *MintingController) AddCustodian(name string, pubKey ed25519.PublicKey) {
	pubKeyHex := hex.EncodeToString(pubKey)
	mc.Custodians[pubKeyHex] = &Custodian{
		Name:      name,
		PublicKey: pubKey,
	}
}

// isAuthorized checks if the provided signature comes from a valid custodian
func (mc *MintingController) isAuthorized(pubKey ed25519.PublicKey, message, signature []byte) bool {
	pubKeyHex := hex.EncodeToString(pubKey)
	if _, exists := mc.Custodians[pubKeyHex]; !exists {
		return false
	}
	return ed25519.Verify(pubKey, message, signature)
}

// Mint ORP when new collateral is deposited. Requires an authorized custodian signature.
func (mc *MintingController) Mint(amount float64, pubKey ed25519.PublicKey, signature []byte) error {
	msg := []byte(fmt.Sprintf("MINT:%.2f", amount))
	
	if !mc.isAuthorized(pubKey, msg, signature) {
		return fmt.Errorf("unauthorized minting attempt")
	}

	mc.Engine.TotalSupply += amount
	return nil
}

// Burn ORP when a user redeems it for real-world collateral. Requires custodian authorization.
func (mc *MintingController) Burn(amount float64, pubKey ed25519.PublicKey, signature []byte) error {
	msg := []byte(fmt.Sprintf("BURN:%.2f", amount))
	
	if !mc.isAuthorized(pubKey, msg, signature) {
		return fmt.Errorf("unauthorized burning attempt")
	}

	if mc.Engine.TotalSupply < amount {
		return fmt.Errorf("cannot burn more than total supply")
	}

	mc.Engine.TotalSupply -= amount
	return nil
}
