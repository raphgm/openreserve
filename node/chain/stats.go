package chain

import "github.com/openreserve/node/types"

// Stats is the network's public scoreboard: how much is held in escrow
// right now, what has been released or refunded, how disputes ended, and
// what ajo circles have paid out. Every number is computed from ledger
// state, so anyone can re-check it.
type Stats struct {
	Escrows struct {
		Total      int                     `json:"total"`
		Open       int                     `json:"open"`
		Completed  int                     `json:"completed"`
		Refunded   int                     `json:"refunded"`
		Disputed   int                     `json:"disputed"` // ever disputed (open disputes + resolved)
		Resolved   int                     `json:"resolved"`
		Held       map[string]types.Amount `json:"held"`       // by asset ("" = ORP)
		ToSellers  map[string]types.Amount `json:"to_sellers"` // released or awarded to sellers
		ToBuyers   map[string]types.Amount `json:"to_buyers"`  // refunded or awarded to buyers
		ArbiterFee map[string]types.Amount `json:"arbiter_fees"`
	} `json:"escrows"`
	Circles struct {
		Total     int                     `json:"total"`
		Active    int                     `json:"active"`
		Completed int                     `json:"completed"`
		Members   int                     `json:"members"`
		Payouts   int                     `json:"payouts"`    // pots collected
		PaidOut   map[string]types.Amount `json:"paid_out"`   // by asset
		Saving    map[string]types.Amount `json:"saving_now"` // held in circles now
		Missed    int                     `json:"missed_payments"`
		OnTime    int                     `json:"payments"`
	} `json:"circles"`
}

func (c *Chain) Stats() Stats {
	c.mu.RLock()
	defer c.mu.RUnlock()
	var st Stats
	e := &st.Escrows
	e.Held, e.ToSellers, e.ToBuyers, e.ArbiterFee = map[string]types.Amount{}, map[string]types.Amount{}, map[string]types.Amount{}, map[string]types.Amount{}
	for _, x := range c.state.Escrows {
		e.Total++
		switch x.Status {
		case "completed":
			e.Completed++
		case "refunded":
			e.Refunded++
		case "resolved":
			e.Resolved++
			e.Disputed++
		case "disputed":
			e.Disputed++
		}
		if x.Open() {
			e.Open++
			e.Held[x.Asset] += x.Balance
		}
		e.ToSellers[x.Asset] += x.PaidSeller
		e.ToBuyers[x.Asset] += x.PaidBuyer
		if x.ArbiterFee > 0 {
			e.ArbiterFee[x.Asset] += x.ArbiterFee
		}
	}
	p := &st.Circles
	p.PaidOut, p.Saving = map[string]types.Amount{}, map[string]types.Amount{}
	for _, x := range c.state.Pools {
		if x.Status == "cancelled" {
			continue
		}
		p.Total++
		p.Members += len(x.Members)
		switch x.Status {
		case "active":
			p.Active++
		case "done":
			p.Completed++
		}
		p.Saving[x.Asset] += x.Balance + x.Held()
		if x.Mode == types.PoolGoal {
			continue
		}
		p.Payouts += x.Round
		for j, paid := range x.PaidOut {
			if x.Claimed[j] {
				p.PaidOut[x.Asset] += paid
			}
		}
		if x.PaidOut == nil { // unscheduled circles: every pot was full
			p.PaidOut[x.Asset] += x.Pot() * types.Amount(x.Round)
		}
		n := len(x.Members)
		paidRounds := x.Round * n
		for _, d := range x.Defaults {
			p.Missed += d
			paidRounds -= d
		}
		p.OnTime += max(0, paidRounds)
	}
	return st
}
