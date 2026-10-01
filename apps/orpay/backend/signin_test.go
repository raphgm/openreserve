package main

import (
	"crypto/ed25519"
	"encoding/hex"
	"strings"
	"testing"
	"time"
)

func TestSixWordSignIn(t *testing.T) {
	alice := newWallet(t)
	e := newEnv(t, alice)
	data := map[string]string{"salt": strings.Repeat("ab", 16), "iv": strings.Repeat("cd", 12), "ct": strings.Repeat("ef", 48), "auth": strings.Repeat("12", 32)}

	// Needs a username first.
	if code := e.call("POST", "/api/signin", data, &alice, "", nil); code != 400 {
		t.Fatalf("without username: %d", code)
	}
	sig := hex.EncodeToString(ed25519.Sign(alice.priv, []byte(RegisterMessage("alice", alice.addr))))
	e.call("POST", "/api/users", map[string]any{"username": "alice", "address": alice.addr, "sig": sig}, nil, "", nil)
	if code := e.call("POST", "/api/signin", data, &alice, "", nil); code != 200 {
		t.Fatalf("set: %d", code)
	}
	var salt map[string]string
	e.call("POST", "/api/signin/salt", map[string]string{"username": "@Alice"}, nil, "", &salt)
	if salt["salt"] != data["salt"] {
		t.Fatalf("salt: %v", salt)
	}
	open := func(auth string) (int, map[string]string) {
		var out map[string]string
		code := e.call("POST", "/api/signin/open", map[string]string{"username": "alice", "auth": auth}, nil, "", &out)
		return code, out
	}
	if code, out := open(data["auth"]); code != 200 || out["ct"] != data["ct"] || out["address"] != string(alice.addr) {
		t.Fatalf("open: %d %v", code, out)
	}
	// Five wrong guesses lock it, even for the right words.
	wrong := strings.Repeat("99", 32)
	for i := 0; i < 5; i++ {
		if code, _ := open(wrong); code != 401 {
			t.Fatalf("wrong guess %d: %d", i, code)
		}
	}
	if code, _ := open(data["auth"]); code != 429 {
		t.Fatalf("locked account opened: %d", code)
	}
	e.srv.now = func() time.Time { return time.Now().Add(20 * time.Minute) }
	if code, _ := open(data["auth"]); code != 200 {
		t.Fatalf("after lock expired: %d", code)
	}
	// Turning it off.
	e.call("DELETE", "/api/signin", nil, &alice, "", nil)
	var st map[string]bool
	e.call("GET", "/api/signin", nil, &alice, "", &st)
	if st["enabled"] {
		t.Fatal("still enabled after delete")
	}
}
