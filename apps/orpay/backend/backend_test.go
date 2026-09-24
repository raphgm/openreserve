package main

import (
	"bytes"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/openreserve/node/api"
	"github.com/openreserve/node/chain"
	"github.com/openreserve/node/client"
	"github.com/openreserve/node/keys"
	"github.com/openreserve/node/reqauth"
	"github.com/openreserve/node/types"
)

type wallet struct {
	priv ed25519.PrivateKey
	addr types.Address
}

func newWallet(t *testing.T) wallet {
	_, priv, _ := ed25519.GenerateKey(nil)
	return wallet{priv, keys.Address(priv)}
}

type env struct {
	t        *testing.T
	c        *chain.Chain
	proposer ed25519.PrivateKey
	nodeURL  string
	api      *httptest.Server
	srv      *server
	dir      string
	faucet   wallet
	admin    wallet
	nowMs    int64
}

func newEnv(t *testing.T, funded ...wallet) *env {
	e := &env{t: t, dir: t.TempDir(), faucet: newWallet(t), admin: newWallet(t), nowMs: 1_000}
	prop := newWallet(t)
	e.proposer = prop.priv
	allocs := []chain.Allocation{{Address: e.faucet.addr, Amount: 10_000 * types.Unit}}
	for _, w := range funded {
		allocs = append(allocs, chain.Allocation{Address: w.addr, Amount: 1_000 * types.Unit})
	}
	g := &chain.Genesis{ChainID: "test", Time: 1, Proposer: prop.addr, MinFee: 1000, Allocations: allocs}
	c, err := chain.Open(t.TempDir(), g)
	if err != nil {
		t.Fatal(err)
	}
	e.c = c
	node := httptest.NewServer(api.Handler(c))
	t.Cleanup(node.Close)
	e.nodeURL = node.URL
	t.Setenv("ORPAY_FAUCET_SEED", keys.SeedHex(e.faucet.priv))
	e.start()
	return e
}

// start (re)starts the backend on the same state directory.
func (e *env) start() {
	if e.api != nil {
		e.api.Close()
	}
	s, err := newServer(serverConfig{
		usersPath: e.dir + "/users.json", stateDir: e.dir,
		node:         client.New(e.nodeURL),
		privateHooks: true, faucetAmount: "100", faucetWait: time.Hour,
		admins: string(e.admin.addr), publicURL: "https://pay.test",
	})
	if err != nil {
		e.t.Fatal(err)
	}
	e.srv = s
	e.api = httptest.NewServer(s.routes(false))
	e.t.Cleanup(e.api.Close)
}

func (e *env) block() {
	e.nowMs += 1000
	if _, err := e.c.Produce(e.proposer, e.nowMs); err != nil {
		e.t.Fatal(err)
	}
}

// call makes a request, signed by w when w is non-nil, or with an API key.
func (e *env) call(method, path string, body any, w *wallet, apiKey string, out any) int {
	e.t.Helper()
	var b []byte
	if body != nil {
		b, _ = json.Marshal(body)
	}
	req, _ := http.NewRequest(method, e.api.URL+path, bytes.NewReader(b))
	if w != nil {
		ts := time.Now().UnixMilli()
		sig := ed25519.Sign(w.priv, []byte(reqauth.RequestMessage(method, strings.SplitN(path, "?", 2)[0], ts, b)))
		req.Header.Set("X-ORP-Address", string(w.addr))
		req.Header.Set("X-ORP-Time", strconv.FormatInt(ts, 10))
		req.Header.Set("X-ORP-Sig", hex.EncodeToString(sig))
	}
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if out != nil {
		json.Unmarshal(data, out)
	}
	if resp.StatusCode >= 300 {
		e.t.Logf("%s %s -> %d %s", method, path, resp.StatusCode, data)
	}
	return resp.StatusCode
}

func (e *env) pay(from wallet, to types.Address, amt types.Amount, memo string) {
	e.t.Helper()
	acc, next := e.c.Account(from.addr)
	_ = acc
	tx := &types.Tx{ChainID: "test", From: from.addr, To: to, Amount: amt, Fee: 1000, Nonce: next, Memo: memo}
	tx.Sign(from.priv)
	if _, err := e.c.Submit(tx); err != nil {
		e.t.Fatal(err)
	}
	e.block()
}

