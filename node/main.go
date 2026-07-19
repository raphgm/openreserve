package main

import (
	"fmt"
	"log"

	"github.com/openreserve/node/api"
	"github.com/openreserve/node/consensus"
	"github.com/openreserve/node/ledger"
	"github.com/openreserve/node/p2p"
	"github.com/openreserve/node/reserve"
)

func main() {
	fmt.Println("Starting OpenReserve Node...")

	// 1. Initialize Global Ledger State
	state := ledger.NewState()

	// For demonstration, mint some genesis tokens to an address
	state.GenesisMint("0xDeployerAddress", 1000000.0)

	// 2. Initialize Reserve Economics Engine
	res := reserve.NewEngine(1000000.0) // Matches genesis mint

	// 3. Initialize Consensus Engine & Validator Set
	valSet := consensus.NewValidatorSet()
	cons := consensus.NewEngine(valSet)

	// 4. Initialize P2P Networking
	p2pServer := p2p.NewServer(":3000")
	go func() {
		if err := p2pServer.Start(); err != nil {
			log.Fatalf("P2P server failed: %v", err)
		}
	}()

	// 5. Initialize and Start REST API
	apiServer := api.NewServer(":8080", state, cons, p2pServer, res)
	fmt.Println("REST API listening on :8080")
	if err := apiServer.Start(); err != nil {
		log.Fatalf("API server failed: %v", err)
	}
}
