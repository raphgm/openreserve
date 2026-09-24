package main

import (
	"testing"

	"github.com/openreserve/node/types"
)

func TestPhoneRecoveryRequest(t *testing.T) {
	alice, g1, g2 := newWallet(t), newWallet(t), newWallet(t)
	e := newEnv(t, alice, g1, g2)

	// Link a phone number with an SMS code.
	var start map[string]any
	if code := e.call("POST", "/api/phone/link", map[string]string{"phone": "0803 123 4567"}, &alice, "", &start); code != 201 {
		t.Fatalf("link start: %d", code)
	}
	if code := e.call("POST", "/api/phone/link/verify", map[string]string{"otp_id": start["otp_id"].(string), "code": "000000x"}, &alice, "", nil); code != 400 {
		t.Errorf("wrong code accepted: %d", code)
	}
	if code := e.call("POST", "/api/phone/link/verify", map[string]string{"otp_id": start["otp_id"].(string), "code": start["dev_code"].(string)}, &alice, "", nil); code != 200 {
		t.Fatalf("link verify: %d", code)
	}
	var found map[string]any
	e.call("GET", "/api/phones/+2348031234567", nil, nil, "", &found)
	if found["address"] != string(alice.addr) {
		t.Fatalf("resolve: %v", found)
	}

	// A new phone proves the number; without guardians, recovery is refused.
	newPhone := newWallet(t)
	recover := func() int {
		var s map[string]any
		e.call("POST", "/api/recovery/start", map[string]string{"phone": "+2348031234567"}, nil, "", &s)
		return e.call("POST", "/api/recovery/verify", map[string]any{"otp_id": s["otp_id"], "code": s["dev_code"], "new_owner": newPhone.addr}, nil, "", nil)
	}
	if code := recover(); code != 409 {
		t.Errorf("recovery without guardians: %d", code)
	}
	_, next := e.c.Account(alice.addr)
	tx := &types.Tx{ChainID: "test", From: alice.addr, Fee: 1000, Nonce: next, Guard: &types.GuardOp{Op: types.GuardSet, Guardians: []types.Address{g1.addr, g2.addr}, Threshold: 2, DelaySecs: 3600}}
	tx.Sign(alice.priv)
	if _, err := e.c.Submit(tx); err != nil {
		t.Fatal(err)
	}
	e.block()
	if code := recover(); code != 201 {
		t.Fatalf("recovery request: %d", code)
	}
	var reqs []map[string]any
	e.call("GET", "/api/recovery/requests", nil, &g1, "", &reqs)
	if len(reqs) != 1 || reqs[0]["new_owner"] != string(newPhone.addr) {
		t.Fatalf("guardian requests: %v", reqs)
	}
	// Migration is refused until the chain shows the recovery finished.
	if code := e.call("POST", "/api/recovery/migrate", map[string]any{"old": alice.addr}, &newPhone, "", nil); code != 403 {
		t.Errorf("early migrate: %d", code)
	}
}
