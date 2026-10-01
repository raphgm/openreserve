package main

import (
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/openreserve/node/reqauth"
	"github.com/openreserve/node/types"
)

// Ajo invites. An on-chain pool needs its full member list up front, but an
// organiser rarely knows everyone's wallet address. So the organiser opens
// a draft and shares an invite link; people join the draft with their
// wallet; the organiser then creates the pool on-chain with the joined
// members (payout order = join order unless rearranged) and everyone joins
// it there. Nothing here holds money.

const (
	DraftOpen    = "open"
	DraftStarted = "started"
	maxDraftSize = types.MaxPoolMembers
)

type PoolDraft struct {
	ID           string          `json:"id"`
	Organizer    types.Address   `json:"organizer"`
	Name         string          `json:"name"`
	Asset        string          `json:"asset,omitempty"`
	Contribution types.Amount    `json:"contribution"`
	RoundSecs    int64           `json:"round_secs,omitempty"`
	Deposit      types.Amount    `json:"deposit,omitempty"`
	Mode         string          `json:"mode,omitempty"`
	InsuranceBps uint32          `json:"insurance_bps,omitempty"`
	Target       types.Amount    `json:"target,omitempty"`
	Slots        int             `json:"slots"`
	Members      []types.Address `json:"members"` // organizer first
	Status       string          `json:"status"`
	PoolID       string          `json:"pool_id,omitempty"`
	CreatedAt    time.Time       `json:"created_at"`
}

func (s *server) createDraft(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name         string       `json:"name"`
		Asset        string       `json:"asset"`
		Contribution types.Amount `json:"contribution"`
		RoundSecs    int64        `json:"round_secs"`
		Deposit      types.Amount `json:"deposit"`
		Mode         string       `json:"mode"`
		InsuranceBps uint32       `json:"insurance_bps"`
		Target       types.Amount `json:"target"`
		Slots        int          `json:"slots"`
	}
	if err := decodeBody(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	switch {
	case req.Name == "" || len(req.Name) > types.MaxPoolName:
		writeErr(w, http.StatusBadRequest, fmt.Errorf("pool name must be 1-%d characters", types.MaxPoolName))
		return
	case req.Contribution == 0:
		writeErr(w, http.StatusBadRequest, errors.New("contribution must be positive"))
		return
	case req.Slots < 2 || req.Slots > maxDraftSize:
		writeErr(w, http.StatusBadRequest, fmt.Errorf("a pool needs 2-%d members", maxDraftSize))
		return
	case !types.ValidAsset(req.Asset):
		writeErr(w, http.StatusBadRequest, errors.New("invalid currency"))
		return
	}
	// Check the circle options exactly as the chain will when the pool is created.
	probe := &types.Tx{From: reqauth.Caller(r), Pool: &types.PoolOp{Op: types.PoolCreate, Name: req.Name,
		Members: []types.Address{reqauth.Caller(r), types.Address(strings.Repeat("0", 64))}, Contribution: req.Contribution,
		RoundSecs: req.RoundSecs, Deposit: req.Deposit, Mode: req.Mode, InsuranceBps: req.InsuranceBps, Target: req.Target}}
	if err := probe.CheckPool(); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	d := &PoolDraft{
		ID: "ajo_" + randToken("", 8), Organizer: reqauth.Caller(r), Name: req.Name, Asset: req.Asset,
		Contribution: req.Contribution, RoundSecs: req.RoundSecs, Deposit: req.Deposit, Slots: req.Slots,
		Mode: req.Mode, InsuranceBps: req.InsuranceBps, Target: req.Target,
		Members: []types.Address{reqauth.Caller(r)}, Status: DraftOpen, CreatedAt: s.now(),
	}
	if err := s.drafts.Update(func(m map[string]*PoolDraft) error { m[d.ID] = d; return nil }); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusCreated, s.draftView(d))
}

func (s *server) draftView(d *PoolDraft) map[string]any {
	return map[string]any{
		"id": d.ID, "organizer": d.Organizer, "name": d.Name, "asset": d.Asset, "contribution": d.Contribution,
		"round_secs": d.RoundSecs, "deposit": d.Deposit, "slots": d.Slots, "members": d.Members,
		"mode": d.Mode, "insurance_bps": d.InsuranceBps, "target": d.Target,
		"status": d.Status, "pool_id": d.PoolID, "created_at": d.CreatedAt,
		"invite_url": s.publicURL + "/?ajo_invite=" + d.ID,
	}
}

func (s *server) draft(id string) *PoolDraft {
	var out *PoolDraft
	s.drafts.Read(func(m map[string]*PoolDraft) {
		if d, ok := m[id]; ok {
			c := *d
			c.Members = slices.Clone(d.Members)
			out = &c
		}
	})
	return out
}

// myDrafts lists open invites the caller organises or has joined.
func (s *server) myDrafts(w http.ResponseWriter, r *http.Request) {
	me := reqauth.Caller(r)
	out := []map[string]any{}
	s.drafts.Read(func(m map[string]*PoolDraft) {
		for _, d := range m {
			if d.Status == DraftOpen && slices.Contains(d.Members, me) {
				out = append(out, s.draftView(d))
			}
		}
	})
	writeJSON(w, http.StatusOK, out)
}

