package main

import (
	"errors"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/openreserve/node/reqauth"
	"github.com/openreserve/node/types"
)

// Seller ratings that can't be faked: only the buyer of a completed escrow
// can rate that seller, once per escrow. Every review links to a real,
// on-chain purchase.

type Review struct {
	Escrow string        `json:"escrow"`
	Buyer  types.Address `json:"buyer"`
	Stars  int           `json:"stars"`
	Text   string        `json:"text,omitempty"`
	At     time.Time     `json:"at"`
}

type ratingStore = map[types.Address]map[string]*Review // seller -> escrow id -> review

func (s *server) rateSeller(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	me := reqauth.Caller(r)
	var req struct {
		Stars int    `json:"stars"`
		Text  string `json:"text"`
	}
	if err := decodeBody(r, &req); err != nil || req.Stars < 1 || req.Stars > 5 {
		writeErr(w, http.StatusBadRequest, errors.New("stars must be 1-5"))
		return
	}
	req.Text = strings.TrimSpace(req.Text)
	if len(req.Text) > 280 {
		writeErr(w, http.StatusBadRequest, errors.New("keep the review under 280 characters"))
		return
	}
	e, err := s.node.Escrow(id)
	if err != nil || e == nil {
		writeErr(w, http.StatusNotFound, errors.New("escrow not found"))
		return
	}
	if e.Buyer != me {
		writeErr(w, http.StatusForbidden, errors.New("only the buyer can rate the seller"))
		return
	}
	if e.Status != "completed" && e.Status != "resolved" {
		writeErr(w, http.StatusConflict, errors.New("you can rate the seller once the escrow is finished"))
		return
	}
	err = s.ratings.Update(func(m ratingStore) error {
		if m[e.Seller] == nil {
			m[e.Seller] = map[string]*Review{}
		}
		if _, done := m[e.Seller][id]; done {
			return errors.New("you already rated this purchase")
		}
		m[e.Seller][id] = &Review{Escrow: id, Buyer: me, Stars: req.Stars, Text: req.Text, At: s.now()}
		return nil
	})
	if err != nil {
		writeErr(w, http.StatusConflict, err)
		return
	}
	s.trust.mu.Lock()
	delete(s.trust.m, e.Seller) // refresh their trust profile
	s.trust.mu.Unlock()
	writeJSON(w, http.StatusOK, map[string]bool{"rated": true})
}

type sellerRating struct {
	Average float64  `json:"average"`
	Count   int      `json:"count"`
	Recent  []Review `json:"recent"`
}

func (s *server) ratingOf(a types.Address) sellerRating {
	var out sellerRating
	var rs []Review
	s.ratings.Read(func(m ratingStore) {
		for _, rv := range m[a] {
			rs = append(rs, *rv)
		}
	})
	sum := 0
	for _, rv := range rs {
		sum += rv.Stars
	}
	if out.Count = len(rs); out.Count > 0 {
		out.Average = float64(sum) / float64(out.Count)
	}
	slices.SortFunc(rs, func(x, y Review) int { return y.At.Compare(x.At) })
	out.Recent = rs[:min(len(rs), 5)]
	return out
}

// myReview tells a buyer whether they've rated an escrow yet.
func (s *server) myReview(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	me := reqauth.Caller(r)
	var found *Review
	s.ratings.Read(func(m ratingStore) {
		for _, byEscrow := range m {
			if rv, ok := byEscrow[id]; ok && rv.Buyer == me {
				c := *rv
				found = &c
			}
		}
	})
	writeJSON(w, http.StatusOK, map[string]any{"review": found})
}
