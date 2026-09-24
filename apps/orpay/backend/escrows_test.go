package main

import (
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

	"github.com/openreserve/node/types"
)

func (e *env) escrowTx(w wallet, x *types.EscrowOp, fee types.Amount) string {
	e.t.Helper()
	_, next := e.c.Account(w.addr)
	tx := &types.Tx{ChainID: "test", From: w.addr, Fee: fee, Nonce: next, Escrow: x}
	tx.Sign(w.priv)
	if _, err := e.c.Submit(tx); err != nil {
		e.t.Fatal(err)
	}
	e.block()
	return tx.ID().String()
}

func TestPartnerEscrowFlow(t *testing.T) {
	client := newWallet(t) // pays for the work
	e := newEnv(t, client)
	e.c.Clock = func() int64 { return time.Now().UnixMilli() }
	e.nowMs = time.Now().UnixMilli()
	owner, developer := newWallet(t), newWallet(t)

	var mu sync.Mutex
	var events []string
	var secret string
	hook := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		sig := r.Header.Get("ORPay-Signature")
		ts, _ := strconv.ParseInt(strings.TrimPrefix(strings.SplitN(sig, ",", 2)[0], "t="), 10, 64)
		mu.Lock()
		defer mu.Unlock()
		if SignWebhook(secret, ts, body) != sig {
			t.Error("bad signature")
		}
		var ev struct {
			Type string `json:"type"`
		}
		json.Unmarshal(body, &ev)
		events = append(events, ev.Type)
	}))
	defer hook.Close()

	// SSLabs is approved and wires a webhook; the developer has a username.
	var app App
	e.call("POST", "/api/apps", map[string]string{"name": "SSLabs", "website": "https://sslabs.example", "contact_email": "dev@sslabs.example"}, &owner, "", &app)
	e.call("POST", "/api/apps/"+app.ID+"/review", map[string]string{"decision": "approve"}, &e.admin, "", nil)
	var key struct {
		APIKey string `json:"api_key"`
	}
	e.call("POST", "/api/apps/"+app.ID+"/keys", nil, &owner, "", &key)
	e.call("POST", "/api/apps/"+app.ID+"/settings", map[string]string{"webhook_url": hook.URL}, &owner, "", nil)
	var list struct {
		Apps []App `json:"apps"`
	}
	e.call("GET", "/api/apps", nil, &owner, "", &list)
	secret = list.Apps[0].WebhookSecret
	sig := hex.EncodeToString(ed25519.Sign(developer.priv, []byte(RegisterMessage("ada_dev", developer.addr))))
	e.call("POST", "/api/users", map[string]any{"username": "ada_dev", "address": developer.addr, "sig": sig}, nil, "", nil)

	// SSLabs holds the developer's payout in two milestones.
	var req map[string]any
	code := e.call("POST", "/api/v1/escrows", map[string]any{
		"seller": "@ada_dev", "currency": "ORP", "description": "Landing page build",
		"milestones":   []map[string]string{{"label": "Design approved", "amount": "40"}, {"label": "Site delivered", "amount": "60"}},
		"ship_by_days": 10, "review_days": 2, "reference": "job-17",
	}, nil, key.APIKey, &req)
	if code != 201 || req["arbiter"] != string(owner.addr) || !strings.Contains(req["funding_url"].(string), "escrow_request=esr_") {
		t.Fatalf("create request: %d %v", code, req)
	}
	id := req["id"].(string)
	terms := func(ms ...types.Amount) *types.EscrowOp {
		return &types.EscrowOp{Op: types.EscrowCreate, Seller: developer.addr, Arbiter: owner.addr, Milestones: ms,
			ShipBy: time.Now().Add(10 * 24 * time.Hour).UnixMilli(), ReviewSecs: 2 * 86400, Ref: id}
	}

	// An escrow with the right ref but different terms is not linked.
	e.escrowTx(client, terms(1*types.Unit), 1000)
	e.srv.checkEscrows()
	if e.srv.escrowRequest(id).Status != EscrowAwaiting {
		t.Fatal("linked an escrow with the wrong milestones")
	}

	escrowID := e.escrowTx(client, terms(40*types.Unit, 60*types.Unit), 1000)
	e.srv.checkEscrows()
	if got := e.srv.escrowRequest(id); got.Status != EscrowLinked || got.EscrowID != escrowID {
		t.Fatalf("after funding: %+v", got)
	}

	// Developer delivers; the client approves both milestones.
	op := func(w wallet, o string) { e.escrowTx(w, &types.EscrowOp{Op: o, ID: types.MustHash(escrowID)}, 0) }
	op(developer, types.EscrowDispatch)
	e.srv.checkEscrows()
	op(client, types.EscrowRelease)
	op(client, types.EscrowRelease)
	e.srv.checkEscrows()
	for range 3 {
		e.srv.deliverEscrowEvents()
	}
	for range 5 {
		e.srv.deliverEscrowEvents()
	}

	mu.Lock()
	got := strings.Join(events, ",")
	mu.Unlock()
	want := "escrow.funded,escrow.dispatched,escrow.milestone_released,escrow.milestone_released,escrow.completed"
	if got != want {
		t.Fatalf("webhooks:\n got  %s\n want %s", got, want)
	}
	acc, _ := e.c.Account(developer.addr)
	if acc.Balance != 100*types.Unit {
		t.Errorf("developer paid %d", acc.Balance)
	}

	// The app sees the escrow's live state; the public view has milestone labels.
	var view map[string]any
	e.call("GET", "/api/v1/escrows/"+id, nil, nil, key.APIKey, &view)
	if view["reference"] != "job-17" || view["escrow"].(map[string]any)["status"] != "completed" {
		t.Errorf("app view: %v", view)
	}
	var pub map[string]any
	e.call("GET", "/api/escrow-requests/"+id, nil, nil, "", &pub)
	if _, leaked := pub["reference"]; leaked || pub["milestones"].([]any)[1].(map[string]any)["label"] != "Site delivered" {
		t.Errorf("public view: %v", pub)
	}
}

func TestEscrowRequestExpires(t *testing.T) {
	e := newEnv(t)
	owner, dev := newWallet(t), newWallet(t)
	var app App
	e.call("POST", "/api/apps", map[string]string{"name": "Paynautik", "website": "https://paynautik.example", "contact_email": "a@b.c"}, &owner, "", &app)
	e.call("POST", "/api/apps/"+app.ID+"/review", map[string]string{"decision": "approve"}, &e.admin, "", nil)
	var key struct {
		APIKey string `json:"api_key"`
	}
	e.call("POST", "/api/apps/"+app.ID+"/keys", nil, &owner, "", &key)
	var req map[string]any
	e.call("POST", "/api/v1/escrows", map[string]any{"seller": string(dev.addr), "currency": "ORP",
		"milestones": []map[string]string{{"amount": "5"}}, "fund_within_hours": 1}, nil, key.APIKey, &req)
	if code := e.call("POST", "/api/v1/escrows", map[string]any{"seller": string(owner.addr), "currency": "ORP",
		"milestones": []map[string]string{{"amount": "5"}}}, nil, key.APIKey, nil); code != 400 {
		t.Errorf("seller who is also the arbiter: %d", code)
	}
	e.srv.now = func() time.Time { return time.Now().Add(2 * time.Hour) }
	e.srv.checkEscrows()
	if s := e.srv.escrowRequest(req["id"].(string)).Status; s != EscrowNoFund {
		t.Errorf("status %s, want expired", s)
	}
}
