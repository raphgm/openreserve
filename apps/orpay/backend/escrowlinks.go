package main

import (
	"errors"
	"net/http"
	"slices"

	"github.com/openreserve/node/reqauth"
	"github.com/openreserve/node/types"
)

// Escrow links: anyone selling on Instagram, WhatsApp, TikTok or Jiji can
// create an escrow link from their wallet (no API key) and share it. The
// buyer opens it, reads the terms, and locks the money until they have
// inspected the goods. Disputes go to a vetted arbiter.

func (s *server) createEscrowLink(w http.ResponseWriter, r *http.Request) {
	me := reqauth.Caller(r)
	if _, ok := s.dir.byAddress(me); !ok {
		writeErr(w, http.StatusBadRequest, errors.New("claim a @username first so buyers can see who they are paying"))
		return
	}
	var req escrowInput
	if err := decodeBody(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	er, err := s.newEscrowRequest("", me, s.defaultArbiter(me), "", req)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusCreated, s.escrowView(er, true))
}

func (s *server) myEscrowLinks(w http.ResponseWriter, r *http.Request) {
	me := reqauth.Caller(r)
	var ers []*EscrowRequest
	s.escrows.Read(func(m map[string]*EscrowRequest) {
		for _, er := range m {
			if er.AppID == "" && er.Seller == me {
				c := *er
				ers = append(ers, &c)
			}
		}
	})
	slices.SortFunc(ers, func(a, b *EscrowRequest) int { return b.CreatedAt.Compare(a.CreatedAt) })
	out := []map[string]any{}
	for _, er := range ers[:min(len(ers), 50)] {
		out = append(out, s.escrowView(er, true))
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *server) cancelEscrowLink(w http.ResponseWriter, r *http.Request) {
	me := reqauth.Caller(r)
	var out *EscrowRequest
	err := s.escrows.Update(func(m map[string]*EscrowRequest) error {
		er, ok := m[r.PathValue("id")]
		if !ok || er.AppID != "" || er.Seller != me {
			return errNotFound
		}
		if er.Status != EscrowAwaiting {
			return errors.New("only links nobody has paid yet can be cancelled")
		}
		er.Status = EscrowCancel
		c := *er
		out = &c
		return nil
	})
	switch {
	case errors.Is(err, errNotFound):
		writeErr(w, http.StatusNotFound, errors.New("escrow link not found"))
	case err != nil:
		writeErr(w, http.StatusConflict, err)
	default:
		writeJSON(w, http.StatusOK, s.escrowView(out, true))
	}
}

// defaultArbiter picks the best-rated approved arbiter who isn't the
// seller, falling back to an ORPay admin.
func (s *server) defaultArbiter(seller types.Address) types.Address {
	var best *Arbiter
	s.arbiters.Read(func(m map[types.Address]*Arbiter) {
		for _, a := range m {
			if a.Status != AppApproved || a.Address == seller {
				continue
			}
			if best == nil || a.score() > best.score() {
				c := *a
				best = &c
			}
		}
	})
	if best != nil {
		return best.Address
	}
	for _, a := range s.admins {
		if a != seller {
			return a
		}
	}
	return ""
}
