package main

import (
	"fmt"
	"time"
)

// Monitor simulates a background worker pulling from the Explorer API
// and feeding the data into the Heuristics Engine.
type Monitor struct {
	Engine *HeuristicsEngine
}

func NewMonitor(engine *HeuristicsEngine) *Monitor {
	return &Monitor{
		Engine: engine,
	}
}

// Start polling
func (m *Monitor) Start() {
	fmt.Println("Starting Analytics Monitor...")
	go func() {
		for {
			// In a production environment, this would call `GET http://localhost:5000/api/blocks/latest`
			// For this Phase, we'll simulate ingesting sketchy transactions
			
			// Simulate a whale
			m.Engine.Analyze("tx_whale_01", "orp1_whale_wallet", "orp1_exchange", 150000.0)

			// Simulate a sybil attack
			for i := 0; i < 55; i++ {
				m.Engine.Analyze(fmt.Sprintf("tx_sybil_%d", i), "orp1_sybil_master", fmt.Sprintf("orp1_bot_%d", i), 1.0)
			}

			time.Sleep(10 * time.Second)
		}
	}()
}
