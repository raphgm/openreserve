package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/openreserve/node/types"
)

// An Invoice is a checkout session an app creates for one payment. The
// customer pays on-chain from their own wallet to the app's settlement
// address with memo "inv:<id>"; the watcher sees it land and notifies the app.

const (
	InvoicePending = "pending"
	InvoicePaid    = "paid"
	InvoiceExpired = "expired"
)

type Invoice struct {
	ID          string        `json:"id"`
	AppID       string        `json:"app_id"`
	Merchant    types.Address `json:"merchant"`
	Amount      types.Amount  `json:"amount"`
	Description string        `json:"description"`
	Reference   string        `json:"reference,omitempty"` // the app's own order id
	ReturnURL   string        `json:"return_url,omitempty"`
	Status      string        `json:"status"`
	CreatedAt   time.Time     `json:"created_at"`
	ExpiresAt   time.Time     `json:"expires_at"`
	PaidAt      time.Time     `json:"paid_at,omitzero"`
	PaidTx      string        `json:"paid_tx,omitempty"`
	PaidBy      types.Address `json:"paid_by,omitempty"`
	Height      uint64        `json:"height,omitempty"`
	// Webhook delivery bookkeeping (not shown to payers).
	HookPending bool      `json:"hook_pending,omitempty"`
	HookTries   int       `json:"hook_tries,omitempty"`
	HookNextAt  time.Time `json:"hook_next_at,omitzero"`
}

// Memo is what the payment must carry.
func (inv *Invoice) Memo() string { return "inv:" + inv.ID }

type appCtxKey struct{}

func withApp(ctx context.Context, a *App) context.Context {
	return context.WithValue(ctx, appCtxKey{}, a)
}
func appFrom(r *http.Request) *App { a, _ := r.Context().Value(appCtxKey{}).(*App); return a }

// createCheckout is called by an app's server with its API key.
func (s *server) createCheckout(w http.ResponseWriter, r *http.Request) {
	app := appFrom(r)
	var req struct {
		Amount      string `json:"amount"` // decimal ORP, e.g. "12.50"
		Description string `json:"description"`
		Reference   string `json:"reference"`
		ReturnURL   string `json:"return_url"`
		ExpiresIn   int    `json:"expires_in_minutes"`
	}
	if err := decodeBody(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	amt, err := types.ParseAmount(req.Amount)
	if err != nil || amt == 0 {
		writeErr(w, http.StatusBadRequest, errors.New(`amount must be a positive decimal string, e.g. "12.50"`))
		return
	}
	if len(req.Description) > 140 || len(req.Reference) > 100 {
		writeErr(w, http.StatusBadRequest, errors.New("description max 140 chars, reference max 100"))
		return
	}
	if req.ReturnURL != "" {
		if err := validURL(req.ReturnURL, false); err != nil {
			writeErr(w, http.StatusBadRequest, err)
			return
		}
	}
	if req.ExpiresIn == 0 {
		req.ExpiresIn = 60
	}
	if req.ExpiresIn < 1 || req.ExpiresIn > 7*24*60 {
		writeErr(w, http.StatusBadRequest, errors.New("expires_in_minutes must be 1-10080"))
		return
	}
	now := s.now()
	inv := &Invoice{
		ID: randToken("", 10), AppID: app.ID, Merchant: app.Settlement, Amount: amt,
		Description: req.Description, Reference: req.Reference, ReturnURL: req.ReturnURL,
		Status: InvoicePending, CreatedAt: now, ExpiresAt: now.Add(time.Duration(req.ExpiresIn) * time.Minute),
	}
	if err := s.invoices.update(func(m map[string]*Invoice) error { m[inv.ID] = inv; return nil }); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusCreated, s.invoiceView(inv, true))
}

func (s *server) invoiceView(inv *Invoice, forApp bool) map[string]any {
	v := map[string]any{
		"id": inv.ID, "app_id": inv.AppID, "merchant": inv.Merchant, "amount": inv.Amount,
		"amount_orp": types.FormatAmount(inv.Amount), "description": inv.Description,
		"memo": inv.Memo(), "status": inv.Status, "created_at": inv.CreatedAt, "expires_at": inv.ExpiresAt,
		"checkout_url": s.publicURL + "/?invoice=" + inv.ID,
	}
	if inv.Status == InvoicePaid {
		v["paid_at"], v["paid_tx"], v["paid_by"], v["height"] = inv.PaidAt, inv.PaidTx, inv.PaidBy, inv.Height
		if inv.ReturnURL != "" {
			v["return_url"] = inv.ReturnURL
		}
	}
	if forApp {
		v["reference"] = inv.Reference
		v["return_url"] = inv.ReturnURL
	}
	s.apps.read(func(apps map[string]*App) {
		if a, ok := apps[inv.AppID]; ok {
			v["app"] = a.public()
		}
	})
	return v
}

