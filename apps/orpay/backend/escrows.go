package main

import (
	"errors"
	"fmt"
	"log"
	"net/http"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/openreserve/node/client"
	"github.com/openreserve/node/types"
)

// An EscrowRequest is how a partner app (e.g. SSLabs holding a developer's
// payout until work is approved) asks a buyer to lock funds. The buyer funds
// it from their own wallet on ORPay; the on-chain escrow carries the
// request id as its ref, and this service follows it and sends webhooks.
// Funds are never held here: the ledger holds them under the escrow rules.

const (
	EscrowAwaiting = "awaiting_funding"
	EscrowLinked   = "linked" // funded on-chain; see Chain for its live state
	EscrowNoFund   = "expired"
)

type Milestone struct {
	Label  string       `json:"label"`
	Amount types.Amount `json:"amount"`
}

type EscrowRequest struct {
	ID          string         `json:"id"`
	AppID       string         `json:"app_id"`
	Seller      types.Address  `json:"seller"`
	Arbiter     types.Address  `json:"arbiter"`
	Asset       string         `json:"asset,omitempty"`
	Milestones  []Milestone    `json:"milestones"`
	ShipByDays  int            `json:"ship_by_days"`
	ReviewDays  int            `json:"review_days"`
	Description string         `json:"description"`
	Reference   string         `json:"reference,omitempty"`
	ReturnURL   string         `json:"return_url,omitempty"`
	Policy      string         `json:"policy,omitempty"`     // terms the buyer must accept
	TermsHash   string         `json:"terms_hash,omitempty"` // sha256 of Policy
	Status      string         `json:"status"`
	CreatedAt   time.Time      `json:"created_at"`
	ExpiresAt   time.Time      `json:"expires_at"` // funding deadline
	EscrowID    string         `json:"escrow_id,omitempty"`
	Chain       *client.Escrow `json:"chain,omitempty"`  // last seen on-chain state
	Events      []*hookEvent   `json:"events,omitempty"` // webhook deliveries still owed
}

// hookEvent is a webhook waiting to be delivered.
type hookEvent struct {
	Type   string         `json:"type"`
	Data   map[string]any `json:"data"`
	Tries  int            `json:"tries"`
	NextAt time.Time      `json:"next_at"`
}

func (r *EscrowRequest) total() types.Amount {
	var t types.Amount
	for _, m := range r.Milestones {
		t += m.Amount
	}
	return t
}

func (s *server) createEscrowRequest(w http.ResponseWriter, r *http.Request) {
	app := appFrom(r)
	var req struct {
		Seller     string `json:"seller"` // @username or address
		Arbiter    string `json:"arbiter"`
		Currency   string `json:"currency"`
		Milestones []struct {
			Label  string `json:"label"`
			Amount string `json:"amount"`
		} `json:"milestones"`
		ShipByDays  int    `json:"ship_by_days"`
		ReviewDays  int    `json:"review_days"`
		Description string `json:"description"`
		Reference   string `json:"reference"`
		ReturnURL   string `json:"return_url"`
		FundWithinH int    `json:"fund_within_hours"`
		Policy      string `json:"policy"` // defaults to the app's escrow policy
	}
	if err := decodeBody(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	bad := func(msg string, a ...any) { writeErr(w, http.StatusBadRequest, fmt.Errorf(msg, a...)) }
	seller, err := s.dir.resolveRef(req.Seller)
	if err != nil {
		bad("seller: %v", err)
		return
	}
	arbiter := app.Arbiter()
	if req.Arbiter != "" {
		if arbiter, err = s.dir.resolveRef(req.Arbiter); err != nil {
			bad("arbiter: %v", err)
			return
		}
	}
	if arbiter == seller {
		bad("the arbiter must be independent of the seller")
		return
	}
	asset, err := s.checkoutAsset(req.Currency)
	if err != nil {
		bad("%v", err)
		return
	}
	if len(req.Milestones) == 0 || len(req.Milestones) > types.MaxMilestones {
		bad("give 1-%d milestones", types.MaxMilestones)
		return
	}
	var ms []Milestone
	for i, m := range req.Milestones {
		amt, err := types.ParseAmount(m.Amount)
		if err != nil || amt == 0 || (asset == "NGN" && amt%10_000 != 0) {
			bad("milestone %d: amount must be a positive decimal string like \"25000.00\"", i+1)
			return
		}
		label := strings.TrimSpace(m.Label)
		if label == "" {
			label = fmt.Sprintf("Milestone %d", i+1)
		}
		if len(label) > 80 {
			bad("milestone %d: label too long", i+1)
			return
		}
		ms = append(ms, Milestone{Label: label, Amount: amt})
	}
	if req.ShipByDays == 0 {
		req.ShipByDays = 14
	}
	if req.ReviewDays == 0 {
		req.ReviewDays = 3
	}
	if req.FundWithinH == 0 {
		req.FundWithinH = 72
	}
	switch {
	case req.ShipByDays < 1 || req.ShipByDays > 365:
		bad("ship_by_days must be 1-365")
		return
	case req.ReviewDays < 1 || req.ReviewDays > 90:
		bad("review_days must be 1-90")
		return
	case req.FundWithinH < 1 || req.FundWithinH > 30*24:
		bad("fund_within_hours must be 1-720")
		return
	case len(req.Description) > 280 || len(req.Reference) > 100:
		bad("description max 280 chars, reference max 100")
		return
	}
	if req.ReturnURL != "" {
		if err := validURL(req.ReturnURL, false); err != nil {
			bad("%v", err)
			return
		}
	}
	policy := strings.TrimSpace(req.Policy)
	if policy == "" {
		policy = app.EscrowPolicy
	}
	var termsHash string
	if policy != "" {
		if termsHash, err = s.storeTerms(policy); err != nil {
			bad("policy: %v", err)
			return
		}
	}
	now := s.now()
	er := &EscrowRequest{
		Policy: policy, TermsHash: termsHash,
		ID: "esr_" + randToken("", 10), AppID: app.ID, Seller: seller, Arbiter: arbiter, Asset: asset,
		Milestones: ms, ShipByDays: req.ShipByDays, ReviewDays: req.ReviewDays, Description: req.Description,
		Reference: req.Reference, ReturnURL: req.ReturnURL, Status: EscrowAwaiting, CreatedAt: now,
		ExpiresAt: now.Add(time.Duration(req.FundWithinH) * time.Hour),
	}
	if err := s.escrows.Update(func(m map[string]*EscrowRequest) error { m[er.ID] = er; return nil }); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusCreated, s.escrowView(er, true))
}

