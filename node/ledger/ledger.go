// Package ledger holds account state and the rules for applying transactions.
package ledger

import (
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"sort"

	"github.com/openreserve/node/types"
)

// Account is the state of a single address.
type Account struct {
	Balance types.Amount `json:"balance"`
	Nonce   uint64       `json:"nonce"`
}

// State maps addresses to accounts and tracks total supply. It is not safe
// for concurrent use; the chain package serializes access.
type State struct {
	Accounts map[types.Address]Account
	Supply   types.Amount // circulating supply; fees are burned from it
	Burned   types.Amount
	MinFee   types.Amount
}

// Errors returned by Apply. ErrNonceTooHigh lets the mempool keep a tx that
// may become valid once earlier txs from the same sender land.
var (
	ErrNonceTooLow  = errors.New("nonce too low")
	ErrNonceTooHigh = errors.New("nonce too high")
	ErrInsufficient = errors.New("insufficient funds")
	ErrFeeTooLow    = errors.New("fee below minimum")
	ErrOverflow     = errors.New("balance overflow")
)

// New creates an empty state.
func New(minFee types.Amount) *State {
	return &State{Accounts: map[types.Address]Account{}, MinFee: minFee}
}

// Get returns an account; unknown addresses have zero balance and nonce.
func (s *State) Get(a types.Address) Account { return s.Accounts[a] }

// Credit adds funds outside of a transaction (genesis allocation only).
func (s *State) Credit(a types.Address, amt types.Amount) error {
	acc := s.Accounts[a]
	if acc.Balance+amt < acc.Balance || s.Supply+amt < s.Supply {
		return ErrOverflow
	}
	acc.Balance += amt
	s.Supply += amt
	s.Accounts[a] = acc
	return nil
}

// Check validates a tx against current state without modifying it. The tx
// must already have passed CheckStateless.
func (s *State) Check(tx *types.Tx) error {
	if tx.Fee < s.MinFee {
		return fmt.Errorf("%w: need %s ORP", ErrFeeTooLow, types.FormatAmount(s.MinFee))
	}
	from := s.Accounts[tx.From]
	switch {
	case tx.Nonce < from.Nonce:
		return fmt.Errorf("%w: account nonce is %d", ErrNonceTooLow, from.Nonce)
	case tx.Nonce > from.Nonce:
		return fmt.Errorf("%w: account nonce is %d", ErrNonceTooHigh, from.Nonce)
	}
	if from.Balance < tx.Amount+tx.Fee {
		return fmt.Errorf("%w: balance %s ORP", ErrInsufficient, types.FormatAmount(from.Balance))
	}
	if to := s.Accounts[tx.To]; to.Balance+tx.Amount < to.Balance {
		return ErrOverflow
	}
	return nil
}

// Apply validates and executes a tx.
func (s *State) Apply(tx *types.Tx) error {
	if err := s.Check(tx); err != nil {
		return err
	}
	from := s.Accounts[tx.From]
	from.Balance -= tx.Amount + tx.Fee
	from.Nonce++
	s.Accounts[tx.From] = from

	to := s.Accounts[tx.To]
	to.Balance += tx.Amount
	s.Accounts[tx.To] = to

	s.Supply -= tx.Fee
	s.Burned += tx.Fee
	return nil
}

// Clone returns an independent copy, used to build and verify blocks
// without touching the committed state.
func (s *State) Clone() *State {
	c := *s
	c.Accounts = make(map[types.Address]Account, len(s.Accounts))
	for k, v := range s.Accounts {
		c.Accounts[k] = v
	}
	return &c
}

// Root commits to the full state: every account plus supply counters.
// It is a flat hash over sorted accounts, which is fine at this scale; a
// Merkle tree would be needed for light-client proofs.
func (s *State) Root() types.Hash {
	addrs := make([]string, 0, len(s.Accounts))
	for a := range s.Accounts {
		addrs = append(addrs, string(a))
	}
	sort.Strings(addrs)
	h := sha256.New()
	var buf [8]byte
	u64 := func(v uint64) { binary.BigEndian.PutUint64(buf[:], v); h.Write(buf[:]) }
	for _, a := range addrs {
		acc := s.Accounts[types.Address(a)]
		h.Write([]byte(a))
		u64(acc.Balance)
		u64(acc.Nonce)
	}
	u64(s.Supply)
	u64(s.Burned)
	var out types.Hash
	copy(out[:], h.Sum(nil))
	return out
}
