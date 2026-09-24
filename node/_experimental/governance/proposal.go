package governance

import (
	"time"
)

// ProposalStatus defines the lifecycle state of a network proposal
type ProposalStatus string

const (
	StatusActive   ProposalStatus = "ACTIVE"
	StatusPassed   ProposalStatus = "PASSED"
	StatusRejected ProposalStatus = "REJECTED"
)

// Proposal represents a request to change the OpenReserve network parameters
type Proposal struct {
	ID          string
	Proposer    string
	Title       string
	Description string
	Deposit     float64   // Anti-spam deposit in ORP
	ExpiryTime  time.Time // When voting closes
	Status      ProposalStatus
}

// NewProposal initializes a governance proposal. Requires a minimum deposit of 1000 ORP to prevent spam.
func NewProposal(id, proposer, title, description string, deposit float64) *Proposal {
	return &Proposal{
		ID:          id,
		Proposer:    proposer,
		Title:       title,
		Description: description,
		Deposit:     deposit,
		ExpiryTime:  time.Now().Add(7 * 24 * time.Hour), // 7 day voting period
		Status:      StatusActive,
	}
}

// IsExpired checks if the voting window has closed
func (p *Proposal) IsExpired() bool {
	return time.Now().After(p.ExpiryTime)
}
