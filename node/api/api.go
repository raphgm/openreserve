// Package api exposes the chain over a JSON HTTP API.
package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/openreserve/node/chain"
	"github.com/openreserve/node/types"
)

const maxBody = 64 << 10

// Handler returns the HTTP handler for the node API.
func Handler(c *chain.Chain) http.Handler {
	s := &server{c: c}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/status", s.status)
	mux.HandleFunc("GET /v1/genesis", s.genesis)
	mux.HandleFunc("GET /v1/accounts/{addr}", s.account)
	mux.HandleFunc("GET /v1/accounts/{addr}/txs", s.history)
	mux.HandleFunc("GET /v1/blocks", s.blocks)
	mux.HandleFunc("GET /v1/blocks/{height}", s.block)
	mux.HandleFunc("GET /v1/txs/{id}", s.tx)
	mux.HandleFunc("POST /v1/txs", s.submit)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { w.Write([]byte("ok\n")) })
	return cors(mux)
}

type server struct{ c *chain.Chain }

func (s *server) status(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, s.c.Status())
}

func (s *server) genesis(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, s.c.Genesis())
}

func (s *server) account(w http.ResponseWriter, r *http.Request) {
	addr := types.Address(r.PathValue("addr"))
	if err := addr.Validate(); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	acc, next := s.c.Account(addr)
	writeJSON(w, http.StatusOK, map[string]any{
		"address":     addr,
		"balance":     acc.Balance,
		"balance_orp": types.FormatAmount(acc.Balance),
		"nonce":       acc.Nonce,
		"next_nonce":  next,
	})
}

func (s *server) history(w http.ResponseWriter, r *http.Request) {
	addr := types.Address(r.PathValue("addr"))
	if err := addr.Validate(); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, s.c.History(addr, queryInt(r, "limit", 50, 500)))
}

// blocks lists blocks from ?from=. With ?wait=N (seconds) it long-polls until
// a block at that height exists, which is how followers stay in sync.
func (s *server) blocks(w http.ResponseWriter, r *http.Request) {
	from, _ := strconv.ParseUint(r.URL.Query().Get("from"), 10, 64)
	limit := queryInt(r, "limit", 100, 500)
	if wait := queryInt(r, "wait", 0, 60); wait > 0 && from > s.c.Height() {
		timer := time.NewTimer(time.Duration(wait) * time.Second)
		defer timer.Stop()
		for from > s.c.Height() {
			select {
			case <-s.c.Updated():
			case <-timer.C:
				writeJSON(w, http.StatusOK, []*types.Block{})
				return
			case <-r.Context().Done():
				return
			}
		}
	}
	out := s.c.Blocks(from, limit)
	if out == nil {
		out = []*types.Block{}
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *server) block(w http.ResponseWriter, r *http.Request) {
	h, err := strconv.ParseUint(r.PathValue("height"), 10, 64)
	if err != nil {
		writeErr(w, http.StatusBadRequest, errors.New("height must be a number"))
		return
	}
	b := s.c.Block(h)
	if b == nil {
		writeErr(w, http.StatusNotFound, errors.New("block not found"))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"hash": b.Header.Hash(), "block": b})
}

func (s *server) tx(w http.ResponseWriter, r *http.Request) {
	id, err := types.ParseHash(r.PathValue("id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	tx, loc := s.c.Tx(id)
	if tx == nil {
		writeErr(w, http.StatusNotFound, errors.New("tx not found"))
		return
	}
	status := "pending"
	if loc != nil {
		status = "committed"
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": id, "status": status, "location": loc, "tx": tx})
}

func (s *server) submit(w http.ResponseWriter, r *http.Request) {
	var tx types.Tx
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBody))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&tx); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	id, err := s.c.Submit(&tx)
	if errors.Is(err, chain.ErrDuplicate) {
		writeJSON(w, http.StatusOK, map[string]any{"id": id, "status": "duplicate"})
		return
	}
	if err != nil {
		writeErr(w, http.StatusUnprocessableEntity, err)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"id": id, "status": "pending"})
}

func queryInt(r *http.Request, key string, def, max int) int {
	v, err := strconv.Atoi(r.URL.Query().Get(key))
	if err != nil || v <= 0 {
		return def
	}
	return min(v, max)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, err error) {
	writeJSON(w, status, map[string]string{"error": err.Error()})
}

// cors allows browser wallets and explorers on other origins to read the API.
// The API holds no cookies or credentials, so a wildcard origin is safe.
func cors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		if r.Method == http.MethodOptions {
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}
