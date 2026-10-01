package main

import (
	"errors"
	"net/http"
	"slices"
	"time"

	"github.com/openreserve/node/reqauth"
	"github.com/openreserve/node/types"
)

// Partner dashboard: what an app's owner sees about its money flows in the
// Developers tab, without writing any code. Checkouts and escrows are
// summarised per currency, with the most recent of each.

type moneyTotals map[string]types.Amount // asset ("" = ORP) -> amount

func (m moneyTotals) add(asset string, a types.Amount) { m[asset] += a }

func (s *server) appDashboard(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	me := reqauth.Caller(r)
	var app *App
	s.apps.Read(func(apps map[string]*App) {
		if a, ok := apps[id]; ok && (a.Owner == me || slices.Contains(s.admins, me)) {
			c := *a
			app = &c
		}
	})
	if app == nil {
		writeErr(w, http.StatusNotFound, errors.New("app not found"))
		return
	}
	now := s.now()
	weekAgo := now.Add(-7 * 24 * time.Hour)

	type checkoutStats struct {
		Paid         int         `json:"paid"`
		Pending      int         `json:"pending"`
		Expired      int         `json:"expired"`
		PaidTotal    moneyTotals `json:"paid_total"`
		PaidThisWeek moneyTotals `json:"paid_this_week"`
		Recent       []*Invoice  `json:"recent"`
	}
	cs := checkoutStats{PaidTotal: moneyTotals{}, PaidThisWeek: moneyTotals{}}
	var invs []*Invoice
	s.invoices.Read(func(m map[string]*Invoice) {
		for _, inv := range m {
			if inv.AppID != id {
				continue
			}
			c := *inv
			invs = append(invs, &c)
		}
	})
	for _, inv := range invs {
		switch inv.Status {
		case InvoicePaid:
			cs.Paid++
			cs.PaidTotal.add(inv.Asset, inv.Amount)
			if inv.PaidAt.After(weekAgo) {
				cs.PaidThisWeek.add(inv.Asset, inv.Amount)
			}
		case InvoicePending:
			cs.Pending++
		default:
			cs.Expired++
		}
	}
	slices.SortFunc(invs, func(a, b *Invoice) int { return b.CreatedAt.Compare(a.CreatedAt) })
	cs.Recent = invs[:min(len(invs), 10)]

	type escrowStats struct {
		Awaiting  int              `json:"awaiting_funding"`
		Open      int              `json:"open"`
		Disputed  int              `json:"disputed"`
		Completed int              `json:"completed"`
		Refunded  int              `json:"refunded"`
		Other     int              `json:"other"`
		Held      moneyTotals      `json:"held"`
		Released  moneyTotals      `json:"released"`
		Recent    []map[string]any `json:"recent"`
	}
	es := escrowStats{Held: moneyTotals{}, Released: moneyTotals{}}
	var ers []*EscrowRequest
	s.escrows.Read(func(m map[string]*EscrowRequest) {
		for _, er := range m {
			if er.AppID == id {
				c := *er
				ers = append(ers, &c)
			}
		}
	})
	for _, er := range ers {
		if er.Status == EscrowAwaiting {
			es.Awaiting++
			continue
		}
		if er.Chain == nil {
			es.Other++
			continue
		}
		switch er.Chain.Status {
		case "funded", "dispatched":
			es.Open++
		case "disputed":
			es.Disputed++
		case "completed", "resolved":
			es.Completed++
		case "refunded":
			es.Refunded++
		}
		if isOpen(er.Chain.Status) {
			es.Held.add(er.Asset, er.Chain.Balance)
		}
		es.Released.add(er.Asset, er.Chain.PaidSeller)
	}
	slices.SortFunc(ers, func(a, b *EscrowRequest) int { return b.CreatedAt.Compare(a.CreatedAt) })
	for _, er := range ers[:min(len(ers), 10)] {
		v := s.escrowView(er, true)
		delete(v, "app")
		es.Recent = append(es.Recent, v)
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"app":       map[string]any{"id": app.ID, "name": app.Name, "status": app.Status, "webhook_url": app.WebhookURL},
		"checkouts": cs,
		"escrows":   es,
	})
}
