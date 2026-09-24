package main

import (
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/openreserve/node/types"
)

func fakeSubscription(t *testing.T, endpoint string) map[string]any {
	k, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	auth := make([]byte, 16)
	rand.Read(auth)
	b64 := base64.RawURLEncoding.EncodeToString
	return map[string]any{"endpoint": endpoint, "keys": map[string]string{"p256dh": b64(k.PublicKey().Bytes()), "auth": b64(auth)}}
}

func TestPushNotifications(t *testing.T) {
	payer := newWallet(t)
	e := newEnv(t, payer)
	e.c.Clock = func() int64 { return time.Now().UnixMilli() }
	e.nowMs = time.Now().UnixMilli()
	me, arbiter := newWallet(t), newWallet(t)

	var mu sync.Mutex
	deliveries := 0
	gone := false
	svc := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if gone {
			w.WriteHeader(http.StatusGone)
			return
		}
		if r.Header.Get("Content-Encoding") != "aes128gcm" || !strings.HasPrefix(r.Header.Get("Authorization"), "vapid ") {
			t.Errorf("not a web push request: %v", r.Header)
		}
		deliveries++
		w.WriteHeader(http.StatusCreated)
	}))
	defer svc.Close()
	e.srv.hookClient = svc.Client() // trust the test push service's certificate

	var key map[string]string
	e.call("GET", "/api/push/key", nil, nil, "", &key)
	if key["public_key"] == "" {
		t.Fatal("no VAPID key")
	}
	// Local endpoints are only allowed because the test env permits private hooks.
	sub := fakeSubscription(t, svc.URL)
	if code := e.call("POST", "/api/push/subscribe", sub, &me, "", nil); code != 201 {
		t.Fatalf("subscribe: %d", code)
	}

	// The first scan only records state: no flood of old events.
	if n := e.srv.pushEvents(me.addr); len(n) != 0 {
		t.Fatalf("first scan sent %v", n)
	}
	e.pay(payer, me.addr, 7*types.Unit, "for the fabric")
	n := e.srv.pushEvents(me.addr)
	if len(n) != 1 || n[0].Title != "Money received" || !strings.Contains(n[0].Body, "7 ORP") {
		t.Fatalf("payment notification: %+v", n)
	}
	if again := e.srv.pushEvents(me.addr); len(again) != 0 {
		t.Fatalf("duplicate notification: %+v", again)
	}

	// Escrow: the arbiter hears about a dispute; the seller hears nothing new until something changes.
	eid := e.escrowTx(payer, &types.EscrowOp{Op: types.EscrowCreate, Seller: me.addr, Arbiter: arbiter.addr,
		Milestones: []types.Amount{3 * types.Unit}, ShipBy: time.Now().Add(48 * time.Hour).UnixMilli(), ReviewSecs: 86400}, 1000)
	e.srv.pushEvents(me.addr) // records the new escrow
	sub2 := fakeSubscription(t, "https://push.example.invalid/x")
	e.call("POST", "/api/push/subscribe", sub2, &arbiter, "", nil)
	e.srv.pushEvents(arbiter.addr)
	e.escrowTx(payer, &types.EscrowOp{Op: types.EscrowDispute, ID: types.MustHash(eid)}, 0)
	if n := e.srv.pushEvents(arbiter.addr); len(n) != 1 || !strings.Contains(n[0].Body, "needs your decision") {
		t.Fatalf("arbiter dispute notification: %+v", n)
	}
	if n := e.srv.pushEvents(me.addr); len(n) != 1 || !strings.Contains(n[0].Body, "dispute was opened") {
		t.Fatalf("seller dispute notification: %+v", n)
	}

	// Delivery goes through the push service; dead subscriptions are dropped.
	e.srv.notify(me.addr, notification{Title: "t", Body: "b", URL: "/"})
	mu.Lock()
	if deliveries != 1 {
		t.Errorf("deliveries %d", deliveries)
	}
	gone = true
	mu.Unlock()
	e.srv.notify(me.addr, notification{Title: "t", Body: "b", URL: "/"})
	var subs int
	e.srv.pushes.Read(func(m map[types.Address]*pushState) { subs = len(m[me.addr].Subs) })
	if subs != 0 {
		t.Errorf("gone subscription kept (%d)", subs)
	}
}