func (s *server) getDraft(w http.ResponseWriter, r *http.Request) {
	d := s.draft(r.PathValue("id"))
	if d == nil {
		writeErr(w, http.StatusNotFound, errors.New("invite not found"))
		return
	}
	writeJSON(w, http.StatusOK, s.draftView(d))
}

// changeDraft applies fn to an open draft under the store lock.
func (s *server) changeDraft(w http.ResponseWriter, r *http.Request, fn func(d *PoolDraft) error) {
	var out *PoolDraft
	err := s.drafts.Update(func(m map[string]*PoolDraft) error {
		d, ok := m[r.PathValue("id")]
		if !ok {
			return errors.New("invite not found")
		}
		if err := fn(d); err != nil {
			return err
		}
		c := *d
		out = &c
		return nil
	})
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, s.draftView(out))
}

func (s *server) joinDraft(w http.ResponseWriter, r *http.Request) {
	me := reqauth.Caller(r)
	s.changeDraft(w, r, func(d *PoolDraft) error {
		switch {
		case d.Status != DraftOpen:
			return errors.New("this pool has already started")
		case slices.Contains(d.Members, me):
			return nil // already in
		case len(d.Members) >= d.Slots:
			return errors.New("this pool is full")
		}
		d.Members = append(d.Members, me)
		return nil
	})
}

func (s *server) leaveDraft(w http.ResponseWriter, r *http.Request) {
	me := reqauth.Caller(r)
	s.changeDraft(w, r, func(d *PoolDraft) error {
		if d.Status != DraftOpen {
			return errors.New("this pool has already started")
		}
		if me == d.Organizer {
			return errors.New("the organiser cannot leave; start or abandon the pool instead")
		}
		d.Members = slices.DeleteFunc(d.Members, func(a types.Address) bool { return a == me })
		return nil
	})
}

// orderDraft lets the organiser set the payout order (same members).
func (s *server) orderDraft(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Members []types.Address `json:"members"`
	}
	if err := decodeBody(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	me := reqauth.Caller(r)
	s.changeDraft(w, r, func(d *PoolDraft) error {
		if me != d.Organizer || d.Status != DraftOpen {
			return errors.New("only the organiser can change the order before the pool starts")
		}
		a, b := slices.Clone(req.Members), slices.Clone(d.Members)
		slices.Sort(a)
		slices.Sort(b)
		if !slices.Equal(a, b) {
			return errors.New("the new order must list exactly the joined members")
		}
		d.Members = req.Members
		return nil
	})
}

// startedDraft records the on-chain pool the organiser created from this
// draft. It is checked against the chain: same members in the same order,
// same contribution and schedule.
func (s *server) startedDraft(w http.ResponseWriter, r *http.Request) {
	var req struct {
		PoolID string `json:"pool_id"`
	}
	if err := decodeBody(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	var pool struct {
		Pool struct {
			Creator      types.Address   `json:"creator"`
			Members      []types.Address `json:"members"`
			Contribution types.Amount    `json:"contribution"`
			Asset        string          `json:"asset"`
			RoundSecs    int64           `json:"round_secs"`
			Deposit      types.Amount    `json:"deposit"`
			Mode         string          `json:"mode"`
			InsuranceBps uint32          `json:"insurance_bps"`
			Target       types.Amount    `json:"target"`
		} `json:"pool"`
	}
	if err := s.node.GetJSON("/v1/pools/"+req.PoolID, &pool); err != nil {
		writeErr(w, http.StatusBadRequest, errors.New("pool not found on-chain"))
		return
	}
	me := reqauth.Caller(r)
	p := pool.Pool
	s.changeDraft(w, r, func(d *PoolDraft) error {
		switch {
		case me != d.Organizer || p.Creator != me:
			return errors.New("only the organiser can start the pool")
		case d.Status != DraftOpen:
			return errors.New("already started")
		case !slices.Equal(p.Members, d.Members) || p.Contribution != d.Contribution || p.Asset != d.Asset ||
			p.RoundSecs != d.RoundSecs || p.Deposit != d.Deposit || p.Mode != d.Mode || p.InsuranceBps != d.InsuranceBps || p.Target != d.Target:
			return errors.New("the on-chain pool does not match this invite")
		}
		d.Status, d.PoolID = DraftStarted, req.PoolID
		return nil
	})
}

// removeFromDraft lets the organiser remove someone before the pool starts.
func (s *server) removeFromDraft(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Member types.Address `json:"member"`
	}
	if err := decodeBody(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	me := reqauth.Caller(r)
	s.changeDraft(w, r, func(d *PoolDraft) error {
		switch {
		case me != d.Organizer:
			return errors.New("only the organiser can remove members")
		case d.Status != DraftOpen:
			return errors.New("the pool has already started")
		case req.Member == d.Organizer:
			return errors.New("the organiser cannot be removed")
		}
		d.Members = slices.DeleteFunc(d.Members, func(a types.Address) bool { return a == req.Member })
		return nil
	})
}
