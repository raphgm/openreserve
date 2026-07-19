package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
)

type APIServer struct {
	DB   *Database
	Port string
}

func NewAPIServer(db *Database, port string) *APIServer {
	return &APIServer{
		DB:   db,
		Port: port,
	}
}

func (s *APIServer) Start() {
	mux := http.NewServeMux()

	mux.HandleFunc("/api/search", s.handleSearch)
	mux.HandleFunc("/api/metrics", s.handleMetrics)

	fmt.Printf("ORPScan API listening on %s\\n", s.Port)
	log.Fatal(http.ListenAndServe(s.Port, mux))
}

func (s *APIServer) handleSearch(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query().Get("q")
	if query == "" {
		http.Error(w, "missing query parameter 'q'", http.StatusBadRequest)
		return
	}

	result, err := s.DB.Search(query)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"query":  query,
		"result": result,
	})
}

func (s *APIServer) handleMetrics(w http.ResponseWriter, r *http.Request) {
	s.DB.mu.RLock()
	totalTxs := s.DB.TotalTransactions
	blocks := len(s.DB.BlocksByHeight)
	s.DB.mu.RUnlock()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"total_transactions": totalTxs,
		"total_blocks":       blocks,
	})
}
