package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync"
)

// In a production app, this would be a secure database mapping usernames to blockchain addresses
type Database struct {
	mu    sync.RWMutex
	Users map[string]string // Username -> orp1... address
}

var db = &Database{Users: make(map[string]string)}
var totalBurned float64 = 0.0
var burnMutex sync.RWMutex

// ConvenienceFee is the percentage ORPay charges to buy and burn ORP
const ConvenienceFee = 0.001 // 0.1%

func main() {
	mux := http.NewServeMux()

	// Handle CORS for local frontend testing
	corsMiddleware := func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Access-Control-Allow-Origin", "*")
			w.Header().Set("Access-Control-Allow-Methods", "POST, GET, OPTIONS, PUT, DELETE")
			w.Header().Set("Access-Control-Allow-Headers", "Accept, Content-Type, Content-Length, Accept-Encoding, X-CSRF-Token, Authorization")
			if r.Method == "OPTIONS" {
				return
			}
			next(w, r)
		}
	}

	mux.HandleFunc("/api/register", corsMiddleware(handleRegister))
	mux.HandleFunc("/api/resolve", corsMiddleware(handleResolve))
	mux.HandleFunc("/api/stats", corsMiddleware(handleStats))
	mux.HandleFunc("/api/record_burn", corsMiddleware(handleRecordBurn))

	fmt.Println("ORPay Backend listening on :4000")
	log.Fatal(http.ListenAndServe(":4000", mux))
}

func handleRegister(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username string `json:"username"`
		Address  string `json:"address"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	db.mu.Lock()
	db.Users[req.Username] = req.Address
	db.mu.Unlock()

	w.WriteHeader(http.StatusOK)
}

func handleResolve(w http.ResponseWriter, r *http.Request) {
	username := r.URL.Query().Get("username")
	
	db.mu.RLock()
	address, exists := db.Users[username]
	db.mu.RUnlock()

	if !exists {
		http.Error(w, "User not found", http.StatusNotFound)
		return
	}

	json.NewEncoder(w).Encode(map[string]string{"address": address})
}

func handleStats(w http.ResponseWriter, r *http.Request) {
	burnMutex.RLock()
	burned := totalBurned
	burnMutex.RUnlock()

	json.NewEncoder(w).Encode(map[string]interface{}{
		"total_burned": burned,
		"fee_rate":     ConvenienceFee,
	})
}

func handleRecordBurn(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Amount float64 `json:"amount"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	burnMutex.Lock()
	totalBurned += req.Amount
	burnMutex.Unlock()

	w.WriteHeader(http.StatusOK)
}
