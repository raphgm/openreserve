package api

import (
	"encoding/hex"
	"encoding/json"
	"net/http"

	"github.com/openreserve/node/core"
)

// handleGetBalance returns the current balance and nonce for an address
func (s *Server) handleGetBalance(w http.ResponseWriter, r *http.Request) {
	address := r.URL.Query().Get("address")
	if address == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "address is required"})
		return
	}

	acc := s.LedgerState.GetAccount(address)
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"address": acc.Address,
		"balance": acc.Balance,
		"nonce":   acc.Nonce,
	})
}

// TransactionPayload defines the expected JSON payload for submitting a tx
type TransactionPayload struct {
	Sender    string  `json:"sender"`
	Receiver  string  `json:"receiver"`
	Amount    float64 `json:"amount"`
	Fee       float64 `json:"fee"`
	Nonce     uint64  `json:"nonce"`
	PublicKey string  `json:"publicKey"` // Hex encoded
	Signature string  `json:"signature"` // Hex encoded
}

// handlePostTransaction accepts a signed transaction, applies it, and broadcasts it
func (s *Server) handlePostTransaction(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "only POST is allowed"})
		return
	}

	var payload TransactionPayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json payload"})
		return
	}

	pubKeyBytes, err := hex.DecodeString(payload.PublicKey)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid public key hex"})
		return
	}

	sigBytes, err := hex.DecodeString(payload.Signature)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid signature hex"})
		return
	}

	tx := core.NewTransaction(payload.Sender, payload.Receiver, payload.Amount, payload.Fee, payload.Nonce)
	tx.Signature = sigBytes

	// Apply it to the local ledger
	if err := s.LedgerState.ApplyTransaction(tx, pubKeyBytes); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	// In a real network, this would go to the mempool first.
	// We'll simulate broadcasting it immediately.
	// s.P2P.Broadcast(...) -> requires converting tx to p2p.Message

	writeJSON(w, http.StatusOK, map[string]string{"status": "transaction accepted"})
}

// handleGetValidators returns the active consensus validator set
func (s *Server) handleGetValidators(w http.ResponseWriter, r *http.Request) {
	var validators []map[string]interface{}

	for addr, val := range s.Consensus.Validators.Validators {
		validators = append(validators, map[string]interface{}{
			"address": addr,
			"stake":   val.Stake,
		})
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"total_network_stake": s.Consensus.Validators.TotalStake,
		"validators":          validators,
	})
}

// handleGetReserveAudit returns the real-time solver report for the protocol
func (s *Server) handleGetReserveAudit(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{
		"audit_report": s.ReserveEngine.GenerateAuditReport(),
	})
}