// getInvoice is public: the checkout page and receipts use it.
func (s *server) getInvoice(w http.ResponseWriter, r *http.Request) {
	inv := s.invoice(r.PathValue("id"))
	if inv == nil {
		writeErr(w, http.StatusNotFound, errors.New("invoice not found"))
		return
	}
	writeJSON(w, http.StatusOK, s.invoiceView(inv, false))
}

func (s *server) getCheckout(w http.ResponseWriter, r *http.Request) {
	inv := s.invoice(r.PathValue("id"))
	if inv == nil || inv.AppID != appFrom(r).ID {
		writeErr(w, http.StatusNotFound, errors.New("invoice not found"))
		return
	}
	writeJSON(w, http.StatusOK, s.invoiceView(inv, true))
}

func (s *server) listCheckouts(w http.ResponseWriter, r *http.Request) {
	app := appFrom(r)
	status := r.URL.Query().Get("status")
	var list []*Invoice
	s.invoices.read(func(m map[string]*Invoice) {
		for _, inv := range m {
			if inv.AppID == app.ID && (status == "" || inv.Status == status) {
				c := *inv
				list = append(list, &c)
			}
		}
	})
	sort.Slice(list, func(i, j int) bool { return list[i].CreatedAt.After(list[j].CreatedAt) })
	out := []map[string]any{}
	for i, inv := range list {
		if i == 200 {
			break
		}
		out = append(out, s.invoiceView(inv, true))
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *server) invoice(id string) *Invoice {
	var out *Invoice
	s.invoices.read(func(m map[string]*Invoice) {
		if inv, ok := m[id]; ok {
			c := *inv
			out = &c
		}
	})
	return out
}

// watchInvoices marks invoices paid when a matching payment is on-chain,
// expires stale ones, and delivers webhooks.
func (s *server) watchInvoices(every time.Duration) {
	for range time.Tick(every) {
		if err := s.checkInvoices(); err != nil {
			log.Printf("invoice watcher: %v", err)
		}
		s.deliverWebhooks()
	}
}

func (s *server) checkInvoices() error {
	// Group pending invoices by merchant so each address is fetched once.
	byMerchant := map[types.Address][]string{}
	s.invoices.read(func(m map[string]*Invoice) {
		for id, inv := range m {
			if inv.Status == InvoicePending {
				byMerchant[inv.Merchant] = append(byMerchant[inv.Merchant], id)
			}
		}
	})
	var firstErr error
	for merchant, ids := range byMerchant {
		hist, err := s.node.history(merchant)
		if err != nil {
			firstErr = err
			continue
		}
		payments := map[string][]historyEntry{} // memo -> payments, oldest first
		for i := len(hist) - 1; i >= 0; i-- {
			if e := hist[i]; e.Tx.To == merchant && strings.HasPrefix(e.Tx.Memo, "inv:") {
				payments[e.Tx.Memo] = append(payments[e.Tx.Memo], e)
			}
		}
		now := s.now()
		s.invoices.update(func(m map[string]*Invoice) error {
			for _, id := range ids {
				inv := m[id]
				if inv == nil || inv.Status != InvoicePending {
					continue
				}
				if p, ok := firstCovering(payments[inv.Memo()], inv.Amount); ok {
					inv.Status, inv.PaidAt, inv.PaidTx, inv.PaidBy, inv.Height = InvoicePaid, time.UnixMilli(p.Time), p.ID, p.Tx.From, p.Height
					inv.HookPending, inv.HookTries, inv.HookNextAt = true, 0, now
				} else if now.After(inv.ExpiresAt) {
					inv.Status = InvoiceExpired
					inv.HookPending, inv.HookTries, inv.HookNextAt = true, 0, now
				}
			}
			return nil
		})
	}
	return firstErr
}

func (s *server) invoiceSummary(inv *Invoice) string {
	return fmt.Sprintf("%s %s ORP (%s)", inv.ID, types.FormatAmount(inv.Amount), inv.Status)
}

// firstCovering returns the earliest payment of at least amt. Partial
// payments are not summed: each checkout expects one payment.
func firstCovering(ps []historyEntry, amt types.Amount) (historyEntry, bool) {
	for _, p := range ps {
		if p.Tx.Amount >= amt {
			return p, true
		}
	}
	return historyEntry{}, false
}
