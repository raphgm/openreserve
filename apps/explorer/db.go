package main

import (
	"fmt"
	"sync"
	"time"
)

// IndexedTransaction represents a flattened, easily searchable transaction
type IndexedTransaction struct {
	TxHash    string
	Sender    string
	Receiver  string
	Amount    float64
	Fee       float64
	Timestamp time.Time
	BlockHeight uint64
}

// Database provides ultra-fast in-memory O(1) lookups for the block explorer.
// In production, this would be a connector to PostgreSQL or Elasticsearch.
type Database struct {
	mu sync.RWMutex

	// Core Indexes
	TxByHash       map[string]*IndexedTransaction
	BlocksByHeight map[uint64]string // Maps Height to BlockHash
	
	// Complex Indexes
	AddressHistory map[string][]*IndexedTransaction // Maps Address to a list of its transactions

	// Global Metrics
	TotalTransactions uint64
	LiveTPS           float64
}

func NewDatabase() *Database {
	return &Database{
		TxByHash:       make(map[string]*IndexedTransaction),
		BlocksByHeight: make(map[uint64]string),
		AddressHistory: make(map[string][]*IndexedTransaction),
	}
}

// InsertTransaction adds a transaction to all relevant search indexes
func (db *Database) InsertTransaction(tx *IndexedTransaction) {
	db.mu.Lock()
	defer db.mu.Unlock()

	// 1. Index by Hash
	db.TxByHash[tx.TxHash] = tx

	// 2. Index by Sender History
	db.AddressHistory[tx.Sender] = append(db.AddressHistory[tx.Sender], tx)

	// 3. Index by Receiver History
	db.AddressHistory[tx.Receiver] = append(db.AddressHistory[tx.Receiver], tx)

	db.TotalTransactions++
}

// Search queries the database. Can accept a TxHash or an Address
func (db *Database) Search(query string) (interface{}, error) {
	db.mu.RLock()
	defer db.mu.RUnlock()

	// Try TxHash
	if tx, exists := db.TxByHash[query]; exists {
		return tx, nil
	}

	// Try Address
	if history, exists := db.AddressHistory[query]; exists {
		return history, nil
	}

	return nil, fmt.Errorf("no results found for query: %s", query)
}
