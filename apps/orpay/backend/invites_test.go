package main

import (
	"testing"

	"github.com/openreserve/node/types"
)

func TestAjoInviteFlow(t *testing.T) {
	org, ada, tunde := newWallet(t), newWallet(t), newWallet(t)
	e := newEnv(t, org, ada, tunde)
	var d map[string]any
	if code := e.call("POST", "/api/ajo-invites", map[string]any{"name": "Office ajo", "contribution": 5 * types.Unit, "slots": 3}, &org, "", &d); code != 201 {
		t.Fatalf("create: %d", code)
	}
	id := d["id"].(string)
	e.call("POST", "/api/ajo-invites/"+id+"/join", nil, &ada, "", nil)
	e.call("POST", "/api/ajo-invites/"+id+"/join", nil, &tunde, "", &d)
	if n := len(d["members"].([]any)); n != 3 {
		t.Fatalf("members %d", n)
	}
	outsider := newWallet(t)
	if code := e.call("POST", "/api/ajo-invites/"+id+"/join", nil, &outsider, "", nil); code != 400 {
		t.Errorf("join when full: %d", code)
	}
	// Only the organiser can reorder, and only with the same members.
	order := []types.Address{org.addr, tunde.addr, ada.addr}
	if code := e.call("POST", "/api/ajo-invites/"+id+"/order", map[string]any{"members": order}, &ada, "", nil); code != 400 {
		t.Errorf("non-organiser reorder: %d", code)
	}
	if code := e.call("POST", "/api/ajo-invites/"+id+"/order", map[string]any{"members": []types.Address{org.addr, tunde.addr, outsider.addr}}, &org, "", nil); code != 400 {
		t.Errorf("reorder with a stranger: %d", code)
	}
	e.call("POST", "/api/ajo-invites/"+id+"/order", map[string]any{"members": order}, &org, "", nil)

	// A pool that doesn't match the invite is refused; the matching one is recorded.
	create := func(members []types.Address) string {
		_, next := e.c.Account(org.addr)
		tx := &types.Tx{ChainID: "test", From: org.addr, Fee: 1000, Nonce: next, Pool: &types.PoolOp{Op: types.PoolCreate, Name: "Office ajo", Members: members, Contribution: 5 * types.Unit}}
		tx.Sign(org.priv)
		if _, err := e.c.Submit(tx); err != nil {
			t.Fatal(err)
		}
		e.block()
		return tx.ID().String()
	}
	wrong := create([]types.Address{org.addr, ada.addr, tunde.addr})
	if code := e.call("POST", "/api/ajo-invites/"+id+"/started", map[string]string{"pool_id": wrong}, &org, "", nil); code != 400 {
		t.Errorf("mismatched pool accepted: %d", code)
	}
	right := create(order)
	e.call("POST", "/api/ajo-invites/"+id+"/started", map[string]string{"pool_id": right}, &org, "", &d)
	if d["status"] != DraftStarted || d["pool_id"] != right {
		t.Fatalf("after start: %v", d)
	}
	if code := e.call("POST", "/api/ajo-invites/"+id+"/join", nil, &outsider, "", nil); code != 400 {
		t.Errorf("join after start: %d", code)
	}
}