func (s *server) escrowView(er *EscrowRequest, forApp bool) map[string]any {
	ms := make([]map[string]any, len(er.Milestones))
	for i, m := range er.Milestones {
		ms[i] = map[string]any{"label": m.Label, "amount": m.Amount, "amount_display": types.FormatAmount(m.Amount)}
	}
	v := map[string]any{
		"id": er.ID, "app_id": er.AppID, "seller": er.Seller, "arbiter": er.Arbiter,
		"currency": types.AssetName(er.Asset), "asset": er.Asset, "milestones": ms, "total": er.total(),
		"ship_by_days": er.ShipByDays, "review_days": er.ReviewDays, "description": er.Description,
		"status": er.Status, "created_at": er.CreatedAt, "expires_at": er.ExpiresAt,
		"funding_url": s.publicURL + "/?escrow_request=" + er.ID,
	}
	if er.Policy != "" {
		v["policy"], v["terms_hash"], v["terms_memo"] = er.Policy, er.TermsHash, "terms:"+er.TermsHash
	}
	if er.EscrowID != "" {
		v["escrow_id"] = er.EscrowID
		v["escrow_url"] = s.publicURL + "/?escrow=" + er.EscrowID
	}
	if er.Chain != nil {
		v["escrow"] = er.Chain
	}
	if forApp {
		v["reference"], v["return_url"] = er.Reference, er.ReturnURL
	}
	s.apps.Read(func(apps map[string]*App) {
		if a, ok := apps[er.AppID]; ok {
			v["app"] = a.public()
		}
	})
	return v
}

func (s *server) escrowRequest(id string) *EscrowRequest {
	var out *EscrowRequest
	s.escrows.Read(func(m map[string]*EscrowRequest) {
		if er, ok := m[id]; ok {
			c := *er
			out = &c
		}
	})
	return out
}

// getEscrowRequest is public: the funding page and escrow screen use it
// (e.g. for milestone labels).
func (s *server) getEscrowRequest(w http.ResponseWriter, r *http.Request) {
	er := s.escrowRequest(r.PathValue("id"))
	if er == nil {
		writeErr(w, http.StatusNotFound, errors.New("escrow request not found"))
		return
	}
	writeJSON(w, http.StatusOK, s.escrowView(er, false))
}

func (s *server) getAppEscrow(w http.ResponseWriter, r *http.Request) {
	er := s.escrowRequest(r.PathValue("id"))
	if er == nil || er.AppID != appFrom(r).ID {
		writeErr(w, http.StatusNotFound, errors.New("escrow not found"))
		return
	}
	writeJSON(w, http.StatusOK, s.escrowView(er, true))
}

func (s *server) listAppEscrows(w http.ResponseWriter, r *http.Request) {
	app := appFrom(r)
	var list []*EscrowRequest
	s.escrows.Read(func(m map[string]*EscrowRequest) {
		for _, er := range m {
			if er.AppID == app.ID {
				c := *er
				list = append(list, &c)
			}
		}
	})
	sort.Slice(list, func(i, j int) bool { return list[i].CreatedAt.After(list[j].CreatedAt) })
	out := []map[string]any{}
	for i, er := range list {
		if i == 200 {
			break
		}
		out = append(out, s.escrowView(er, true))
	}
	writeJSON(w, http.StatusOK, out)
}

