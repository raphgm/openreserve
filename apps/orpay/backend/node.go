package main

import (
	"bytes"
	"crypto/ed25519"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/openreserve/node/keys"
	"github.com/openreserve/node/types"
)

// nodeClient talks to an OpenReserve node's HTTP API.
type nodeClient struct {
	base string
	http *http.Client
}

type historyEntry struct {
	ID     string    `json:"id"`
	Height uint64    `json:"height"`
	Time   int64     `json:"time"`
	Tx     *types.Tx `json:"tx"`
}

func (n *nodeClient) getJSON(path string, out any) error {
	resp, err := n.http.Get(n.base + path)
	if err != nil {
		return fmt.Errorf("node unreachable: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("node returned %s for %s", resp.Status, path)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

// history returns up to 500 recent committed txs touching addr, newest first.
func (n *nodeClient) history(addr types.Address) ([]historyEntry, error) {
	var h []historyEntry
	return h, n.getJSON("/v1/accounts/"+string(addr)+"/txs?limit=500", &h)
}

// send signs and submits a transfer from key.
func (n *nodeClient) send(key ed25519.PrivateKey, to types.Address, amt types.Amount, memo string) (string, error) {
	from := keys.Address(key)
	var st struct {
		ChainID string       `json:"chain_id"`
		MinFee  types.Amount `json:"min_fee"`
	}
	if err := n.getJSON("/v1/status", &st); err != nil {
		return "", err
	}
	var acc struct {
		NextNonce uint64 `json:"next_nonce"`
	}
	if err := n.getJSON("/v1/accounts/"+string(from), &acc); err != nil {
		return "", err
	}
	tx := &types.Tx{ChainID: st.ChainID, From: from, To: to, Amount: amt, Fee: st.MinFee, Nonce: acc.NextNonce, Memo: memo}
	tx.Sign(key)
	body, _ := json.Marshal(tx)
	resp, err := n.http.Post(n.base+"/v1/txs", "application/json", bytes.NewReader(body))
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
