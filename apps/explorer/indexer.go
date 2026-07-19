package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"time"
)

// In a real network, the Indexer would connect via WebSocket to the Node
// and listen for "BlockFinalized" events.

type Indexer struct {
	DB *Database
}

func NewIndexer(db *Database) *Indexer {
	return &Indexer{DB: db}
}

// SimulateBlockIngestion mimics receiving a finalized block from the consensus engine
func (idx *Indexer) SimulateBlockIngestion(height uint64, numTxs int) {
	blockHash := fmt.Sprintf("blockhash_mock_%d", height)
	
	idx.DB.mu.Lock()
	idx.DB.BlocksByHeight[height] = blockHash
	idx.DB.mu.Unlock()

	for i := 0; i < numTxs; i++ {
		// Mock a transaction
		sender := fmt.Sprintf("orp1_sender_%d_%d", height, i)
		receiver := "orp1_treasury"
		
		// Generate a fake deterministic hash
		h := sha256.Sum256([]byte(fmt.Sprintf("%s%s%d", sender, receiver, time.Now().UnixNano())))
		txHash := "0x" + hex.EncodeToString(h[:])

		tx := &IndexedTransaction{
			TxHash:      txHash,
			Sender:      sender,
			Receiver:    receiver,
			Amount:      100.0,
			Fee:         0.1,
			Timestamp:   time.Now(),
			BlockHeight: height,
		}

		idx.DB.InsertTransaction(tx)
	}

	fmt.Printf("[INDEXER] Processed Block %d | Extracted %d Transactions\\n", height, numTxs)
}