// checkEscrows links funded requests to their on-chain escrow and turns
// every on-chain change into a webhook event.
func (s *server) checkEscrows() error {
	var todo []EscrowRequest
	s.escrows.Read(func(m map[string]*EscrowRequest) {
		for _, er := range m {
			if er.Status == EscrowAwaiting || (er.Status == EscrowLinked && (er.Chain == nil || isOpen(er.Chain.Status))) {
				todo = append(todo, *er)
			}
		}
	})
	var firstErr error
	for _, er := range todo {
		var chain *client.Escrow
		var err error
		if er.EscrowID == "" {
			chain, err = s.findFunding(&er)
		} else {
			chain, err = s.node.Escrow(er.EscrowID)
		}
		if err != nil {
			firstErr = err
			continue
		}
		now := s.now()
		s.escrows.Update(func(m map[string]*EscrowRequest) error {
			cur := m[er.ID]
			if cur == nil {
				return nil
			}
			if chain == nil {
				if cur.Status == EscrowAwaiting && now.After(cur.ExpiresAt) {
					cur.Status = EscrowNoFund
					s.queueEscrowEvent(cur, "escrow.expired", now)
				}
				return nil
			}
			if cur.EscrowID == "" {
				cur.EscrowID, cur.Status = chain.ID, EscrowLinked
			}
			for _, ev := range escrowEvents(cur.Chain, chain) {
				s.queueEscrowEvent(cur, ev, now)
			}
			cur.Chain = chain
			return nil
		})
	}
	return firstErr
}

// findFunding looks for the on-chain escrow that funds a request: same
// ref, seller, arbiter, asset and exact milestones.
func (s *server) findFunding(er *EscrowRequest) (*client.Escrow, error) {
	list, err := s.node.EscrowsOf(er.Seller)
	if err != nil {
		return nil, err
	}
	want := make([]types.Amount, len(er.Milestones))
	for i, m := range er.Milestones {
		want[i] = m.Amount
	}
	for _, e := range list {
		if e.Ref == er.ID && e.Arbiter == er.Arbiter && e.Asset == er.Asset && slices.Equal(e.Milestones, want) {
			// The buyer must have accepted these exact terms in the create tx.
			if er.TermsHash != "" {
				memo, err := s.node.EscrowCreateMemo(e.ID)
				if err != nil {
					return nil, err
				}
				if memo != "terms:"+er.TermsHash {
					continue
				}
			}
			return e, nil
		}
	}
	return nil, nil
}

func isOpen(status string) bool {
	return status == "funded" || status == "dispatched" || status == "disputed"
}

// escrowEvents lists what changed between two observations, in order.
func escrowEvents(prev, cur *client.Escrow) []string {
	var out []string
	if prev == nil {
		out = append(out, "escrow.funded")
		prev = &client.Escrow{Status: "funded"}
	}
	if prev.DispatchedAt == 0 && cur.DispatchedAt != 0 {
		out = append(out, "escrow.dispatched")
	}
	for i := prev.Released; i < cur.Released; i++ {
		out = append(out, "escrow.milestone_released")
	}
	if prev.Status != "disputed" && cur.Status == "disputed" {
		out = append(out, "escrow.disputed")
	}
	if prev.Status != cur.Status {
		switch cur.Status {
		case "completed", "refunded", "resolved":
			out = append(out, "escrow."+cur.Status)
		}
	}
	return out
}

func (s *server) queueEscrowEvent(er *EscrowRequest, typ string, now time.Time) {
	data := s.escrowView(er, true)
	data["event_at"] = now
	er.Events = append(er.Events, &hookEvent{Type: typ, Data: data, NextAt: now})
}

// deliverEscrowEvents sends owed escrow webhooks in order, one request at a time.
func (s *server) deliverEscrowEvents() {
	type job struct {
		id, url, secret string
		ev              hookEvent
	}
	var jobs []job
	now := s.now()
	s.escrows.Read(func(m map[string]*EscrowRequest) {
		for _, er := range m {
			if len(er.Events) > 0 && !now.Before(er.Events[0].NextAt) {
				jobs = append(jobs, job{id: er.ID, ev: *er.Events[0]})
			}
		}
	})
	// Lock order is always escrows then apps (never nested the other way).
	for i := range jobs {
		var appID string
		s.escrows.Read(func(m map[string]*EscrowRequest) { appID = m[jobs[i].id].AppID })
		s.apps.Read(func(apps map[string]*App) {
			if a, ok := apps[appID]; ok {
				jobs[i].url, jobs[i].secret = a.WebhookURL, a.WebhookSecret
			}
		})
	}
	for _, j := range jobs {
		err := errors.New("no webhook URL configured")
		if j.url != "" {
			err = s.postEvent(j.url, j.secret, j.ev.Type, j.ev.Data)
		}
		s.escrows.Update(func(m map[string]*EscrowRequest) error {
			er := m[j.id]
			if er == nil || len(er.Events) == 0 {
				return nil
			}
			head := er.Events[0]
			if err == nil || j.url == "" {
				er.Events = er.Events[1:]
				return nil
			}
			head.Tries++
			if head.Tries >= len(hookBackoff) {
				log.Printf("escrow webhook %s for %s abandoned: %v", head.Type, er.ID, err)
				er.Events = er.Events[1:]
				return nil
			}
			head.NextAt = s.now().Add(hookBackoff[head.Tries])
			return nil
		})
	}
}
