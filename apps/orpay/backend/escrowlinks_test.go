package main

import (
	"crypto/ed25519"
	"encoding/hex"
	"testing"
)

func claimName(e *env, w wallet, name string) {
	sig := hex.EncodeToString(ed25519.Sign(w.priv, []byte(RegisterMessage(name, w.addr))))
	e.call("POST", "/api/users", map[string]any{"username": name, "address": w.addr, "sig": sig}, nil, "", nil)
}

func TestEscrowLinksAndArbiters(t *testing.T) {
	e := newEnv(t)
	seller, arb, buyer := newWallet(t), newWallet(t), newWallet(t)
	link := map[string]any{"currency": "ORP", "description": "iPhone 14", "kind": "goods",
		"milestones": []map[string]string{{"amount": "5"}}, "checklist": []string{"IMEI matches", "Battery health above 85%"}}

	if code := e.call("POST", "/api/escrow-links", link, &seller, "", nil); code != 400 {
		t.Fatalf("link without username: %d", code)
	}
	claimName(e, seller, "gadgethub")
	var out map[string]any
	if code := e.call("POST", "/api/escrow-links", link, &seller, "", &out); code != 201 {
		t.Fatalf("create link: %d", code)
	}
	// With no marketplace arbiters yet, an ORPay admin settles disputes.
	if out["arbiter"] != string(e.admin.addr) || len(out["checklist"].([]any)) != 2 {
		t.Fatalf("link: %v", out)
	}
	var mine []map[string]any
	e.call("GET", "/api/escrow-links", nil, &seller, "", &mine)
	if len(mine) != 1 {
		t.Fatalf("my links: %d", len(mine))
	}
	if code := e.call("POST", "/api/escrow-links/"+out["id"].(string)+"/cancel", nil, &buyer, "", nil); code != 404 {
		t.Errorf("stranger cancelled: %d", code)
	}

	// An arbiter applies, an admin approves; new links use them.
	claimName(e, arb, "fairjudge")
	if code := e.call("POST", "/api/arbiters/apply", map[string]string{"bio": "Ten years settling trade disputes in Lagos markets."}, &arb, "", nil); code != 200 {
		t.Fatalf("apply: %d", code)
	}
	var list []map[string]any
	e.call("GET", "/api/arbiters", nil, nil, "", &list)
	if len(list) != 0 {
		t.Fatalf("pending arbiter listed publicly: %v", list)
	}
	if code := e.call("POST", "/api/arbiters/"+string(arb.addr)+"/review", map[string]string{"decision": "approve"}, &seller, "", nil); code != 403 {
		t.Errorf("non-admin approved: %d", code)
	}
	e.call("POST", "/api/arbiters/"+string(arb.addr)+"/review", map[string]string{"decision": "approve"}, &e.admin, "", nil)
	e.call("GET", "/api/arbiters", nil, nil, "", &list)
	if len(list) != 1 || list[0]["username"] != "fairjudge" {
		t.Fatalf("arbiters: %v", list)
	}
	e.call("POST", "/api/escrow-links", link, &seller, "", &out)
	if out["arbiter"] != string(arb.addr) {
		t.Fatalf("new link arbiter %v, want marketplace arbiter", out["arbiter"])
	}
}
