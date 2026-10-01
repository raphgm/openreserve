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

// Referrals: everyone's invite link is /?ref=<their username>. A new user
// records who invited them once, right after claiming their own username.
// ORPay counts who brought in active users (anyone who has made a payment);
// rewards can be paid against these counts later.

type referral struct {
	By types.Address `json:"by"`
	At time.Time     `json:"at"`
}

func (s *server) recordReferral(w http.ResponseWriter, r *http.Request) {
	me := reqauth.Caller(r)
	var req struct {
		Ref string `json:"ref"`
	}
	if err := decodeBody(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	by, ok := s.dir.byName(strings.ToLower(strings.TrimPrefix(strings.TrimSpace(req.Ref), "@")))
	if !ok {
		writeErr(w, http.StatusNotFound, errors.New("no user with that invite code"))
		return
	}
	if by == me {
		writeErr(w, http.StatusBadRequest, errors.New("you can't refer yourself"))
		return
	}
	// Only new wallets count: no payments before they were invited.
	if hist, err := s.node.History(me); err == nil && len(hist) > 2 {
		writeErr(w, http.StatusConflict, errors.New("referrals are for new wallets"))
		return
	}
	err := s.referrals.Update(func(m map[types.Address]*referral) error {
		if _, done := m[me]; done {
			return errors.New("you were already referred")
		}
		m[me] = &referral{By: by, At: s.now()}
		return nil
	})
	if err != nil {
		writeErr(w, http.StatusConflict, err)
		return
	}
	go s.notify(by, notification{Title: "Someone joined with your link", Body: s.nameOrShort(me) + " joined ORPay from your invite.", URL: "/"})
	writeJSON(w, http.StatusOK, map[string]bool{"recorded": true})
}

// myReferrals lists who joined with the caller's link and who is active.
func (s *server) myReferrals(w http.ResponseWriter, r *http.Request) {
	me := reqauth.Caller(r)
	var people []types.Address
	s.referrals.Read(func(m map[types.Address]*referral) {
		for a, ref := range m {
			if ref.By == me {
				people = append(people, a)
			}
		}
	})
	slices.Sort(people)
	out := []map[string]any{}
	active := 0
	for _, a := range people {
		hist, _ := s.node.History(a)
		on := false
		for _, e := range hist {
			if e.Tx.From == a { // they've paid, saved or bought something themselves
				on = true
				break
			}
		}
		if on {
			active++
		}
		name, _ := s.dir.byAddress(a)
		out = append(out, map[string]any{"address": a, "username": name, "active": on})
	}
	code, _ := s.dir.byAddress(me)
	writeJSON(w, http.StatusOK, map[string]any{"code": code, "invited": len(people), "active": active, "people": out})
}
