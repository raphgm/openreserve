package main

import (
	"time"
)

func main() {
	db := NewDatabase()
	indexer := NewIndexer(db)

	// Simulate blockchain activity in the background
	go func() {
		var height uint64 = 1
		for {
			// Simulate a new block arriving every 2 seconds with 5 transactions
			indexer.SimulateBlockIngestion(height, 5)
			height++
			time.Sleep(2 * time.Second)
		}
	}()

	api := NewAPIServer(db, ":5000")
	api.Start()
}
