package governance

// Choice represents how a user voted
type Choice string

const (
	VoteYes     Choice = "YES"
	VoteNo      Choice = "NO"
	VoteAbstain Choice = "ABSTAIN"
)

// Vote represents a cryptographically verifiable decision by a stakeholder
type Vote struct {
	VoterAddress string
	ProposalID   string
	Choice       Choice
	VotingPower  float64 // 1 ORP = 1 Vote
	Signature    []byte  // Hex encoded Ed25519 signature
}

// NewVote creates a voting record
func NewVote(address, proposalID string, choice Choice, votingPower float64, sig []byte) *Vote {
	return &Vote{
		VoterAddress: address,
		ProposalID:   proposalID,
		Choice:       choice,
		VotingPower:  votingPower,
		Signature:    sig,
	}
}
