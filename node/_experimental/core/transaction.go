package core

import (
	"crypto/sha256"
	"fmt"
	"time"
)

// Transaction represents a transfer of value (ORP) between two addresses.
type Transaction struct {
	Sender    string
	Receiver  string
	Amount    float64
	Fee       float64
	Nonce     uint64
	Timestamp int64
	Signature []byte
}

// Hash calculates the SHA256 hash of the transaction data.
func (tx *Transaction) Hash() []byte {
	record := fmt.Sprintf("%s:%s:%f:%f:%d:%d", tx.Sender, tx.Receiver, tx.Amount, tx.Fee, tx.Nonce, tx.Timestamp)
	h := sha256.New()
	h.Write([]byte(record))
	return h.Sum(nil)
}

// Sign applies a cryptographic signature to the transaction using an Ed25519 private key
func (tx *Transaction) Sign(privateKey []byte) {
	tx.Signature = SignData(privateKey, tx.Hash())
}

// Verify checks the validity of the transaction signature using an Ed25519 public key
func (tx *Transaction) Verify(publicKey []byte) bool {
	return VerifySignature(publicKey, tx.Hash(), tx.Signature)
}

// NewTransaction creates a new unconfirmed transaction
func NewTransaction(sender, receiver string, amount, fee float64, nonce uint64) *Transaction {
	return &Transaction{
		Sender:    sender,
		Receiver:  receiver,
		Amount:    amount,
		Fee:       fee,
		Nonce:     nonce,
		Timestamp: time.Now().Unix(),
	}
}
