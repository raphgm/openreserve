package main

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/openreserve/node/client"
)

func TestCourierWebhook(t *testing.T) {
	e := newEnv(t)
	buyer := newWallet(t)
	t.Setenv("COURIER_GIG_SECRET", "s3cret")
	e.srv.escrows.Update(func(m map[string]*EscrowRequest) error {
		m["esr_x"] = &EscrowRequest{ID: "esr_x", EscrowID: "abc", Status: EscrowLinked,
			Chain: &client.Escrow{ID: "abc", Buyer: buyer.addr, Status: "dispatched", Tracking: "GIG Logistics · GL12345"}}
		return nil
	})
	post := func(body, sig string) int {
		req := httptest.NewRequest(http.MethodPost, "/api/couriers/gig/webhook", bytes.NewBufferString(body))
		req.SetPathValue("name", "gig")
		req.Header.Set("X-Courier-Signature", sig)
		w := httptest.NewRecorder()
		e.srv.courierWebhook(w, req)
		return w.Code
	}
	body := `{"tracking":"GL12345","status":"DELIVERED","detail":"Signed by Ada"}`
	if code := post(body, "deadbeef"); code != http.StatusUnauthorized {
		t.Fatalf("unsigned update accepted: %d", code)
	}
	mac := hmac.New(sha256.New, []byte("s3cret"))
	mac.Write([]byte(body))
	if code := post(body, hex.EncodeToString(mac.Sum(nil))); code != 200 {
		t.Fatalf("signed update: %d", code)
	}
	if d := e.srv.escrowRequest("esr_x").Delivery; d == nil || d.State != "delivered" || d.Courier != "GIG Logistics" {
		t.Fatalf("delivery: %+v", d)
	}
}

func TestNormaliseState(t *testing.T) {
	for in, want := range map[string]string{"delivered": "delivered", "transit": "in_transit", "pre-transit": "pre_transit", "OUT_FOR_DELIVERY": "out_for_delivery", "failure": "failed"} {
		if got := normaliseState(in); got != want {
			t.Errorf("%s: %s, want %s", in, got, want)
		}
	}
}
