package governance

import (
	"fmt"
	"sync"
)

// Engine manages the DAO
type Engine struct {
	mu          sync.RWMutex
	Proposals   map[string]*Proposal
	Votes       map[string]map[string]*Vote // ProposalID -> VoterAddress -> Vote
	TotalSupply float64                     // Snapshot of total ORP supply to calculate Quorum
}

func NewEngine(totalSupply float64) *Engine {
	return &Engine{
		Proposals:   make(map[string]*Proposal),
		Votes:       make(map[string]map[string]*Vote),
		TotalSupply: totalSupply,
	}
}

// SubmitProposal adds a new proposal to the DAO
func (e *Engine) SubmitProposal(p *Proposal) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if p.Deposit < 1000.0 {
		return fmt.Errorf("anti-spam rejection: minimum deposit is 1000 ORP")
	}

	e.Proposals[p.ID] = p
	e.Votes[p.ID] = make(map[string]*Vote)
	return nil
}

// CastVote records a stake-weighted vote
func (e *Engine) CastVote(v *Vote) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	p, exists := e.Proposals[v.ProposalID]
	if !exists {
		return fmt.Errorf("proposal not found")
	}

	if p.Status != StatusActive {
		return fmt.Errorf("voting is closed for this proposal")
	}

	if p.IsExpired() {
		return fmt.Errorf("voting period has expired")
	}

	// 1 ORP = 1 Vote. This power is drawn from the Phase 7 Ledger State externally.
	e.Votes[v.ProposalID][v.VoterAddress] = v
	return nil
}

// Tally evaluates the proposal against the Quorum and Threshold logic
func (e *Engine) Tally(proposalID string) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	p, exists := e.Proposals[proposalID]
	if !exists {
		return fmt.Errorf("proposal not found")
	}

	if p.Status != StatusActive {
		return fmt.Errorf("proposal already finalized")
	}

	var yes, no, abstain float64
	for _, v := range e.Votes[proposalID] {
		switch v.Choice {
		case VoteYes:
			yes += v.VotingPower
		case VoteNo:
			no += v.VotingPower
		case VoteAbstain:
			abstain += v.VotingPower
		}
	}

	totalParticipating := yes + no + abstain

	// 1. Check Quorum (33%)
	quorumRequired := e.TotalSupply * 0.33
	if totalParticipating < quorumRequired {
		p.Status = StatusRejected
		return fmt.Errorf("proposal rejected: did not reach 33%% quorum")
	}

	// 2. Check Pass Threshold (>50% of yes/no)
	totalActiveVotes := yes + no
	if totalActiveVotes == 0 {
		p.Status = StatusRejected
		return nil
	}

	if yes > (totalActiveVotes * 0.50) {
		p.Status = StatusPassed
	} else {
		p.Status = StatusRejected
	}

	return nil
}
