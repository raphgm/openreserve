package main

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha512"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

// paystack is a minimal client for the Paystack API
// (https://paystack.com/docs/api). Amounts are in kobo.
type paystack struct {
	base   string
	secret string
	http   *http.Client
}

func newPaystack(base, secret string) *paystack {
	return &paystack{base: base, secret: secret, http: &http.Client{Timeout: 20 * time.Second}}
}

type psEnvelope struct {
	Status  bool            `json:"status"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}

func (p *paystack) do(method, path string, body, out any) error {
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, p.base+path, rd)
	req.Header.Set("Authorization", "Bearer "+p.secret)
	req.Header.Set("Content-Type", "application/json")
	resp, err := p.http.Do(req)
	if err != nil {
		return fmt.Errorf("paystack unreachable: %w", err)
	}
	defer resp.Body.Close()
	var env psEnvelope
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		return fmt.Errorf("paystack %s %s: bad response (%s)", method, path, resp.Status)
	}
	if !env.Status || resp.StatusCode >= 300 {
		return fmt.Errorf("paystack: %s", env.Message)
	}
	if out != nil {
		return json.Unmarshal(env.Data, out)
	}
	return nil
}

type psInit struct {
	AuthorizationURL string `json:"authorization_url"`
	AccessCode       string `json:"access_code"`
	Reference        string `json:"reference"`
}

func (p *paystack) initialize(email string, kobo int64, reference, callbackURL string, metadata map[string]string) (psInit, error) {
	var out psInit
	err := p.do("POST", "/transaction/initialize", map[string]any{
		"email": email, "amount": kobo, "currency": "NGN", "reference": reference,
		"callback_url": callbackURL, "metadata": metadata,
	}, &out)
	return out, err
}

type psTransaction struct {
	Status    string `json:"status"` // success, failed, abandoned, ...
	Reference string `json:"reference"`
	Amount    int64  `json:"amount"`
	Fees      int64  `json:"fees"`
	Currency  string `json:"currency"`
}

func (p *paystack) verify(reference string) (psTransaction, error) {
	var out psTransaction
	err := p.do("GET", "/transaction/verify/"+url.PathEscape(reference), nil, &out)
	return out, err
}

type psBank struct {
	Name string `json:"name"`
	Code string `json:"code"`
}

func (p *paystack) banks() ([]psBank, error) {
	var out []psBank
	err := p.do("GET", "/bank?country=nigeria&currency=NGN&perPage=100", nil, &out)
	return out, err
}

func (p *paystack) resolveAccount(accountNumber, bankCode string) (string, error) {
	var out struct {
		AccountName string `json:"account_name"`
	}
	q := url.Values{"account_number": {accountNumber}, "bank_code": {bankCode}}
	err := p.do("GET", "/bank/resolve?"+q.Encode(), nil, &out)
	return out.AccountName, err
}

func (p *paystack) createRecipient(name, accountNumber, bankCode string) (string, error) {
	var out struct {
		RecipientCode string `json:"recipient_code"`
	}
	err := p.do("POST", "/transferrecipient", map[string]any{
		"type": "nuban", "name": name, "account_number": accountNumber, "bank_code": bankCode, "currency": "NGN",
	}, &out)
	return out.RecipientCode, err
}

// transfer pays out from the Paystack balance. Status is "pending",
// "success", or "otp" when the account requires OTP approval (disable OTP
// for transfers in the Paystack dashboard to automate payouts).
func (p *paystack) transfer(kobo int64, recipient, reference, reason string) (string, error) {
	var out struct {
		Status string `json:"status"`
	}
	err := p.do("POST", "/transfer", map[string]any{
		"source": "balance", "amount": kobo, "recipient": recipient, "reference": reference, "reason": reason,
	}, &out)
	return out.Status, err
}

// transferStatus looks up a transfer by our reference. An error means
// Paystack has no such transfer.
func (p *paystack) transferStatus(reference string) (string, error) {
	var out struct {
		Status string `json:"status"`
	}
	err := p.do("GET", "/transfer/verify/"+url.PathEscape(reference), nil, &out)
	return out.Status, err
}

func (p *paystack) balanceNGN() (int64, error) {
	var out []struct {
		Currency string `json:"currency"`
		Balance  int64  `json:"balance"`
	}
	if err := p.do("GET", "/balance", nil, &out); err != nil {
		return 0, err
	}
	for _, b := range out {
		if b.Currency == "NGN" {
			return b.Balance, nil
		}
	}
	return 0, errors.New("no NGN balance on this Paystack account")
}

// validSignature checks Paystack's x-paystack-signature header: the hex
// HMAC-SHA512 of the raw body keyed with the secret key.
func validSignature(secret string, body []byte, header string) bool {
	mac := hmac.New(sha512.New, []byte(secret))
	mac.Write(body)
	want := mac.Sum(nil)
	got, err := hex.DecodeString(header)
	return err == nil && hmac.Equal(got, want)
}

// Paystack Nigeria local-card pricing: 1.5% + ₦100, the ₦100 waived under
// ₦2,500, capped at ₦2,000. grossUp returns what to charge so that roughly
// net kobo remain after Paystack's fee; the exact fee Paystack reports is
// what the gateway uses when minting.
func grossUp(net int64) int64 {
	ceilDiv := func(x int64) int64 { return (x*1000 + 984) / 985 } // ceil(x / 0.985)
	gross := ceilDiv(net)
	// The ₦100 flat fee is waived only when the *charged* amount is under
	// ₦2,500, so check the grossed-up amount, not the net.
	if gross >= 250_000 {
		gross = ceilDiv(net + 10_000)
	}
	if gross-net > 200_000 {
		gross = net + 200_000
	}
	return gross
}
