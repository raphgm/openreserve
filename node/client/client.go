// Package client is a Go client for an OpenReserve node's HTTP API.
package client

import (
	"bytes"
	"crypto/ed25519"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/openreserve/node/types"
)

type Client struct {
	Base string
	HTTP *http.Client

	mu sync.Mutex // serializes Sign-and-submit so one key's nonces never collide
}

func New(base string) *Client {
	return &Client{Base: strings.TrimRight(base, "/"), HTTP: &http.Client{Timeout: 10 * time.Second}}
}

type Status struct {
	ChainID string       `json:"chain_id"`
	Height  uint64       `json:"height"`
	MinFee  types.Amount `json:"min_fee"`
	Supply  types.Amount `json:"supply"`
	Assets  []struct {
		Symbol string        `json:"symbol"`
		Issuer types.Address `json:"issuer"`
		MinFee types.Amount  `json:"min_fee"`
		Supply types.Amount  `json:"supply"`
	} `json:"assets"`
}

type Account struct {
	Balance   types.Amount            `json:"balance"`
	Nonce     uint64                  `json:"nonce"`
	NextNonce uint64                  `json:"next_nonce"`
	Assets    map[string]types.Amount `json:"assets"`
}

type HistoryEntry struct {
	ID     string    `json:"id"`
	Height uint64    `json:"height"`
	Time   int64     `json:"time"`
	Tx     *types.Tx `json:"tx"`
}

func (c *Client) GetJSON(path string, out any) error {
	resp, err := c.HTTP.Get(c.Base + path)
	if err != nil {
		return fmt.Errorf("node unreachable: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("node returned %s for %s", resp.Status, path)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func (c *Client) Status() (st Status, err error) { return st, c.GetJSON("/v1/status", &st) }

func (c *Client) Account(a types.Address) (acc Account, err error) {
	return acc, c.GetJSON("/v1/accounts/"+string(a), &acc)
}

// History returns up to 500 recent committed txs touching a, newest first.
func (c *Client) History(a types.Address) (h []HistoryEntry, err error) {
	return h, c.GetJSON("/v1/accounts/"+string(a)+"/txs?limit=500", &h)
}

// Tx returns a tx's status: "pending", "committed", or an error if unknown.
func (c *Client) TxStatus(id string) (string, error) {
	var r struct {
		Status string `json:"status"`
	}
	return r.Status, c.GetJSON("/v1/txs/"+id, &r)
}

// Submit posts a signed tx and returns its id.
func (c *Client) Submit(tx *types.Tx) (string, error) {
	body, _ := json.Marshal(tx)
	resp, err := c.HTTP.Post(c.Base+"/v1/txs", "application/json", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("node unreachable: %w", err)
	}
	defer resp.Body.Close()
	var res struct {
		ID    string `json:"id"`
		Error string `json:"error"`
	}
	json.NewDecoder(resp.Body).Decode(&res)
	if resp.StatusCode >= 300 {
		return "", fmt.Errorf("node rejected tx: %s", res.Error)
	}
	return res.ID, nil
}

// SignAndSubmit fills chain id, sender, nonce and (unless set) the minimum
// fee for tx.Asset, signs with key and submits.
func (c *Client) SignAndSubmit(key ed25519.PrivateKey, tx *types.Tx) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	st, err := c.Status()
	if err != nil {
		return "", err
	}
	from := types.AddressFromPubKey(key.Public().(ed25519.PublicKey))
	acc, err := c.Account(from)
	if err != nil {
		return "", err
	}
	tx.ChainID, tx.From, tx.Nonce = st.ChainID, from, acc.NextNonce
	if tx.Fee == 0 && tx.Kind != types.KindMint {
		tx.Fee = st.MinFee
		for _, a := range st.Assets {
			if a.Symbol == tx.Asset {
				tx.Fee = a.MinFee
			}
		}
	}
	tx.Sign(key)
	return c.Submit(tx)
}

// WaitCommitted polls until tx id is in a block or the timeout passes.
func (c *Client) WaitCommitted(id string, timeout time.Duration) error {
	end := time.Now().Add(timeout)
	for time.Now().Before(end) {
		if s, err := c.TxStatus(id); err == nil && s == "committed" {
			return nil
		}
		time.Sleep(300 * time.Millisecond)
	}
	return fmt.Errorf("tx %s not committed within %s", id, timeout)
}
