package ledger

import (
	"crypto/sha256"
	"encoding/binary"

	"github.com/openreserve/node/types"
)

// Saving-circle rules shared by every circle mode: instalments, the
// payout (with insurance and bidding discounts), lottery order and turn
// swaps. A plain rotation pool that uses none of these behaves exactly as
// before and hashes the same.

func (p *Pool) hasCircleState() bool {
	return p.Mode != "" || p.InsuranceBps != 0 || p.Target != 0 || p.PaidAmt != nil || p.Bids != nil || p.Saved != nil || p.SwapWith != nil
}

// paidSoFar is what member j has paid toward this round.
func (p *Pool) paidSoFar(j int) types.Amount {
	if p.PaidAmt != nil {
		return p.PaidAmt[j]
	}
	if p.Paid[j] {
		return p.Contribution
	}
	return 0
}

// payIn records amt from member j toward this round.
func (p *Pool) payIn(j int, amt types.Amount) {
	if p.PaidAmt == nil && amt < p.Contribution {
		p.PaidAmt = make([]types.Amount, len(p.Members))
		for k, paid := range p.Paid {
			if paid {
				p.PaidAmt[k] = p.Contribution
			}
		}
	}
	if p.PaidAmt != nil {
		p.PaidAmt[j] += amt
		p.Paid[j] = p.PaidAmt[j] == p.Contribution
	} else {
		p.Paid[j] = true
	}
	p.Balance += amt
}

// Recipient is who collects this round: the fixed turn, or in a bidding
// circle the member offering the biggest discount (ties: earliest in the
// list; no bids: the next member who has not collected).
func (p *Pool) Recipient() int {
	if p.Mode != types.PoolBidding {
		return p.Round
	}
	best, first := -1, -1
	for j := range p.Members {
		if p.Claimed[j] {
			continue
		}
		if first < 0 {
			first = j
		}
		if p.Bids[j] > 0 && (best < 0 || p.Bids[j] > p.Bids[best]) {
			best = j
		}
	}
	if best >= 0 {
		return best
	}
	return first
}

// payout pays member i this round's pot and moves to the next round.
func (p *Pool) payout(s *State, i int) {
	// Past the due date: record missed payments, covered from the member's
	// deposit, then from the circle's insurance.
	for j := range p.Members {
		if p.Paid[j] {
			continue
		}
		missing := p.Contribution - p.paidSoFar(j)
		if p.Defaults != nil {
			p.Defaults[j]++
		}
		switch {
		case p.Deposits != nil && p.Deposits[j] >= missing:
			p.Deposits[j] -= missing
			p.Balance += missing
		case p.Insurance >= missing:
			p.Insurance -= missing
			p.Balance += missing
		}
	}
	pot := p.Balance
	p.Balance = 0
	if p.InsuranceBps > 0 {
		cut := types.Amount(uint64(pot)/10_000*uint64(p.InsuranceBps)) + types.Amount(uint64(pot)%10_000*uint64(p.InsuranceBps)/10_000)
		p.Insurance += cut
		pot -= cut
	}
	if p.Mode == types.PoolBidding && p.Bids[i] > 0 {
		// The winner's discount is shared equally by everyone else.
		d := min(p.Bids[i], pot)
		share := d / types.Amount(len(p.Members)-1)
		for j, m := range p.Members {
			if j != i {
				s.credit(m, p.Asset, share)
				pot -= share
			}
		}
	}
	s.credit(p.Members[i], p.Asset, pot)
	if p.PaidOut != nil {
		p.PaidOut[i] = pot
	}
	p.Claimed[i] = true
	p.Round++
	clear(p.Paid)
	clear(p.PaidAmt)
	clear(p.Bids)
	if p.Round < len(p.Members) {
		p.autopay()
		return
	}
	p.Status = PoolDone
	// Return what is left: autopay money, deposits, and insurance shared equally.
	for j, m := range p.Members {
		if p.Prepaid != nil && p.Prepaid[j] > 0 {
			s.credit(m, p.Asset, p.Prepaid[j])
			p.Prepaid[j] = 0
		}
		if p.Deposits != nil && p.Deposits[j] > 0 {
			s.credit(m, p.Asset, p.Deposits[j])
			p.Deposits[j] = 0
		}
	}
	if p.Insurance > 0 {
		n := types.Amount(len(p.Members))
		share := p.Insurance / n
		for j, m := range p.Members {
			v := share
			if j == len(p.Members)-1 {
				v = p.Insurance - share*(n-1)
			}
			s.credit(m, p.Asset, v)
		}
		p.Insurance = 0
	}
}

// swapIdx exchanges members i and j with everything tracked per member.
func (p *Pool) swapIdx(i, j int) {
	sw := func(xs any) {
		switch v := xs.(type) {
		case []bool:
			if v != nil {
				v[i], v[j] = v[j], v[i]
			}
		case []int:
			if v != nil {
				v[i], v[j] = v[j], v[i]
			}
		case []types.Amount:
			if v != nil {
				v[i], v[j] = v[j], v[i]
			}
		case []types.Address:
			if v != nil {
				v[i], v[j] = v[j], v[i]
			}
		}
	}
	for _, xs := range []any{p.Members, p.Joined, p.Paid, p.Claimed, p.Deposits, p.Defaults, p.PaidOut, p.Prepaid, p.PaidAmt, p.Bids, p.Saved, p.Withdrawn, p.SwapWith} {
		sw(xs)
	}
}

// drawOrder shuffles the payout order when a lottery circle starts. The
// draw is derived from the pool id and start time, so every validator
// gets the same order and anyone can re-check it.
func (p *Pool) drawOrder() {
	seed := sha256.Sum256(append(p.ID[:], binary.BigEndian.AppendUint64(nil, uint64(p.StartedAt))...))
	for k := len(p.Members) - 1; k > 0; k-- {
		seed = sha256.Sum256(seed[:])
		r := int(binary.BigEndian.Uint64(seed[:8]) % uint64(k+1))
		p.swapIdx(k, r)
	}
}
