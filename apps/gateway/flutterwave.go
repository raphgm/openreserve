package main

import (
	"bytes"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// flutterwave implements Provider on the Flutterwave v3 API
// (https://developer.flutterwave.com/docs). Flutterwave amounts are decimal
// naira; the adapter converts to and from kobo at the edges.
type flutterwave struct {
	base       string
	secret     string // from FLW_SECRET_KEY in .env
	secretHash string // the "secret hash" set in the dashboard; sent as verif-hash on webhooks
	http       *http.Client
}

func newFlutterwave(base, secret, secretHash string) *flutterwave {
	return &flutterwave{base: base, secret: secret, secretHash: secretHash, http: &http.Client{Timeout: 20 * time.Second}}
}

func (f *flutterwave) Name() string   { return "flutterwave" }
func (f *flutterwave) TestMode() bool { return strings.Contains(f.secret, "_TEST") }

func koboToNaira(k int64) float64 { return float64(k) / 100 }
func nairaToKobo(n float64) int64 { return int64(math.Round(n * 100)) }

func (f *flutterwave) do(method, path string, body, out any) error {
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, f.base+path, rd)
	req.Header.Set("Authorization", "Bearer "+f.secret)
	req.Header.Set("Content-Type", "application/json")
	resp, err := f.http.Do(req)
	if err != nil {
		return fmt.Errorf("flutterwave unreachable: %w", err)
	}
	defer resp.Body.Close()
	var env struct {
		Status  string          `json:"status"`
		Message string          `json:"message"`
		Data    json.RawMessage `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		return fmt.Errorf("flutterwave %s %s: bad response (%s)", method, path, resp.Status)
	}
	if env.Status != "success" || resp.StatusCode >= 300 {
		return fmt.Errorf("flutterwave: %s", env.Message)
	}
	if out != nil {
		return json.Unmarshal(env.Data, out)
	}
	return nil
}

func (f *flutterwave) StartCollection(email string, kobo int64, ref, callbackURL string, meta map[string]string) (string, error) {
	var out struct {
		Link string `json:"link"`
	}
	err := f.do("POST", "/payments", map[string]any{
		"tx_ref": ref, "amount": koboToNaira(kobo), "currency": "NGN", "redirect_url": callbackURL,
		"customer": map[string]string{"email": email}, "meta": meta,
		"customizations": map[string]string{"title": "ORPay"},
	}, &out)
	return out.Link, err
}

func (f *flutterwave) VerifyCollection(ref string) (Collection, error) {
	var tx struct {
		Status   string  `json:"status"`
		TxRef    string  `json:"tx_ref"`
		Amount   float64 `json:"amount"`
		AppFee   float64 `json:"app_fee"`
		Currency string  `json:"currency"`
	}
	if err := f.do("GET", "/transactions/verify_by_reference?tx_ref="+url.QueryEscape(ref), nil, &tx); err != nil {
		return Collection{}, err
	}
	c := Collection{Ref: tx.TxRef, GrossKobo: nairaToKobo(tx.Amount), FeeKobo: nairaToKobo(tx.AppFee), Currency: tx.Currency, Status: StatusPending}
	switch strings.ToLower(tx.Status) {
	case "successful":
		c.Status = StatusSuccess
	case "failed", "cancelled":
		c.Status = StatusFailed
	}
	return c, nil
}

// GrossUp for Flutterwave's local-card pricing: 1.4%, capped at ₦2,000.
func (f *flutterwave) GrossUp(net int64) int64 {
	gross := (net*1000 + 985) / 986 // ceil(net / 0.986)
	if gross-net > 200_000 {
		gross = net + 200_000
	}
	return gross
}

func (f *flutterwave) Banks() ([]Bank, error) {
	var out []Bank
	err := f.do("GET", "/banks/NG", nil, &out)
	return out, err
}

func (f *flutterwave) ResolveAccount(acct, bank string) (string, error) {
	var out struct {
		AccountName string `json:"account_name"`
	}
	err := f.do("POST", "/accounts/resolve", map[string]string{"account_number": acct, "account_bank": bank}, &out)
	return out.AccountName, err
}

func normalizeFlwTransfer(s string) Status {
	switch strings.ToUpper(s) {
	case "SUCCESSFUL":
		return StatusSuccess
	case "FAILED":
		return StatusFailed
	}
	return StatusPending // NEW, PENDING
}

func (f *flutterwave) Payout(kobo int64, name, acct, bank, ref, reason string) (Status, error) {
	var out struct {
		Status string `json:"status"`
	}
	err := f.do("POST", "/transfers", map[string]any{
		"account_bank": bank, "account_number": acct, "amount": koboToNaira(kobo), "currency": "NGN",
		"debit_currency": "NGN", "reference": ref, "narration": reason, "beneficiary_name": name,
	}, &out)
	if err != nil {
		return StatusFailed, err
	}
	return normalizeFlwTransfer(out.Status), nil
}

func (f *flutterwave) PayoutStatus(ref string) (Status, error) {
	var out []struct {
		Status string `json:"status"`
	}
	if err := f.do("GET", "/transfers?reference="+url.QueryEscape(ref), nil, &out); err != nil {
		return "", err
	}
	if len(out) == 0 {
		return "", errors.New("transfer not found")
	}
	return normalizeFlwTransfer(out[0].Status), nil
}

func (f *flutterwave) BalanceKobo() (int64, error) {
	var out struct {
		Available float64 `json:"available_balance"`
	}
	if err := f.do("GET", "/balances/NGN", nil, &out); err != nil {
		return 0, err
	}
	return nairaToKobo(out.Available), nil
}

// ParseWebhook checks the verif-hash header against the dashboard secret hash.
func (f *flutterwave) ParseWebhook(h http.Header, body []byte) (WebhookEvent, error) {
	got := h.Get("verif-hash")
	if f.secretHash == "" || subtle.ConstantTimeCompare([]byte(got), []byte(f.secretHash)) != 1 {
		return WebhookEvent{}, errors.New("invalid signature")
	}
	var ev struct {
		Event string `json:"event"`
		Data  struct {
			TxRef     string `json:"tx_ref"`
			Reference string `json:"reference"`
			Status    string `json:"status"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &ev); err != nil {
		return WebhookEvent{}, err
	}
	switch ev.Event {
	case "charge.completed":
		st := StatusPending
		if strings.EqualFold(ev.Data.Status, "successful") {
			st = StatusSuccess
		}
		return WebhookEvent{Kind: "collection", Ref: ev.Data.TxRef, Status: st}, nil
	case "transfer.completed":
		return WebhookEvent{Kind: "payout", Ref: ev.Data.Reference, Status: normalizeFlwTransfer(ev.Data.Status)}, nil
	}
	return WebhookEvent{}, nil
}
