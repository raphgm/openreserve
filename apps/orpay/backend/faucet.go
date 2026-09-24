package main

import (
	"crypto/ed25519"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/openreserve/node/types"
)

// faucet sends test ORP from a funded devnet key. The last payout time per
// address is persisted so the cooldown survives restarts.
type faucet struct {
	mu       sync.Mutex // serializes sends so nonces never collide
	key      ed25519.PrivateKey
	amount   types.Amount
	cooldown time.Duration
	last     *jsonStore[map[types.Address]time.Time]
}

func newFaucet(key ed25519.PrivateKey, amount types.Amount, cooldown time.Duration, path string) (*faucet, error) {
	last, err := openStore(path, map[types.Address]time.Time{})
	if err != nil {
		return nil, err
	}
	return &faucet{key: key, amount: amount, cooldown: cooldown, last: last}, nil
}

func (s *server) drip(w http.ResponseWriter, r *http.Request) {
	f := s.faucet
	if f == nil {
		writeErr(w, http.StatusNotFound, errors.New("faucet is disabled on this network"))
		return
	}
	var req struct {
		Address types.Address `json:"address"`
	}
	if err := decodeBody(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if err := req.Address.Validate(); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}

	f.mu.Lock()
	defer f.mu.Unlock()
	var last time.Time
	f.last.read(func(m map[types.Address]time.Time) { last = m[req.Address] })
	if wait := f.cooldown - s.now().Sub(last); wait > 0 {
		writeErr(w, http.StatusTooManyRequests, fmt.Errorf("try again in %s", wait.Round(time.Minute)))
		return
	}
	id, err := s.node.send(f.key, req.Address, f.amount, "ORPay faucet")
	if err != nil {
		writeErr(w, http.StatusBadGateway, err)
		return
	}
	now := s.now()
	f.last.update(func(m map[types.Address]time.Time) error {
		m[req.Address] = now
		// Forget entries past their cooldown so the file stays small.
		for a, t := range m {
			if now.Sub(t) > f.cooldown {
				delete(m, a)
			}
		}
		return nil
	})
	writeJSON(w, http.StatusAccepted, map[string]any{"id": id, "amount": f.amount})
}
