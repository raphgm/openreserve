package main

import "net/http"

// Provider is a money rail: something that can collect naira from a
// customer, pay naira out to a bank account, and report its balance.
// Paystack and Flutterwave implement it; a bank or microfinance-bank partner
// holding customer funds under its licence would be another implementation.
// All amounts are in kobo.
type Provider interface {
	Name() string
	TestMode() bool

	// StartCollection begins a hosted checkout and returns its URL.
	StartCollection(email string, kobo int64, ref, callbackURL string, meta map[string]string) (string, error)
	// VerifyCollection asks the provider (never the customer) how a
	// collection went.
	VerifyCollection(ref string) (Collection, error)
	// GrossUp returns what to charge so about net kobo remain after fees.
	GrossUp(net int64) int64

	Banks() ([]Bank, error)
	ResolveAccount(accountNumber, bankCode string) (string, error)
	// Payout sends money to a bank account. ref is our withdrawal id and
	// must make retries idempotent at the provider.
	Payout(kobo int64, accountName, accountNumber, bankCode, ref, reason string) (Status, error)
	// PayoutStatus looks a payout up by ref; an error means none exists.
	PayoutStatus(ref string) (Status, error)
	BalanceKobo() (int64, error)

	// ParseWebhook authenticates a webhook and extracts what happened.
	ParseWebhook(h http.Header, body []byte) (WebhookEvent, error)
}

// Status is a normalized outcome.
type Status string

const (
	StatusSuccess Status = "success"
	StatusPending Status = "pending"
	StatusFailed  Status = "failed"
)

type Collection struct {
	Status    Status
	Ref       string
	GrossKobo int64 // charged to the customer
	FeeKobo   int64 // kept by the provider
	Currency  string
}

type Bank struct {
	Name string `json:"name"`
	Code string `json:"code"`
}

// WebhookEvent says which of our references changed. The gateway always
// re-verifies with the provider before moving money, so a webhook only
// triggers work; it is never trusted for amounts.
type WebhookEvent struct {
	Kind   string // "collection" or "payout"
	Ref    string
	Status Status
}
