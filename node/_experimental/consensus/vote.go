package consensus

import (
	"crypto/ed25519"
	"fmt"
)

type VoteType int

const (
	VoteTypePrevote VoteType = iota
	VoteTypePrecommit
)

// Vote represents a validator's cryptographically signed endorsement of a block hash
type Vote struct {
	ValidatorAddress string
	BlockHash        []byte
	Type             VoteType
	Round            uint64
	Signature        []byte
}

// Sign applies the validator's Ed25519 signature to the vote
func (v *Vote) Sign(priv ed25519.PrivateKey) {
	payload := fmt.Sprintf("%s:%x:%d:%d", v.ValidatorAddress, v.BlockHash, v.Type, v.Round)
	v.Signature = ed25519.Sign(priv, []byte(payload))
}

// Verify ensures the vote signature is valid
func (v *Vote) Verify(pub ed25519.PublicKey) bool {
	payload := fmt.Sprintf("%s:%x:%d:%d", v.ValidatorAddress, v.BlockHash, v.Type, v.Round)
	return ed25519.Verify(pub, []byte(payload), v.Signature)
}

// VoteSet tracks the votes collected for a specific round and vote type
type VoteSet struct {
	Type          VoteType
	Round         uint64
	Votes         map[string]*Vote
	TotalStake    float64
	RequiredStake float64
}

func NewVoteSet(voteType VoteType, round uint64, totalNetworkStake float64) *VoteSet {
	return &VoteSet{
		Type:          voteType,
		Round:         round,
		Votes:         make(map[string]*Vote),
		RequiredStake: (totalNetworkStake * 2) / 3, // Requires strictly greater than 2/3
	}
}

// AddVote records a vote if it is valid, returning true if the 2/3+ quorum is reached
func (vs *VoteSet) AddVote(vote *Vote, validatorStake float64) bool {
	if _, exists := vs.Votes[vote.ValidatorAddress]; exists {
		return false // Already voted
	}
	vs.Votes[vote.ValidatorAddress] = vote
	vs.TotalStake += validatorStake

	return vs.TotalStake > vs.RequiredStake
}
