package core

import (
	"crypto/ed25519"
	"fmt"
	"os"
)

// HardwareSigner represents an external security device (e.g. Ledger, Trezor).
// The private key is never loaded into the computer's memory.
type HardwareSigner struct {
	DeviceID  string
	PublicKey ed25519.PublicKey
	
	// internal hidden key just to mock the physical hardware for the simulation
	mockSecureEnclave ed25519.PrivateKey 
}

// NewMockHardwareSigner initializes a simulated hardware wallet
func NewMockHardwareSigner(deviceID string) *HardwareSigner {
	pub, priv, _ := ed25519.GenerateKey(nil)
	return &HardwareSigner{
		DeviceID:          deviceID,
		PublicKey:         pub,
		mockSecureEnclave: priv,
	}
}

func (hs *HardwareSigner) GetPublicKey() []byte {
	return hs.PublicKey
}

// Sign simulates sending a payload to a USB hardware device
// and waiting for the user to physically press a button to confirm.
func (hs *HardwareSigner) Sign(message []byte) ([]byte, error) {
	fmt.Printf("\\n[HARDWARE WALLET %s]\\n", hs.DeviceID)
	fmt.Printf("Requesting signature for payload: %s\\n", string(message))
	fmt.Print("Please press the PHYSICAL BUTTON on your device to confirm (Press Enter)... ")
	
	// Wait for user to hit Enter to simulate physical button press
	os.Stdin.Read(make([]byte, 1))

	fmt.Println("Physical confirmation received. Signing inside Secure Enclave...")

	// The signature happens completely isolated from the main program state
	sig := ed25519.Sign(hs.mockSecureEnclave, message)
	
	return sig, nil
}
