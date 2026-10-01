package main

import (
	"errors"
	"fmt"
	"net/http"
	"slices"
	"sync"
	"time"

	"github.com/openreserve/node/reqauth"
	"github.com/openreserve/node/types"
)

// Organiser nudge: remind everyone who hasn't paid this round, by push and,
// for members with a verified phone, by SMS. At most once every 6 hours per
// pool so nobody gets spammed.

var nudged = struct {
	sync.Mutex
	at map[string]time.Time
}{at: map[string]time.Time{}}

func (s *server) nudgePool(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var resp struct {
		Pool struct {
			Name         string          `json:"name"`
			Creator      types.Address   `json:"creator"`
			Members      []types.Address `json:"members"`
			Paid         []bool          `json:"paid"`
			Status       string          `json:"status"`
			Asset        string          `json:"asset"`
			Contribution types.Amount    `json:"contribution"`
		} `json:"pool"`
	}
	if err := s.node.GetJSON("/v1/pools/"+id, &resp); err != nil {
		writeErr(w, http.StatusNotFound, errors.New("pool not found"))
		return
	}
	p := resp.Pool
	if p.Creator != reqauth.Caller(r) {
		writeErr(w, http.StatusForbidden, errors.New("only the organiser can send reminders"))
		return
	}
	if p.Status != "active" {
		writeErr(w, http.StatusConflict, errors.New("reminders are for active circles"))
		return
	}
	nudged.Lock()
	if last, ok := nudged.at[id]; ok && s.now().Sub(last) < 6*time.Hour {
		nudged.Unlock()
		writeErr(w, http.StatusTooManyRequests, fmt.Errorf("reminders were sent at %s; you can send again after %s", last.Format("15:04"), last.Add(6*time.Hour).Format("15:04")))
		return
	}
	nudged.at[id] = s.now()
	nudged.Unlock()

	var unpaid []types.Address
	for i, m := range p.Members {
		if !p.Paid[i] {
			unpaid = append(unpaid, m)
		}
	}
	org := s.nameOrShort(p.Creator)
	text := fmt.Sprintf("%s reminds you: your %s payment for %s is due. Open ORPay to pay.", org, money(p.Contribution, p.Asset), p.Name)
	phones := map[types.Address]string{}
	s.phones.Read(func(d *phoneData) {
		for ph, a := range d.Phones {
			if slices.Contains(unpaid, a) {
				phones[a] = ph
			}
		}
	})
	sms := 0
	for _, a := range unpaid {
		go s.notify(a, notification{Title: p.Name, Body: text, URL: "/?pool=" + id, Tag: "ajo-" + id})
		if ph, ok := phones[a]; ok && s.sms.Send(ph, text) == nil {
			sms++
		}
	}
	writeJSON(w, http.StatusOK, map[string]int{"reminded": len(unpaid), "sms": sms})
}
