package main

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"image/color"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/openreserve/node/types"
)

func TestEscrowTermsRequiredForFunding(t *testing.T) {
	buyer := newWallet(t)
	e := newEnv(t, buyer)
	e.c.Clock = func() int64 { return time.Now().UnixMilli() }
	e.nowMs = time.Now().UnixMilli()
	owner, seller := newWallet(t), newWallet(t)
	var app App
	e.call("POST", "/api/apps", map[string]string{"name": "Paynautik", "website": "https://paynautik.example", "contact_email": "a@b.c"}, &owner, "", &app)
	e.call("POST", "/api/apps/"+app.ID+"/review", map[string]string{"decision": "approve"}, &e.admin, "", nil)
	policy := "No returns. Inspect the item on delivery. Disputes only for items not as described."
	e.call("POST", "/api/apps/"+app.ID+"/settings", map[string]string{"escrow_policy": policy}, &owner, "", nil)
	var key struct {
		APIKey string `json:"api_key"`
	}
	e.call("POST", "/api/apps/"+app.ID+"/keys", nil, &owner, "", &key)

	var req map[string]any
	e.call("POST", "/api/v1/escrows", map[string]any{"seller": string(seller.addr), "currency": "ORP",
		"milestones": []map[string]string{{"label": "Phone", "amount": "50"}}}, nil, key.APIKey, &req)
	if req["policy"] != policy || req["terms_memo"] != "terms:"+termsHash(policy) {
		t.Fatalf("request terms: %v", req)
	}
	var terms map[string]any
	e.call("GET", "/api/escrow-terms/"+termsHash(policy), nil, nil, "", &terms)
	if terms["text"] != policy {
		t.Fatalf("stored terms: %v", terms)
	}

	id := req["id"].(string)
	fund := func(memo string) string {
		_, next := e.c.Account(buyer.addr)
		tx := &types.Tx{ChainID: "test", From: buyer.addr, Fee: 1000, Nonce: next, Memo: memo, Escrow: &types.EscrowOp{
			Op: types.EscrowCreate, Seller: seller.addr, Arbiter: owner.addr, Milestones: []types.Amount{50 * types.Unit},
			ShipBy: time.Now().Add(14 * 24 * time.Hour).UnixMilli(), ReviewSecs: 3 * 86400, Ref: id}}
		tx.Sign(buyer.priv)
		if _, err := e.c.Submit(tx); err != nil {
			t.Fatal(err)
		}
		e.block()
		return tx.ID().String()
	}
	fund("") // buyer did not accept the terms
	e.srv.checkEscrows()
	if e.srv.escrowRequest(id).Status != EscrowAwaiting {
		t.Fatal("linked an escrow funded without accepting the terms")
	}
	escrowID := fund("terms:" + termsHash(policy))
	e.srv.checkEscrows()
	if got := e.srv.escrowRequest(id); got.EscrowID != escrowID {
		t.Fatalf("escrow with accepted terms not linked: %+v", got)
	}
}

func TestEscrowChatAndEvidence(t *testing.T) {
	buyer := newWallet(t)
	e := newEnv(t, buyer)
	e.c.Clock = func() int64 { return time.Now().UnixMilli() }
	e.nowMs = time.Now().UnixMilli()
	seller, arbiter, outsider := newWallet(t), newWallet(t), newWallet(t)
	id := e.escrowTx(buyer, &types.EscrowOp{Op: types.EscrowCreate, Seller: seller.addr, Arbiter: arbiter.addr,
		Milestones: []types.Amount{10 * types.Unit}, ShipBy: time.Now().Add(48 * time.Hour).UnixMilli(), ReviewSecs: 86400}, 1000)
	path := "/api/escrows/" + id + "/messages"

	photo := pngBytes(t, color.RGBA{10, 200, 30, 255})
	var msg EscrowMessage
	if code := e.call("POST", path, map[string]any{"text": "Here is the phone on arrival: screen cracked.",
		"photos": []string{base64.StdEncoding.EncodeToString(photo)}}, &buyer, "", &msg); code != 201 {
		t.Fatalf("buyer post: %d", code)
	}
	sum := sha256.Sum256(photo)
	if msg.Role != "buyer" || len(msg.Files) != 1 || msg.Files[0].SHA256 != hex.EncodeToString(sum[:]) {
		t.Fatalf("message: %+v", msg)
	}
	e.call("POST", path, map[string]any{"text": "It left our store intact, see the dispatch photo."}, &seller, "", nil)

	// Outsiders can neither read nor post; fake images are refused.
	if code := e.call("GET", path, nil, &outsider, "", nil); code != 403 {
		t.Errorf("outsider read: %d", code)
	}
	if code := e.call("POST", path, map[string]any{"text": "hi"}, &outsider, "", nil); code != 403 {
		t.Errorf("outsider post: %d", code)
	}
	if code := e.call("POST", path, map[string]any{"photos": []string{base64.StdEncoding.EncodeToString([]byte("<svg onload=alert(1)>"))}}, &buyer, "", nil); code != 400 {
		t.Errorf("fake photo: %d", code)
	}

	// The arbiter sees the whole thread and can open the evidence.
	var thread []EscrowMessage
	e.call("GET", path, nil, &arbiter, "", &thread)
	if len(thread) != 2 || thread[1].Role != "seller" {
		t.Fatalf("thread: %+v", thread)
	}
	url := thread[0].Files[0].URL
	resp, _ := http.Get(e.api.URL + url)
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 || sha256.Sum256(body) != sum {
		t.Fatalf("evidence download: %d", resp.StatusCode)
	}
	// Tampered or expired links fail.
	if resp, _ := http.Get(e.api.URL + strings.Replace(url, "sig=", "sig=00", 1)); resp.StatusCode != 403 {
		t.Errorf("forged link: %d", resp.StatusCode)
	}
	e.srv.now = func() time.Time { return time.Now().Add(2 * time.Hour) }
	if resp, _ := http.Get(e.api.URL + url); resp.StatusCode != 403 {
		t.Errorf("expired link: %d", resp.StatusCode)
	}
}
