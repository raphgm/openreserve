package core

import (
	"crypto/sha256"
	"fmt"
	"time"
)

// Header contains metadata for a block
type Header struct {
	Version       uint32
	PrevBlockHash []byte
	MerkleRoot    []byte
	Timestamp     int64
	Validator     string // The public key or address of the block proposer
	Signature     []byte // The BFT consensus signature aggregate
}

// Block represents a collection of finalized transactions
type Block struct {
	Header       *Header
	Transactions []*Transaction
	Hash         []byte
}

// CalculateHash generates the unique SHA256 hash for the block header
func (b *Block) CalculateHash() []byte {
	record := fmt.Sprintf("%d:%x:%x:%d:%s", 
		b.Header.Version, 
		b.Header.PrevBlockHash, 
		b.Header.MerkleRoot, 
		b.Header.Timestamp, 
		b.Header.Validator,
	)
	h := sha256.New()
	h.Write([]byte(record))
	return h.Sum(nil)
}

// CalculateMerkleRoot generates a proper Merkle Root of all transactions in the block.
func (b *Block) CalculateMerkleRoot() []byte {
	var txHashes [][]byte
	for _, tx := range b.Transactions {
		txHashes = append(txHashes, tx.Hash())
	}
	
	return CalculateMerkleRoot(txHashes)
}

// NewBlock creates a new block from a set of transactions and the previous block hash
func NewBlock(transactions []*Transaction, prevBlockHash []byte, validator string) *Block {
	block := &Block{
		Header: &Header{
			Version:       1,
			PrevBlockHash: prevBlockHash,
			Timestamp:     time.Now().Unix(),
			Validator:     validator,
		},
		Transactions: transactions,
	}
	
	block.Header.MerkleRoot = block.CalculateMerkleRoot()
	block.Hash = block.CalculateHash()
	
	return block
}

// NewGenesisBlock creates the first block in the OpenReserve blockchain
func NewGenesisBlock() *Block {
	// A special coinbase transaction for genesis
	genesisTx := NewTransaction("0x0000000000000000000000000000000000000000", "0xTreasuryOrFoundationAddress", 1000000000.0, 0, 0)
	return NewBlock([]*Transaction{genesisTx}, []byte{}, "GenesisValidator")
}
