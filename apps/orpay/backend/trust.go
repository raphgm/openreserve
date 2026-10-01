package main

import (
	"errors"
	"net/http"
	"slices"
	"sync"
	"time"

	"github.com/openreserve/node/types"
)

// Trust profile: a reputation anyone can check, computed only from what the
// ledger records (ajo rounds paid or missed, escrows completed, refunded or
// disputed) plus a few verifications ORPay holds (phone, partner business).
// Nobody can buy or fake it: every number links back to on-chain history.

type trustProfile struct {
	Address   types.Address `json:"address"`
	Username  string        `json:"username,omitempty"`
	Score     int           `json:"score"`           // 0-100
	Level     string        `json:"level"`           // new, building, trusted, excellent
	Since     int64         `json:"since,omitempty"` // first activity (unix ms)
	Ajo       ajoStats      `json:"ajo"`
	Escrow    escrowRep     `json:"escrow"`
	Badges    []string      `json:"badges"`
	UpdatedAt time.Time     `json:"updated_at"`
}

type ajoStats struct {
	Circles   int `json:"circles"`
	Finished  int `json:"finished"`
	OnTime    int `json:"rounds_paid"`
	Missed    int `json:"rounds_missed"`
	Organised int `json:"organised"`
}

type escrowRep struct {
	SoldCompleted   int `json:"sold_completed"`
	BoughtCompleted int `json:"bought_completed"`
	Refunded        int `json:"refunded_as_seller"`
	Disputes        int `json:"disputes"`
	Open            int `json:"open"`
}

type trustCache struct {
	mu sync.Mutex
	m  map[types.Address]*trustProfile
}

func (s *server) trustOf(a types.Address) (*trustProfile, error) {
	s.trust.mu.Lock()
	if p, ok := s.trust.m[a]; ok && s.now().Sub(p.UpdatedAt) < time.Minute {
		s.trust.mu.Unlock()
		return p, nil
	}
	s.trust.mu.Unlock()

	p := &trustProfile{Address: a, Badges: []string{}, UpdatedAt: s.now()}
	p.Username, _ = s.dir.byAddress(a)

	if hist, err := s.node.History(a); err == nil && len(hist) > 0 {
		p.Since = hist[len(hist)-1].Time
	}
	var pools []struct {
		Creator  types.Address   `json:"creator"`
		Members  []types.Address `json:"members"`
		Status   string          `json:"status"`
		Round    int             `json:"round"`
		Paid     []bool          `json:"paid"`
		Defaults []int           `json:"defaults"`
	}
	if err := s.node.GetJSON("/v1/accounts/"+string(a)+"/pools", &pools); err != nil {
		return nil, err
	}
	for _, pl := range pools {
		i := slices.Index(pl.Members, a)
		if i < 0 || pl.Status == "forming" {
			continue
		}
		p.Ajo.Circles++
		if pl.Creator == a {
			p.Ajo.Organised++
		}
		missed := 0
		if i < len(pl.Defaults) {
			missed = pl.Defaults[i]
		}
		rounds := pl.Round // completed rounds
		if pl.Status == "active" && pl.Paid[i] {
			rounds++
		}
		if pl.Status == "done" {
			p.Ajo.Finished++
		}
		p.Ajo.OnTime += max(0, rounds-missed)
		p.Ajo.Missed += missed
	}
	escrows, err := s.node.EscrowsOf(a)
	if err != nil {
		return nil, err
	}
	for _, e := range escrows {
		seller, buyer := e.Seller == a, e.Buyer == a
		switch e.Status {
		case "completed", "resolved":
			if seller {
				p.Escrow.SoldCompleted++
			}
			if buyer {
				p.Escrow.BoughtCompleted++
			}
		case "refunded":
			if seller {
				p.Escrow.Refunded++
			}
		case "funded", "dispatched":
			p.Escrow.Open++
		}
		if (seller || buyer) && (e.Status == "disputed" || e.Status == "resolved") {
			p.Escrow.Disputes++
		}
	}

	// Score: start at 20 for an active account, earn with good history,
	// lose with missed payments and disputes. Capped 0-100.
	score := 0
	if p.Since > 0 {
		score = 20
		if days := int(s.now().Sub(time.UnixMilli(p.Since)).Hours() / 24); days > 0 {
			score += min(15, days/6) // up to 15 for ~3 months
		}
	}
	score += min(30, p.Ajo.OnTime*3) + min(10, p.Ajo.Finished*5)
	score += min(30, (p.Escrow.SoldCompleted+p.Escrow.BoughtCompleted)*4)
	score -= p.Ajo.Missed*10 + p.Escrow.Disputes*8 + p.Escrow.Refunded*3
	p.Score = max(0, min(100, score))
	p.Level = map[bool]string{true: "new"}[p.Since == 0]
	switch {
	case p.Since == 0:
	case p.Score >= 80:
		p.Level = "excellent"
	case p.Score >= 55:
		p.Level = "trusted"
	default:
		p.Level = "building"
	}

	// Verifications ORPay holds.
	s.phones.Read(func(d *phoneData) {
		for _, owner := range d.Phones {
			if owner == a {
				p.Badges = append(p.Badges, "phone_verified")
				break
			}
		}
	})
	s.apps.Read(func(apps map[string]*App) {
		for _, app := range apps {
			if app.Owner == a && app.Status == AppApproved {
				p.Badges = append(p.Badges, "verified_business")
				break
			}
		}
	})
	if p.Ajo.Finished > 0 && p.Ajo.Missed == 0 {
		p.Badges = append(p.Badges, "ajo_perfect")
	}
	if p.Escrow.SoldCompleted >= 5 && p.Escrow.Refunded == 0 && p.Escrow.Disputes == 0 {
		p.Badges = append(p.Badges, "top_seller")
	}

	s.trust.mu.Lock()
	if s.trust.m == nil {
		s.trust.m = map[types.Address]*trustProfile{}
	}
	s.trust.m[a] = p
	s.trust.mu.Unlock()
	return p, nil
}

// getTrust serves a trust profile by @username or address.
func (s *server) getTrust(w http.ResponseWriter, r *http.Request) {
	ref := r.PathValue("who")
	addr, err := s.dir.resolveRef(ref)
	if err != nil {
		writeErr(w, http.StatusNotFound, errors.New("no ORPay user "+ref))
		return
	}
	p, err := s.trustOf(addr)
	if err != nil {
		writeErr(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, p)
}
