// Package types defines the wire and on-disk data structures of the
// OpenReserve ledger: addresses, amounts, transactions and blocks.
package types

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// Amount is a quantity of ORP in micro-units (1 ORP = 1_000_000 micro-ORP).
// Integer units avoid the rounding errors of floating point money.
type Amount = uint64

// Unit is the number of micro-units in one ORP.
const Unit Amount = 1_000_000

// MaxMemoLen bounds the size of the free-form transaction memo.
const MaxMemoLen = 256

// Address is the hex encoding of an account's Ed25519 public key. Because the
// address is the public key, any signature can be verified from the address alone.
type Address string

// AddressFromPubKey derives the address for a public key.
func AddressFromPubKey(pub ed25519.PublicKey) Address {
	return Address(hex.EncodeToString(pub))
}

// PubKey decodes the address back into an Ed25519 public key.
func (a Address) PubKey() (ed25519.PublicKey, error) {
	b, err := hex.DecodeString(string(a))
	if err != nil || len(b) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("invalid address %q", a)
	}
	return ed25519.PublicKey(b), nil
}

// Validate reports whether the address is a well-formed public key.
func (a Address) Validate() error {
	if strings.ToLower(string(a)) != string(a) {
		return fmt.Errorf("address must be lowercase hex")
	}
	_, err := a.PubKey()
	return err
}

// Hash is a SHA-256 digest, hex encoded in JSON.
type Hash [32]byte

func (h Hash) String() string { return hex.EncodeToString(h[:]) }

func (h Hash) MarshalText() ([]byte, error) { return []byte(h.String()), nil }

func (h *Hash) UnmarshalText(b []byte) error {
	d, err := hex.DecodeString(string(b))
	if err != nil || len(d) != 32 {
		return fmt.Errorf("invalid hash %q", b)
	}
	copy(h[:], d)
	return nil
}

// ParseHash decodes a hex hash.
func ParseHash(s string) (Hash, error) {
	var h Hash
	err := h.UnmarshalText([]byte(s))
	return h, err
}

// Tx is a transfer of Amount from From to To, a mint or burn of an issued
// asset (Kind), or, when Pool is set, a savings-pool operation. Asset is ""
// for the native ORP or the symbol of an issued asset such as "NGN". Fees
// are paid in the tx's asset: ORP fees are burned, issued-asset fees go to
// the asset's issuer. Nonce must equal the sender's current nonce, which
// prevents replay and orders a sender's txs.
type Tx struct {
	ChainID string   `json:"chain_id"`
	From    Address  `json:"from"`
	To      Address  `json:"to,omitempty"`
	Amount  Amount   `json:"amount"`
	Fee     Amount   `json:"fee"`
	Nonce   uint64   `json:"nonce"`
	Memo    string   `json:"memo,omitempty"`
	Kind    string   `json:"kind,omitempty"`  // "", KindMint or KindBurn
	Asset   string   `json:"asset,omitempty"` // "" = ORP
	Pool    *PoolOp  `json:"pool,omitempty"`
	Sig     HexBytes `json:"sig"`
}

// Tx kinds for issued assets. A mint creates units of an asset and may only
// be sent by that asset's issuer (e.g. the Paystack gateway, after naira is
// received). A burn destroys the sender's own units (e.g. a withdrawal to a
// bank account, paid out by the issuer).
const (
	KindMint = "mint"
	KindBurn = "burn"
)

var assetRe = regexp.MustCompile(`^[A-Z]{2,10}$`)

// ValidAsset reports whether s is "" (ORP) or a well-formed asset symbol.
func ValidAsset(s string) bool { return s == "" || (s != "ORP" && assetRe.MatchString(s)) }

// AssetName is the display symbol for an asset field.
func AssetName(s string) string {
	if s == "" {
		return "ORP"
	}
	return s
}

// Pool operations. A savings pool (ajo/esusu) has a fixed member order and
// contribution. Each round every member contributes; once all have, the
// round's member claims the pot, and the next round begins.
const (
	PoolCreate     = "create"
	PoolJoin       = "join"
	PoolContribute = "contribute"
	PoolClaim      = "claim"

	MaxPoolMembers = 50
	MaxPoolName    = 64
)

// PoolOp is the pool-specific part of a Tx.
type PoolOp struct {
	Op string `json:"op"`
	// ID names the pool for join/contribute/claim. A pool's ID is the ID of
	// the tx that created it.
	ID           Hash      `json:"id,omitzero"`
	Name         string    `json:"name,omitempty"`
	Members      []Address `json:"members,omitempty"` // payout order
	Contribution Amount    `json:"contribution,omitempty"`
}

// HexBytes is a byte slice hex encoded in JSON.
type HexBytes []byte

func (b HexBytes) MarshalText() ([]byte, error) { return []byte(hex.EncodeToString(b)), nil }

func (b *HexBytes) UnmarshalText(t []byte) error {
	d, err := hex.DecodeString(string(t))
	if err != nil {
		return err
	}
	*b = d
	return nil
}

