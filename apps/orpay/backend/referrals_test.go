package main

import "testing"

func TestReferrals(t *testing.T) {
	ada, newbie := newWallet(t), newWallet(t)
	e := newEnv(t, ada)
	claimName(e, ada, "ada")
	if code := e.call("POST", "/api/referrals", map[string]string{"ref": "@ada"}, &ada, "", nil); code != 400 {
		t.Errorf("self-referral: %d", code)
	}
	if code := e.call("POST", "/api/referrals", map[string]string{"ref": "ada"}, &newbie, "", nil); code != 200 {
		t.Fatalf("refer: %d", code)
	}
	if code := e.call("POST", "/api/referrals", map[string]string{"ref": "ada"}, &newbie, "", nil); code != 409 {
		t.Errorf("second referral: %d", code)
	}
	var out struct {
		Code    string `json:"code"`
		Invited int    `json:"invited"`
		Active  int    `json:"active"`
	}
	e.call("GET", "/api/referrals", nil, &ada, "", &out)
	if out.Code != "ada" || out.Invited != 1 || out.Active != 0 {
		t.Fatalf("referrals: %+v", out)
	}
}

func TestRateSellerOnlyBuyer(t *testing.T) {
	e := newEnv(t)
	stranger := newWallet(t)
	if code := e.call("POST", "/api/escrows/"+string(make([]byte, 0))+"00/rate-seller", map[string]int{"stars": 5}, &stranger, "", nil); code != 404 {
		t.Errorf("rating a missing escrow: %d", code)
	}
}
