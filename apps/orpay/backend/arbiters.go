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

// Arbiter marketplace: people apply to settle escrow disputes, ORPay admins
// vet them, and buyers and sellers rate them after each decision. Their
// record (disputes resolved, rating) is public so both sides can pick a
// neutral, proven arbiter.

type Arbiter struct {
	Address   types.Address  `json:"address"`
	Bio       string         `json:"bio"`
	Status    string         `json:"status"`            // pending, approved, rejected
	Ratings   map[string]int `json:"ratings,omitempty"` // "escrowID/rater" -> 1..5
	AppliedAt time.Time      `json:"applied_at"`
}

func (a *Arbiter) rating() (avg float64, n int) {
	sum := 0
	for _, v := range a.Ratings {
		sum += v
	}
	if n = len(a.Ratings); n > 0 {
		avg = float64(sum) / float64(n)
	}
	return
}

// score ranks arbiters: rating, with a small boost for experience.
func (a *Arbiter) score() float64 {
	avg, n := a.rating()
	if n == 0 {
		return 3
	}
	return avg + min(float64(n), 20)/20
}

func (s *server) applyArbiter(w http.ResponseWriter, r *http.Request) {
	me := reqauth.Caller(r)
	if _, ok := s.dir.byAddress(me); !ok {
		writeErr(w, http.StatusBadRequest, errors.New("claim a @username first"))
		return
	}
	var req struct {
		Bio string `json:"bio"`
	}
	if err := decodeBody(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	req.Bio = strings.TrimSpace(req.Bio)
	if len(req.Bio) < 20 || len(req.Bio) > 600 {
		writeErr(w, http.StatusBadRequest, errors.New("tell buyers and sellers about your experience in 20-600 characters"))
		return
	}
	s.arbiters.Update(func(m map[types.Address]*Arbiter) error {
		if a, ok := m[me]; ok {
			a.Bio = req.Bio
			if a.Status == "rejected" {
				a.Status = "pending"
			}
			return nil
		}
		m[me] = &Arbiter{Address: me, Bio: req.Bio, Status: "pending", AppliedAt: s.now()}
		return nil
	})
	writeJSON(w, http.StatusOK, map[string]string{"status": "pending"})
}

func (s *server) reviewArbiter(w http.ResponseWriter, r *http.Request) {
	if !slices.Contains(s.admins, reqauth.Caller(r)) {
		writeErr(w, http.StatusForbidden, errors.New("admins only"))
		return
	}
	var req struct {
		Decision string `json:"decision"`
	}
	decodeBody(r, &req)
	status := map[string]string{"approve": AppApproved, "reject": "rejected"}[req.Decision]
	if status == "" {
		writeErr(w, http.StatusBadRequest, errors.New("decision must be approve or reject"))
		return
	}
	err := s.arbiters.Update(func(m map[types.Address]*Arbiter) error {
		a, ok := m[types.Address(r.PathValue("addr"))]
		if !ok {
			return errNotFound
		}
		a.Status = status
		return nil
	})
	if err != nil {
		writeErr(w, http.StatusNotFound, errors.New("no such applicant"))
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": status})
}

// listArbiters returns approved arbiters with their public record; admins
// also see pending applications.
func (s *server) listArbiters(w http.ResponseWriter, r *http.Request) { s.writeArbiters(w, false) }

func (s *server) pendingArbiters(w http.ResponseWriter, r *http.Request) {
	if !slices.Contains(s.admins, reqauth.Caller(r)) {
		writeErr(w, http.StatusForbidden, errors.New("admins only"))
		return
	}
	s.writeArbiters(w, true)
}

func (s *server) writeArbiters(w http.ResponseWriter, isAdmin bool) {
	var as []Arbiter
	s.arbiters.Read(func(m map[types.Address]*Arbiter) {
		for _, a := range m {
			if a.Status == AppApproved || (isAdmin && a.Status == "pending") {
				as = append(as, *a)
			}
		}
	})
	out := []map[string]any{}
	for _, a := range as {
		avg, n := a.rating()
		resolved := 0
		if es, err := s.node.EscrowsOf(a.Address); err == nil {
			for _, e := range es {
				if e.Arbiter == a.Address && e.Status == "resolved" {
					resolved++
				}
			}
		}
		name, _ := s.dir.byAddress(a.Address)
		out = append(out, map[string]any{"address": a.Address, "username": name, "bio": a.Bio, "status": a.Status,
			"rating": avg, "ratings": n, "resolved": resolved, "score": a.score()})
	}
	slices.SortFunc(out, func(x, y map[string]any) int {
		switch {
		case x["score"].(float64) > y["score"].(float64):
			return -1
		case x["score"].(float64) < y["score"].(float64):
			return 1
		}
		return 0
	})
	writeJSON(w, http.StatusOK, out)
}

// rateArbiter: after a dispute is resolved, the buyer and the seller can
// each rate the arbiter once (1-5).
func (s *server) rateArbiter(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	me := reqauth.Caller(r)
	var req struct {
		Stars int `json:"stars"`
	}
	if err := decodeBody(r, &req); err != nil || req.Stars < 1 || req.Stars > 5 {
		writeErr(w, http.StatusBadRequest, errors.New("stars must be 1-5"))
		return
	}
	e, err := s.node.Escrow(id)
	if err != nil || e == nil {
		writeErr(w, http.StatusNotFound, errors.New("escrow not found"))
		return
	}
	if e.Status != "resolved" || (me != e.Buyer && me != e.Seller) {
		writeErr(w, http.StatusForbidden, errors.New("only the buyer and seller can rate, after the arbiter decides"))
		return
	}
	err = s.arbiters.Update(func(m map[types.Address]*Arbiter) error {
		a, ok := m[e.Arbiter]
		if !ok {
			return errors.New("this arbiter is not in the marketplace")
		}
		if a.Ratings == nil {
			a.Ratings = map[string]int{}
		}
		key := id + "/" + string(me)
		if _, done := a.Ratings[key]; done {
			return errors.New("you already rated this decision")
		}
		a.Ratings[key] = req.Stars
		return nil
	})
	if err != nil {
		writeErr(w, http.StatusConflict, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"rated": true})
}
