package audit

import (
	"sync"
	"testing"
	"github.com/openreserve/node/ledger"
)

// TestDoubleSpendAttack heavily bombards the Ledger with concurrent transactions
// trying to spend the exact same ORP balance across hundreds of threads.
// It verifies that the Ledger's internal Mutex locks prevent race conditions.
func TestDoubleSpendAttack(t *testing.T) {
	state := ledger.NewState()

	// Initial setup: Alice has 100 ORP
	alice := "orp1_alice"
	bob := "orp1_bob"
	charlie := "orp1_charlie"

	state.GenesisMint(alice, 100.0)

	var wg sync.WaitGroup
	attackVectors := 1000

	// 1000 concurrent threads try to steal Alice's 100 ORP and send it to Bob and Charlie
	for i := 0; i < attackVectors; i++ {
		wg.Add(1)
		go func(iteration int) {
			defer wg.Done()
			
			// Odd threads send to Bob, Even threads send to Charlie
			target := bob
			if iteration%2 == 0 {
				target = charlie
			}

			// All threads try to send exactly 100 ORP at the exact same millisecond
			_ = state.Transfer(alice, target, 100.0)
		}(i)
	}

	// Wait for the attack to finish
	wg.Wait()

	// Verification
	finalAlice := state.GetBalance(alice)
	finalBob := state.GetBalance(bob)
	finalCharlie := state.GetBalance(charlie)

	totalNetworkBalance := finalAlice + finalBob + finalCharlie

	if totalNetworkBalance > 100.0 {
		t.Fatalf("CRITICAL SECURITY FAILURE: New ORP was created out of thin air. Total: %f", totalNetworkBalance)
	}

	if finalAlice < 0 {
		t.Fatalf("CRITICAL SECURITY FAILURE: Alice's balance dropped below zero: %f", finalAlice)
	}

	if finalBob+finalCharlie > 100.0 {
		t.Fatalf("CRITICAL SECURITY FAILURE: Double spend successful! Attackers stole %f ORP", finalBob+finalCharlie)
	}

	t.Logf("Double Spend Attack Defeated. Network balance preserved at %.2f ORP.", totalNetworkBalance)
}
