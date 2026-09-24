package consensus

import (
	"fmt"
	"time"
)

type State int

const (
	StatePropose State = iota
	StatePrevote
	StatePrecommit
	StateCommit
)

// Engine is the BFT state machine that drives the network to consensus
type Engine struct {
	CurrentRound uint64
	State        State
	Validators   *ValidatorSet

	Prevotes   *VoteSet
	Precommits *VoteSet
}

func NewEngine(validators *ValidatorSet) *Engine {
	return &Engine{
		CurrentRound: 0,
		State:        StatePropose,
		Validators:   validators,
	}
}

// StartRound begins a new block round
func (e *Engine) StartRound(round uint64) {
	e.CurrentRound = round
	e.State = StatePropose
	e.Prevotes = NewVoteSet(VoteTypePrevote, round, e.Validators.TotalStake)
	e.Precommits = NewVoteSet(VoteTypePrecommit, round, e.Validators.TotalStake)

	fmt.Printf("Started Round %d\\n", round)

	// Deterministically select the leader for this round
	leader := e.Validators.SelectLeader(int64(round))
	fmt.Printf("Leader selected for round %d: %s (Stake: %.2f ORP)\\n", round, leader.Address, leader.Stake)
}

// HandlePrevote processes an incoming prevote from a validator
func (e *Engine) HandlePrevote(vote *Vote) {
	if e.State != StatePropose && e.State != StatePrevote {
		return // Ignore prevotes if we are past this stage
	}

	val, exists := e.Validators.Validators[vote.ValidatorAddress]
	if !exists {
		return // Unknown validator
	}

	if !vote.Verify(val.PublicKey) {
		fmt.Println("Invalid prevote signature rejected")
		return
	}

	quorumReached := e.Prevotes.AddVote(vote, val.Stake)
	if quorumReached && e.State != StatePrecommit {
		fmt.Println("2/3+ Prevote Quorum Reached! Moving to Precommit Phase.")
		e.State = StatePrecommit

		// In a real implementation, the node would now broadcast its own Precommit vote
	}
}

// HandlePrecommit processes an incoming precommit from a validator
func (e *Engine) HandlePrecommit(vote *Vote) {
	if e.State != StatePrecommit {
		return
	}

	val, exists := e.Validators.Validators[vote.ValidatorAddress]
	if !exists {
		return
	}

	if !vote.Verify(val.PublicKey) {
		fmt.Println("Invalid precommit signature rejected")
		return
	}

	quorumReached := e.Precommits.AddVote(vote, val.Stake)
	if quorumReached && e.State != StateCommit {
		fmt.Println("2/3+ Precommit Quorum Reached! Block Finalized.")
		e.State = StateCommit

		// In a real implementation, the node would now append the block to the ledger
		// and immediately StartRound(e.CurrentRound + 1)
		time.Sleep(1 * time.Second)
		e.StartRound(e.CurrentRound + 1)
	}
}
