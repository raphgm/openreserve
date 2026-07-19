package core

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Client handles HTTP communication with the OpenReserve Node
type Client struct {
	NodeURL string
}

func NewClient(nodeURL string) *Client {
	return &Client{
		NodeURL: nodeURL,
	}
}

// BalanceResponse maps to the node's /balance endpoint response
type BalanceResponse struct {
	Address string  `json:"address"`
	Balance float64 `json:"balance"`
	Nonce   uint64  `json:"nonce"`
}

// GetBalance queries the node for an account's ORP balance and nonce
func (c *Client) GetBalance(address string) (*BalanceResponse, error) {
	resp, err := http.Get(fmt.Sprintf("%s/balance?address=%s", c.NodeURL, address))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("node returned status: %d", resp.StatusCode)
	}

	var balanceResp BalanceResponse
	if err := json.NewDecoder(resp.Body).Decode(&balanceResp); err != nil {
		return nil, err
	}

	return &balanceResp, nil
}

// SendTransaction builds, signs, and broadcasts an ORP transfer
func (c *Client) SendTransaction(wallet *KeyPair, receiver string, amount, fee float64) error {
	// 1. Fetch current nonce to prevent replay attacks
	balResp, err := c.GetBalance(wallet.Address)
	if err != nil {
		return fmt.Errorf("failed to fetch nonce: %w", err)
	}

	// 2. Build the payload matching the Node's expected format
	nonce := balResp.Nonce
	timestamp := time.Now().Unix()

	// The hash string must match exactly how the node hashes it in core.Transaction.Hash()
	record := fmt.Sprintf("%s:%s:%f:%f:%d:%d", wallet.Address, receiver, amount, fee, nonce, timestamp)
	
	// 3. Sign the payload
	signature := wallet.SignPayload([]byte(record))

	// 4. Construct the JSON request
	reqBody := map[string]interface{}{
		"sender":    wallet.Address,
		"receiver":  receiver,
		"amount":    amount,
		"fee":       fee,
		"nonce":     nonce,
		"publicKey": hex.EncodeToString(wallet.PublicKey),
		"signature": hex.EncodeToString(signature),
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return err
	}

	// 5. POST to Node
	resp, err := http.Post(c.NodeURL+"/transaction", "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		return fmt.Errorf("failed to broadcast tx: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("transaction rejected: %s", string(bodyBytes))
	}

	return nil
}