// encoder builds a deterministic, length-prefixed binary encoding.
type encoder struct{ buf []byte }

func (e *encoder) str(s string) {
	e.buf = binary.BigEndian.AppendUint32(e.buf, uint32(len(s)))
	e.buf = append(e.buf, s...)
}
func (e *encoder) u64(v uint64) { e.buf = binary.BigEndian.AppendUint64(e.buf, v) }
func (e *encoder) raw(b []byte) { e.buf = append(e.buf, b...) }

// SignBytes is the canonical message a sender signs. It is domain separated
// so a transaction signature can never be reused as a block signature.
// Plain ORP transfers keep the v1 encoding; everything else uses v2.
func (tx *Tx) SignBytes() []byte {
	e := &encoder{}
	v2 := tx.Pool != nil || tx.Kind != "" || tx.Asset != ""
	if v2 {
		e.str("openreserve/tx/v2")
	} else {
		e.str("openreserve/tx/v1")
	}
	e.str(tx.ChainID)
	e.str(string(tx.From))
	e.str(string(tx.To))
	e.u64(tx.Amount)
	e.u64(tx.Fee)
	e.u64(tx.Nonce)
	e.str(tx.Memo)
	if !v2 {
		return e.buf
	}
	e.str(tx.Kind)
	e.str(tx.Asset)
	if p := tx.Pool; p != nil {
		e.raw([]byte{1})
		e.str(p.Op)
		e.raw(p.ID[:])
		e.str(p.Name)
		e.u64(uint64(len(p.Members)))
		for _, m := range p.Members {
			e.str(string(m))
		}
		e.u64(p.Contribution)
	} else {
		e.raw([]byte{0})
	}
	return e.buf
}

// ID uniquely identifies a transaction. It covers the signed content only,
// so it is known before the transaction is submitted.
func (tx *Tx) ID() Hash { return sha256.Sum256(tx.SignBytes()) }

// Sign fills in the signature. The key must belong to tx.From.
func (tx *Tx) Sign(priv ed25519.PrivateKey) {
	tx.Sig = ed25519.Sign(priv, tx.SignBytes())
}

// CheckStateless validates everything that does not depend on ledger state.
func (tx *Tx) CheckStateless(chainID string) error {
	if tx.ChainID != chainID {
		return fmt.Errorf("wrong chain_id %q", tx.ChainID)
	}
	if err := tx.From.Validate(); err != nil {
		return fmt.Errorf("from: %w", err)
	}
	if len(tx.Memo) > MaxMemoLen {
		return fmt.Errorf("memo longer than %d bytes", MaxMemoLen)
	}
	if !ValidAsset(tx.Asset) {
		return fmt.Errorf("invalid asset %q", tx.Asset)
	}
	var err error
	switch {
	case tx.Pool != nil:
		if tx.Kind != "" {
			return errors.New("pool operations cannot have a kind")
		}
		err = tx.checkPoolOp()
	case tx.Kind == "":
		err = tx.checkTransfer()
	case tx.Kind == KindMint:
		err = tx.checkMint()
	case tx.Kind == KindBurn:
		err = tx.checkBurn()
	default:
		err = fmt.Errorf("unknown kind %q", tx.Kind)
	}
	if err != nil {
		return err
	}
	pub, _ := tx.From.PubKey()
	if len(tx.Sig) != ed25519.SignatureSize || !ed25519.Verify(pub, tx.SignBytes(), tx.Sig) {
		return errors.New("invalid signature")
	}
	return nil
}

func (tx *Tx) checkTransfer() error {
	if err := tx.To.Validate(); err != nil {
		return fmt.Errorf("to: %w", err)
	}
	if tx.From == tx.To {
		return errors.New("from and to must differ")
	}
	if tx.Amount == 0 {
		return errors.New("amount must be positive")
	}
	if tx.Amount+tx.Fee < tx.Amount {
		return errors.New("amount + fee overflows")
	}
	return nil
}

func (tx *Tx) checkMint() error {
	if tx.Asset == "" {
		return errors.New("ORP cannot be minted")
	}
	if err := tx.To.Validate(); err != nil {
		return fmt.Errorf("to: %w", err)
	}
	if tx.Amount == 0 {
		return errors.New("amount must be positive")
	}
	if tx.Fee != 0 {
		return errors.New("mints carry no fee")
	}
	return nil
}

func (tx *Tx) checkBurn() error {
	if tx.Asset == "" {
		return errors.New("ORP cannot be burned this way")
	}
	if tx.To != "" {
		return errors.New("burn must not set to")
	}
	if tx.Amount == 0 {
		return errors.New("amount must be positive")
	}
	if tx.Amount+tx.Fee < tx.Amount {
		return errors.New("amount + fee overflows")
	}
	return nil
}