func TestSignedRequests(t *testing.T) {
	e := newEnv(t)
	w := newWallet(t)
	body := map[string]string{"name": "Gabis", "website": "https://gabis.app", "contact_email": "a@gabis.app"}
	if code := e.call("POST", "/api/apps", body, nil, "", nil); code != 401 {
		t.Errorf("unsigned: %d", code)
	}

	// A signature over a different body is rejected.
	b, _ := json.Marshal(body)
	ts := time.Now().UnixMilli()
	sig := ed25519.Sign(w.priv, []byte(reqauth.RequestMessage("POST", "/api/apps", ts, []byte(`{"name":"other"}`))))
	req, _ := http.NewRequest("POST", e.api.URL+"/api/apps", bytes.NewReader(b))
	req.Header.Set("X-ORP-Address", string(w.addr))
	req.Header.Set("X-ORP-Time", strconv.FormatInt(ts, 10))
	req.Header.Set("X-ORP-Sig", hex.EncodeToString(sig))
	resp, _ := http.DefaultClient.Do(req)
	if resp.StatusCode != 401 {
		t.Errorf("tampered body: %d", resp.StatusCode)
	}

	// Stale timestamps are rejected.
	old := time.Now().Add(-10 * time.Minute).UnixMilli()
	sig = ed25519.Sign(w.priv, []byte(reqauth.RequestMessage("GET", "/api/apps", old, nil)))
	req, _ = http.NewRequest("GET", e.api.URL+"/api/apps", nil)
	req.Header.Set("X-ORP-Address", string(w.addr))
	req.Header.Set("X-ORP-Time", strconv.FormatInt(old, 10))
	req.Header.Set("X-ORP-Sig", hex.EncodeToString(sig))
	resp, _ = http.DefaultClient.Do(req)
	if resp.StatusCode != 401 {
		t.Errorf("stale request: %d", resp.StatusCode)
	}
}

func TestPartnerCheckoutFlow(t *testing.T) {
	customer := newWallet(t)
	e := newEnv(t, customer)
	owner := newWallet(t)

	// Webhook receiver that verifies signatures.
	var mu sync.Mutex
	var events []map[string]any
	var secret string
	hook := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		sig := r.Header.Get("ORPay-Signature")
		tsPart, _, _ := strings.Cut(strings.TrimPrefix(sig, "t="), ",")
		ts, _ := strconv.ParseInt(tsPart, 10, 64)
		mu.Lock()
		defer mu.Unlock()
		if SignWebhook(secret, ts, body) != sig {
			t.Errorf("bad webhook signature")
			w.WriteHeader(400)
			return
		}
		var ev map[string]any
		json.Unmarshal(body, &ev)
		events = append(events, ev)
	}))
	defer hook.Close()

	// 1. Company requests access.
	var app App
	code := e.call("POST", "/api/apps", map[string]string{
		"name": "Paynautik", "website": "https://paynautik.com", "contact_email": "dev@paynautik.com",
	}, &owner, "", &app)
	if code != 201 || app.Status != AppPending || app.Settlement != owner.addr {
		t.Fatalf("request app: %d %+v", code, app)
	}
	if code := e.call("POST", "/api/apps", map[string]string{"name": "paynautik", "website": "https://x.com", "contact_email": "a@b.c"}, &owner, "", nil); code != 409 {
		t.Errorf("duplicate name: %d", code)
	}
	// Keys are not issued before approval.
	if code := e.call("POST", "/api/apps/"+app.ID+"/keys", nil, &owner, "", nil); code != 400 {
		t.Errorf("key before approval: %d", code)
	}

	// 2. Only an admin can approve.
	if code := e.call("POST", "/api/apps/"+app.ID+"/review", map[string]string{"decision": "approve"}, &owner, "", nil); code != 403 {
		t.Errorf("owner self-approval: %d", code)
	}
	if code := e.call("POST", "/api/apps/"+app.ID+"/review", map[string]string{"decision": "approve"}, &e.admin, "", nil); code != 200 {
		t.Fatalf("admin approve: %d", code)
	}

	// 3. Owner gets a key and webhook secret, sets a webhook URL.
	var key struct {
		APIKey string `json:"api_key"`
	}
	e.call("POST", "/api/apps/"+app.ID+"/keys", nil, &owner, "", &key)
	var list struct {
		Apps []App `json:"apps"`
	}
	e.call("GET", "/api/apps", nil, &owner, "", &list)
	if len(list.Apps) != 1 || list.Apps[0].WebhookSecret == "" || list.Apps[0].KeyHash != "" {
		t.Fatalf("owner app view: %+v", list.Apps)
	}
	secret = list.Apps[0].WebhookSecret
	var adminList struct {
		Apps []App `json:"apps"`
	}
	e.call("GET", "/api/apps", nil, &e.admin, "", &adminList)
	if len(adminList.Apps) != 1 || adminList.Apps[0].WebhookSecret != "" {
		t.Errorf("admin must see apps but not secrets: %+v", adminList.Apps)
	}
	if code := e.call("POST", "/api/apps/"+app.ID+"/settings", map[string]string{"webhook_url": hook.URL}, &owner, "", nil); code != 200 {
		t.Fatalf("set webhook: %d", code)
	}

	// 4. The app's server creates a checkout with its API key.
	if code := e.call("POST", "/api/v1/checkout", map[string]string{"amount": "5"}, nil, "orp_sk_wrong", nil); code != 401 {
		t.Errorf("bad key: %d", code)
	}
	var inv map[string]any
	code = e.call("POST", "/api/v1/checkout", map[string]any{
		"amount": "12.5", "description": "Order #42", "reference": "order-42", "return_url": "https://paynautik.com/done",
	}, nil, key.APIKey, &inv)
	if code != 201 || inv["status"] != "pending" || !strings.HasPrefix(inv["checkout_url"].(string), "https://pay.test/?invoice=") {
		t.Fatalf("create checkout: %d %v", code, inv)
	}
	id := inv["id"].(string)

	// The public view hides the app's reference.
	var pub map[string]any
	e.call("GET", "/api/invoices/"+id, nil, nil, "", &pub)
	if _, leaked := pub["reference"]; leaked || pub["app"].(map[string]any)["name"] != "Paynautik" {
		t.Errorf("public invoice view: %v", pub)
	}

	// 5. Underpaying does not settle it; paying the full amount does.
	e.pay(customer, owner.addr, 12*types.Unit, "inv:"+id)
	e.srv.checkInvoices()
	if e.srv.invoice(id).Status != InvoicePending {
		t.Fatal("underpayment marked paid")
	}
	e.pay(customer, owner.addr, 12_500_000, "inv:"+id)
	e.srv.checkInvoices()
	got := e.srv.invoice(id)
	if got.Status != InvoicePaid || got.PaidBy != customer.addr {
		t.Fatalf("after payment: %+v", got)
	}
	e.srv.deliverWebhooks()
	mu.Lock()
	if len(events) != 1 || events[0]["type"] != "invoice.paid" {
		t.Errorf("webhook events: %v", events)
	}
	mu.Unlock()

	var status map[string]any
	e.call("GET", "/api/v1/checkout/"+id, nil, nil, key.APIKey, &status)
	if status["status"] != "paid" || status["reference"] != "order-42" {
		t.Errorf("app view after payment: %v", status)
	}

	// 6. Suspension cuts off API access.
	e.call("POST", "/api/apps/"+app.ID+"/review", map[string]string{"decision": "suspend"}, &e.admin, "", nil)
	if code := e.call("POST", "/api/v1/checkout", map[string]string{"amount": "1"}, nil, key.APIKey, nil); code != 403 {
		t.Errorf("suspended app key: %d", code)
	}
}

