package api

import (
	"encoding/json"
	"net/http"

	"github.com/openreserve/node/consensus"
	"github.com/openreserve/node/ledger"
	"github.com/openreserve/node/p2p"
	"github.com/openreserve/node/reserve"
)

// Server provides an HTTP interface to the OpenReserve node
type Server struct {
	ListenAddr    string
	LedgerState   *ledger.State
	Consensus     *consensus.Engine
	P2P           *p2p.Server
	ReserveEngine *reserve.Engine
}

func NewServer(listenAddr string, state *ledger.State, cons *consensus.Engine, p *p2p.Server, res *reserve.Engine) *Server {
	return &Server{
		ListenAddr:    listenAddr,
		LedgerState:   state,
		Consensus:     cons,
		P2P:           p,
		ReserveEngine: res,
	}
}

// Start registers the routes and begins listening for HTTP requests
func (s *Server) Start() error {
	mux := http.NewServeMux()

	mux.HandleFunc("/balance", s.handleGetBalance)
	mux.HandleFunc("/transaction", s.handlePostTransaction)
	mux.HandleFunc("/validators", s.handleGetValidators)
	mux.HandleFunc("/reserve/audit", s.handleGetReserveAudit)

	return http.ListenAndServe(s.ListenAddr, mux)
}

func writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}