func (tx *Tx) checkPoolOp() error {
	p := tx.Pool
	if tx.To != "" || tx.Amount != 0 {
		return errors.New("pool operations must not set to or amount")
	}
	switch p.Op {
	case PoolCreate:
		if p.ID != (Hash{}) {
			return errors.New("create must not set id")
		}
		if n := len([]rune(p.Name)); n == 0 || len(p.Name) > MaxPoolName {
			return fmt.Errorf("pool name must be 1-%d bytes", MaxPoolName)
		}
		if len(p.Members) < 2 || len(p.Members) > MaxPoolMembers {
			return fmt.Errorf("a pool needs 2-%d members", MaxPoolMembers)
		}
		seen := map[Address]bool{}
		for _, m := range p.Members {
			if err := m.Validate(); err != nil {
				return fmt.Errorf("member: %w", err)
			}
			if seen[m] {
				return fmt.Errorf("member %s listed twice", m)
			}
			seen[m] = true
		}
		if !seen[tx.From] {
			return errors.New("the creator must be a member")
		}
		if p.Contribution == 0 {
			return errors.New("contribution must be positive")
		}
		if p.Contribution > ^uint64(0)/uint64(len(p.Members)) {
			return errors.New("contribution too large")
		}
	case PoolJoin, PoolContribute, PoolClaim:
		if p.ID == (Hash{}) {
			return errors.New("pool id required")
		}
		if p.Name != "" || len(p.Members) != 0 || p.Contribution != 0 {
			return fmt.Errorf("%s takes only a pool id", p.Op)
		}
	default:
		return fmt.Errorf("unknown pool op %q", p.Op)
	}
	return nil
}

// Header is the signed part of a block.
type Header struct {
	ChainID   string  `json:"chain_id"`
	Height    uint64  `json:"height"`
	PrevHash  Hash    `json:"prev_hash"`
	Time      int64   `json:"time"` // unix milliseconds
	TxRoot    Hash    `json:"tx_root"`
	StateRoot Hash    `json:"state_root"` // state after applying this block
	Proposer  Address `json:"proposer"`
}

func (h *Header) signBytes() []byte {
	e := &encoder{}
	e.str("openreserve/block/v1")
	e.str(h.ChainID)
	e.u64(h.Height)
	e.raw(h.PrevHash[:])
	e.u64(uint64(h.Time))
	e.raw(h.TxRoot[:])
	e.raw(h.StateRoot[:])
	e.str(string(h.Proposer))
	return e.buf
}

// Hash identifies the block.
func (h *Header) Hash() Hash { return sha256.Sum256(h.signBytes()) }

// Block is an ordered batch of transactions signed by the proposer.
type Block struct {
	Header Header   `json:"header"`
	Txs    []*Tx    `json:"txs"`
	Sig    HexBytes `json:"sig"`
}

// Sign signs the header with the proposer key.
func (b *Block) Sign(priv ed25519.PrivateKey) {
	b.Sig = ed25519.Sign(priv, b.Header.signBytes())
}

// VerifySig checks the proposer signature.
func (b *Block) VerifySig() error {
	pub, err := b.Header.Proposer.PubKey()
	if err != nil {
		return err
	}
	if !ed25519.Verify(pub, b.Header.signBytes(), b.Sig) {
		return errors.New("invalid block signature")
	}
	return nil
}

// TxRoot is a binary Merkle root over transaction IDs. Leaves and inner nodes
// are prefixed differently to rule out second-preimage attacks.
func TxRoot(txs []*Tx) Hash {
	if len(txs) == 0 {
		return Hash{}
	}
	level := make([]Hash, len(txs))
	for i, tx := range txs {
		id := tx.ID()
		level[i] = sha256.Sum256(append([]byte{0}, id[:]...))
	}
	for len(level) > 1 {
		var next []Hash
		for i := 0; i < len(level); i += 2 {
			if i+1 == len(level) {
				next = append(next, level[i]) // odd node is promoted
				continue
			}
			buf := append([]byte{1}, level[i][:]...)
			next = append(next, sha256.Sum256(append(buf, level[i+1][:]...)))
		}
		level = next
	}
	return level[0]
}

// FormatAmount renders micro-units as a decimal ORP string.
func FormatAmount(a Amount) string {
	s := fmt.Sprintf("%d.%06d", a/Unit, a%Unit)
	s = strings.TrimRight(s, "0")
	return strings.TrimSuffix(s, ".")
}

// ParseAmount parses a decimal ORP string ("12.5") into micro-units.
func ParseAmount(s string) (Amount, error) {
	bad := fmt.Errorf("invalid amount %q (digits, at most 6 decimals)", s)
	whole, frac, dot := strings.Cut(s, ".")
	if !digits(whole) || len(frac) > 6 || (dot && !digits(frac)) {
		return 0, bad
	}
	w, err := strconv.ParseUint(whole, 10, 64)
	if err != nil {
		return 0, bad
	}
	f, _ := strconv.ParseUint(frac+strings.Repeat("0", 6-len(frac)), 10, 64)
	if w > (^uint64(0)-f)/Unit {
		return 0, fmt.Errorf("amount %q too large", s)
	}
	return w*Unit + f, nil
}

func digits(s string) bool {
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return s != ""
}