func TestInvoiceExpiry(t *testing.T) {
	e := newEnv(t)
	owner := newWallet(t)
	var app App
	e.call("POST", "/api/apps", map[string]string{"name": "Gabis", "website": "https://gabis.app", "contact_email": "a@gabis.app"}, &owner, "", &app)
	e.call("POST", "/api/apps/"+app.ID+"/review", map[string]string{"decision": "approve"}, &e.admin, "", nil)
	var key struct {
		APIKey string `json:"api_key"`
	}
	e.call("POST", "/api/apps/"+app.ID+"/keys", nil, &owner, "", &key)
	var inv map[string]any
	e.call("POST", "/api/v1/checkout", map[string]any{"amount": "1", "expires_in_minutes": 1}, nil, key.APIKey, &inv)
	e.srv.now = func() time.Time { return time.Now().Add(2 * time.Minute) }
	e.srv.checkInvoices()
	if s := e.srv.invoice(inv["id"].(string)).Status; s != InvoiceExpired {
		t.Errorf("status %s, want expired", s)
	}
}

func TestFaucetCooldownSurvivesRestart(t *testing.T) {
	e := newEnv(t)
	w := newWallet(t)
	if code := e.call("POST", "/api/faucet", map[string]any{"address": w.addr}, nil, "", nil); code != 202 {
		t.Fatalf("first drip: %d", code)
	}
	e.start()
	if code := e.call("POST", "/api/faucet", map[string]any{"address": w.addr}, nil, "", nil); code != 429 {
		t.Errorf("drip after restart: %d, want 429", code)
	}
}

func TestUsernames(t *testing.T) {
	e := newEnv(t)
	w := newWallet(t)
	sig := hex.EncodeToString(ed25519.Sign(w.priv, []byte(RegisterMessage("gabi", w.addr))))
	if code := e.call("POST", "/api/users", map[string]any{"username": "gabi", "address": w.addr, "sig": sig}, nil, "", nil); code != 201 {
		t.Fatalf("register: %d", code)
	}
	other := newWallet(t)
	sig = hex.EncodeToString(ed25519.Sign(other.priv, []byte(RegisterMessage("gabi", other.addr))))
	if code := e.call("POST", "/api/users", map[string]any{"username": "gabi", "address": other.addr, "sig": sig}, nil, "", nil); code != 409 {
		t.Errorf("taken name: %d", code)
	}
	var r map[string]any
	e.call("GET", "/api/users/@gabi", nil, nil, "", &r)
	if r["address"] != string(w.addr) {
		t.Errorf("resolve: %v", r)
	}
}
