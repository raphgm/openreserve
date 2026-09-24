package main

import (
	"bytes"
	"crypto/ed25519"
	"crypto/hmac"
	"crypto/sha512"
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
	"github.com/openreserve/node/ledger"
	"github.com/openreserve/node/reqauth"
	"github.com/openreserve/node/types"
)

const secret = "sk_test_fake"

// fakePaystack mimics the parts of the Paystack API the gateway uses.
type fakePaystack struct {
	mu          sync.Mutex
	txs         map[string]*psTransaction
	transfers   map[string]string // reference -> status
	failPayouts bool
	balance     int64 // kobo
	paidOut     int64
}

func newFakePaystack() *fakePaystack {
	return &fakePaystack{txs: map[string]*psTransaction{}, transfers: map[string]string{}}
}

func (f *fakePaystack) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Authorization") != "Bearer "+secret {
		w.WriteHeader(401)
		json.NewEncoder(w).Encode(map[string]any{"status": false, "message": "Invalid key"})
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	ok := func(data any) {
		json.NewEncoder(w).Encode(map[string]any{"status": true, "message": "ok", "data": data})
	}
	fail := func(msg string) {
		w.WriteHeader(400)
		json.NewEncoder(w).Encode(map[string]any{"status": false, "message": msg})
	}
	var body map[string]any
	json.NewDecoder(r.Body).Decode(&body)
	p := r.URL.Path
	switch {
	case p == "/transaction/initialize":
		ref := body["reference"].(string)
		f.txs[ref] = &psTransaction{Status: "ongoing", Reference: ref, Amount: int64(body["amount"].(float64)), Currency: "NGN"}
		ok(map[string]any{"authorization_url": "https://checkout.paystack.test/" + ref, "reference": ref})
	case strings.HasPrefix(p, "/transaction/verify/"):
		tx := f.txs[strings.TrimPrefix(p, "/transaction/verify/")]
		if tx == nil {
			fail("Transaction reference not found")
			return
		}
		ok(tx)
	case p == "/bank":
		ok([]psBank{{Name: "Access Bank", Code: "044"}, {Name: "GTBank", Code: "058"}})
	case p == "/bank/resolve":
		if r.URL.Query().Get("account_number") != "0123456789" {
			fail("Could not resolve account name")
			return
		}
		ok(map[string]string{"account_name": "ADA OBI", "account_number": "0123456789"})
	case p == "/transferrecipient":
		ok(map[string]string{"recipient_code": "RCP_1"})
	case p == "/transfer":
		ref := body["reference"].(string)
		if f.failPayouts {
			f.transfers[ref] = "failed"
			fail("Insufficient balance")
			return
		}
		f.transfers[ref] = "success"
		amt := int64(body["amount"].(float64))
		f.balance -= amt
		f.paidOut += amt
		ok(map[string]string{"status": "success"})
	case strings.HasPrefix(p, "/transfer/verify/"):
		st, found := f.transfers[strings.TrimPrefix(p, "/transfer/verify/")]
		if !found {
			fail("Transfer not found")
			return
		}
		ok(map[string]string{"status": st})
	case p == "/balance":
		ok([]map[string]any{{"currency": "NGN", "balance": f.balance}})
	default:
		fail("not found: " + p)
	}
}

// pay simulates the customer completing checkout; Paystack keeps its fee.
func (f *fakePaystack) pay(ref string, fee int64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	tx := f.txs[ref]
	tx.Status, tx.Fees = "success", fee
	f.balance += tx.Amount - fee
}

type env struct {
	t        *testing.T
	c        *chain.Chain
	proposer ed25519.PrivateKey
	issuer   ed25519.PrivateKey
	ps       *fakePaystack
	g        *gateway
	srv      *httptest.Server
	nowMs    int64
	stop     chan struct{}
}

func newEnv(t *testing.T, backend string) *env {
	_, prop, _ := ed25519.GenerateKey(nil)
	_, issuer, _ := ed25519.GenerateKey(nil)
	e := &env{t: t, proposer: prop, issuer: issuer, ps: newFakePaystack(), nowMs: 1000, stop: make(chan struct{})}
	g := &chain.Genesis{
		ChainID: "test", Time: 1, Proposer: keys.Address(prop), MinFee: 1000,
		Assets: []ledger.AssetDef{{Symbol: "NGN", Name: "Naira", Issuer: keys.Address(issuer), MinFee: 10_000, Decimals: 2}},
	}
	c, err := chain.Open(t.TempDir(), g)
	if err != nil {
		t.Fatal(err)
	}
	e.c = c
	// Produce blocks continuously so the gateway's WaitCommitted returns.
	go func() {
		for {
			select {
			case <-e.stop:
				return
			case <-time.After(20 * time.Millisecond):
				e.nowMs += 1000
				c.Produce(prop, e.nowMs)
			}
		}
	}()
	t.Cleanup(func() { close(e.stop) })
	node := httptest.NewServer(api.Handler(c))
	pss := httptest.NewServer(e.ps)
	t.Cleanup(node.Close)
	t.Cleanup(pss.Close)
	e.g, err = newGateway(config{
		dataDir: t.TempDir(), node: client.New(node.URL), backend: backend, publicURL: "https://pay.test",
		providers: map[string]Provider{"paystack": newPaystack(pss.URL, secret)}, defaultProvider: "paystack",
		issuer: issuer, asset: "NGN", withdrawFee: 50 * types.Unit,
	})
	if err != nil {
		t.Fatal(err)
	}
	e.srv = httptest.NewServer(e.g.routes(false))
	t.Cleanup(e.srv.Close)
	return e
}

func (e *env) call(method, path string, body any, signer ed25519.PrivateKey, out any) int {
	e.t.Helper()
	var b []byte
	if body != nil {
		b, _ = json.Marshal(body)
	}
	req, _ := http.NewRequest(method, e.srv.URL+path, bytes.NewReader(b))
	if signer != nil {
		ts := time.Now().UnixMilli()
		req.Header.Set("X-ORP-Address", string(keys.Address(signer)))
		req.Header.Set("X-ORP-Time", strconv.FormatInt(ts, 10))
		req.Header.Set("X-ORP-Sig", hex.EncodeToString(ed25519.Sign(signer, []byte(reqauth.RequestMessage(method, path, ts, b)))))
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

func (e *env) webhook(event, ref string, sign bool) int {
	body, _ := json.Marshal(map[string]any{"event": event, "data": map[string]any{"reference": ref}})
	req, _ := http.NewRequest("POST", e.srv.URL+"/pay/webhooks/paystack", bytes.NewReader(body))
	mac := hmac.New(sha512.New, []byte(secret))
	mac.Write(body)
	sig := hex.EncodeToString(mac.Sum(nil))
	if !sign {
		sig = strings.Repeat("0", 128)
	}
	req.Header.Set("x-paystack-signature", sig)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	resp.Body.Close()
	return resp.StatusCode
}

func (e *env) ngn(a types.Address) types.Amount {
	acc, _ := e.c.Account(a)
	return acc.Assets["NGN"]
}

func (e *env) eventually(what string, cond func() bool) {
	e.t.Helper()
	for range 200 {
		if cond() {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	e.t.Fatalf("timed out waiting for %s", what)
}

func TestDepositMintsExactlyOnce(t *testing.T) {
	e := newEnv(t, "")
	_, user, _ := ed25519.GenerateKey(nil)
	addr := keys.Address(user)

	var dep struct {
		Reference string `json:"reference"`
		ChargeKb  int64  `json:"charge_kobo"`
		URL       string `json:"authorization_url"`
	}
	if code := e.call("POST", "/pay/deposits", map[string]string{"amount": "5000", "email": "ada@example.com"}, user, &dep); code != 201 {
		t.Fatalf("create deposit: %d", code)
	}
	if !strings.Contains(dep.URL, dep.Reference) || dep.ChargeKb <= 500_000 {
		t.Fatalf("deposit init: %+v", dep)
	}

	// A forged webhook is rejected; a real one before payment mints nothing.
	if code := e.webhook("charge.success", dep.Reference, false); code != 401 {
		t.Errorf("forged webhook: %d", code)
	}
	e.webhook("charge.success", dep.Reference, true)
	time.Sleep(100 * time.Millisecond)
	if e.ngn(addr) != 0 {
		t.Fatal("minted before payment")
	}

	// Customer pays; Paystack keeps its fee. Duplicate webhooks mint once.
	fee := dep.ChargeKb - 500_000
	e.ps.pay(dep.Reference, fee)
	e.webhook("charge.success", dep.Reference, true)
	e.webhook("charge.success", dep.Reference, true)
	e.eventually("mint", func() bool { return e.ngn(addr) == 5000*types.Unit })
	time.Sleep(150 * time.Millisecond)
	e.g.settleDeposit(dep.Reference)
	if got := e.ngn(addr); got != 5000*types.Unit {
		t.Fatalf("balance %d after duplicate webhooks", got)
	}
	var d Deposit
	e.call("GET", "/pay/deposits/"+dep.Reference, nil, nil, &d)
	if d.Status != "minted" || d.MintTx == "" {
		t.Errorf("deposit record: %+v", d)
	}

	// Reserve: naira held at Paystack covers the NGN in circulation.
	var res map[string]any
	e.call("GET", "/pay/reserve", nil, nil, &res)
	if res["fully_backed"] != true {
		t.Errorf("reserve: %v", res)
	}
}

func TestDepositUnderpaymentNotMinted(t *testing.T) {
	e := newEnv(t, "")
	_, user, _ := ed25519.GenerateKey(nil)
	var dep struct {
		Reference string `json:"reference"`
	}
	e.call("POST", "/pay/deposits", map[string]string{"amount": "5000", "email": "ada@example.com"}, user, &dep)
	// Paystack keeps a much bigger fee than expected: less than ₦5,000 arrives.
	e.ps.pay(dep.Reference, 200_000)
	e.g.settleDeposit(dep.Reference)
	if n := e.ngn(keys.Address(user)); n != 0 {
		t.Fatalf("minted %d despite underpayment", n)
	}
	if d := e.g.deposit(dep.Reference); d.Status != "failed" {
		t.Errorf("status %s", d.Status)
	}
}

func fund(t *testing.T, e *env, user ed25519.PrivateKey, naira string) {
	t.Helper()
	var dep struct {
		Reference string `json:"reference"`
		ChargeKb  int64  `json:"charge_kobo"`
	}
	e.call("POST", "/pay/deposits", map[string]string{"amount": naira, "email": "ada@example.com"}, user, &dep)
	amt, _ := types.ParseAmount(naira)
	e.ps.pay(dep.Reference, dep.ChargeKb-toKobo(amt))
	e.g.settleDeposit(dep.Reference)
}

func burn(t *testing.T, e *env, user ed25519.PrivateKey, amt types.Amount, memo string) {
	t.Helper()
	id, err := e.g.node.SignAndSubmit(user, &types.Tx{Kind: types.KindBurn, Asset: "NGN", Amount: amt, Memo: memo})
	if err != nil {
		t.Fatal(err)
	}
	if err := e.g.node.WaitCommitted(id, 5*time.Second); err != nil {
		t.Fatal(err)
	}
}

func TestWithdrawalPaidOut(t *testing.T) {
	e := newEnv(t, "")
	_, user, _ := ed25519.GenerateKey(nil)
	fund(t, e, user, "10000")

	if code := e.call("POST", "/pay/withdrawals", map[string]string{"amount": "3000", "bank_code": "058", "account_number": "9999999999"}, user, nil); code != 400 {
		t.Errorf("unresolvable account: %d", code)
	}
	var res struct {
		Withdrawal Withdrawal `json:"withdrawal"`
		Burn       struct {
			Memo   string       `json:"memo"`
			Amount types.Amount `json:"amount"`
		} `json:"burn"`
	}
	if code := e.call("POST", "/pay/withdrawals", map[string]string{"amount": "3000", "bank_code": "058", "account_number": "0123456789"}, user, &res); code != 201 {
		t.Fatalf("create withdrawal: %d", code)
	}
	if res.Withdrawal.AccountName != "ADA OBI" || res.Withdrawal.PayoutKobo != 295_000 {
		t.Fatalf("quote: %+v", res.Withdrawal)
	}

	// Nothing is paid until the burn lands on-chain.
	e.g.processWithdrawals()
	if e.ps.paidOut != 0 {
		t.Fatal("paid out before burn")
	}
	burn(t, e, user, res.Burn.Amount, res.Burn.Memo)
	e.g.processWithdrawals()
	e.g.processWithdrawals() // idempotent
	if e.ps.paidOut != 295_000 {
		t.Fatalf("paid out %d kobo, want 295000", e.ps.paidOut)
	}
	var list []Withdrawal
	e.call("GET", "/pay/withdrawals", nil, user, &list)
	if len(list) != 1 || list[0].Status != "paid" {
		t.Errorf("withdrawals: %+v", list)
	}
	if n := e.ngn(keys.Address(user)); n != 7000*types.Unit-10_000 {
		t.Errorf("NGN left %d", n)
	}
}

func TestFailedWithdrawalRefunded(t *testing.T) {
	e := newEnv(t, "")
	_, user, _ := ed25519.GenerateKey(nil)
	fund(t, e, user, "10000")
	e.ps.failPayouts = true
	var res struct {
		Withdrawal Withdrawal `json:"withdrawal"`
	}
	e.call("POST", "/pay/withdrawals", map[string]string{"amount": "2000", "bank_code": "058", "account_number": "0123456789"}, user, &res)
	burn(t, e, user, 2000*types.Unit, res.Withdrawal.Memo())
	before := e.ngn(keys.Address(user))
	e.g.processWithdrawals()
	e.eventually("refund", func() bool { return e.ngn(keys.Address(user)) == before+2000*types.Unit })
	e.g.refund(res.Withdrawal.ID, "again") // must not refund twice
	time.Sleep(100 * time.Millisecond)
	if n := e.ngn(keys.Address(user)); n != before+2000*types.Unit {
		t.Errorf("double refund: %d", n)
	}
}

func TestCardPaysPartnerInvoice(t *testing.T) {
	_, merchant, _ := ed25519.GenerateKey(nil)
	status := "pending"
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"id": "inv123", "merchant": keys.Address(merchant), "amount": 7_250_000_000, "asset": "NGN", "status": status,
		})
	}))
	defer backend.Close()
	e := newEnv(t, backend.URL)

	var res struct {
		Reference string `json:"reference"`
		ChargeKb  int64  `json:"charge_kobo"`
	}
	if code := e.call("POST", "/pay/invoices/inv123/card", map[string]string{"email": "rider@example.com"}, nil, &res); code != 201 {
		t.Fatalf("card checkout: %d", code)
	}
	e.ps.pay(res.Reference, res.ChargeKb-725_000)
	e.g.settleDeposit(res.Reference)
	if n := e.ngn(keys.Address(merchant)); n != 7250*types.Unit {
		t.Fatalf("merchant NGN %d", n)
	}
	h, _ := e.g.node.History(keys.Address(merchant))
	if len(h) != 1 || !strings.HasPrefix(h[0].Tx.Memo, "inv:inv123 ") {
		t.Errorf("merchant history: %+v", h)
	}
	status = "paid"
	if code := e.call("POST", "/pay/invoices/inv123/card", map[string]string{"email": "rider@example.com"}, nil, nil); code != 400 {
		t.Errorf("paying a paid invoice: %d", code)
	}
}

func TestGrossUp(t *testing.T) {
	for _, net := range []int64{10_000, 249_999, 500_000, 10_000_000, 1_000_000_000} {
		gross := grossUp(net)
		fee := gross * 15 / 1000
		if gross >= 250_000 {
			fee += 10_000
		}
		if fee > 200_000 {
			fee = 200_000
		}
		if gross-fee < net {
			t.Errorf("net %d: gross %d leaves %d after fee %d", net, gross, gross-fee, fee)
		}
	}
}

// fakeFlutterwave mimics the parts of the Flutterwave v3 API the adapter uses.
type fakeFlutterwave struct {
	mu        sync.Mutex
	txs       map[string]map[string]any
	transfers map[string]string
	balance   float64
}

func (f *fakeFlutterwave) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Authorization") != "Bearer FLWSECK_TEST-fake" {
		w.WriteHeader(401)
		json.NewEncoder(w).Encode(map[string]any{"status": "error", "message": "Invalid authorization key"})
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	ok := func(data any) {
		json.NewEncoder(w).Encode(map[string]any{"status": "success", "message": "ok", "data": data})
	}
	fail := func(msg string) {
		w.WriteHeader(400)
		json.NewEncoder(w).Encode(map[string]any{"status": "error", "message": msg})
	}
	var body map[string]any
	json.NewDecoder(r.Body).Decode(&body)
	switch p := r.URL.Path; {
	case p == "/payments":
		ref := body["tx_ref"].(string)
		f.txs[ref] = map[string]any{"status": "pending", "tx_ref": ref, "amount": body["amount"], "app_fee": 0.0, "currency": "NGN"}
		ok(map[string]string{"link": "https://checkout.flutterwave.test/" + ref})
	case p == "/transactions/verify_by_reference":
		tx := f.txs[r.URL.Query().Get("tx_ref")]
		if tx == nil {
			fail("No transaction was found for this id")
			return
		}
		ok(tx)
	case p == "/banks/NG":
		ok([]Bank{{Name: "Kuda Bank", Code: "50211"}})
	case p == "/accounts/resolve":
		ok(map[string]string{"account_name": "TUNDE ADE"})
	case p == "/transfers" && r.Method == "POST":
		f.transfers[body["reference"].(string)] = "NEW" // Flutterwave settles transfers asynchronously
		f.balance -= body["amount"].(float64)
		ok(map[string]string{"status": "NEW"})
	case p == "/transfers":
		st, found := f.transfers[r.URL.Query().Get("reference")]
		if !found {
			ok([]any{})
			return
		}
		ok([]map[string]string{{"status": st}})
	case p == "/balances/NGN":
		ok(map[string]float64{"available_balance": f.balance})
	default:
		fail("not found " + p)
	}
}

func TestFlutterwaveProvider(t *testing.T) {
	e := newEnv(t, "")
	fw := &fakeFlutterwave{txs: map[string]map[string]any{}, transfers: map[string]string{}}
	fws := httptest.NewServer(fw)
	defer fws.Close()
	e.g.providers["flutterwave"] = newFlutterwave(fws.URL, "FLWSECK_TEST-fake", "hash123")
	_, user, _ := ed25519.GenerateKey(nil)

	var cfg struct {
		Providers []string `json:"providers"`
	}
	e.call("GET", "/pay/config", nil, nil, &cfg)
	if len(cfg.Providers) != 2 {
		t.Fatalf("providers: %v", cfg.Providers)
	}

	// Deposit through Flutterwave.
	var dep struct {
		Reference string `json:"reference"`
		Provider  string `json:"provider"`
		ChargeKb  int64  `json:"charge_kobo"`
	}
	if code := e.call("POST", "/pay/deposits", map[string]string{"amount": "8000", "email": "t@example.com", "provider": "flutterwave"}, user, &dep); code != 201 || dep.Provider != "flutterwave" {
		t.Fatalf("deposit: %d %+v", code, dep)
	}
	fee := float64(dep.ChargeKb-800_000) / 100
	fw.mu.Lock()
	fw.txs[dep.Reference]["status"], fw.txs[dep.Reference]["app_fee"] = "successful", fee
	fw.balance += float64(dep.ChargeKb)/100 - fee
	fw.mu.Unlock()

	// Webhooks need the dashboard secret hash; a wrong one is refused.
	post := func(hash string, body map[string]any) int {
		b, _ := json.Marshal(body)
		req, _ := http.NewRequest("POST", e.srv.URL+"/pay/webhooks/flutterwave", bytes.NewReader(b))
		req.Header.Set("verif-hash", hash)
		resp, _ := http.DefaultClient.Do(req)
		resp.Body.Close()
		return resp.StatusCode
	}
	ev := map[string]any{"event": "charge.completed", "data": map[string]any{"tx_ref": dep.Reference, "status": "successful"}}
	if code := post("wrong", ev); code != 401 {
		t.Errorf("bad verif-hash: %d", code)
	}
	post("hash123", ev)
	e.eventually("flutterwave mint", func() bool { return e.ngn(keys.Address(user)) == 8000*types.Unit })

	// Withdraw through Flutterwave: payout is asynchronous, settled by webhook.
	var res struct {
		Withdrawal Withdrawal `json:"withdrawal"`
	}
	if code := e.call("POST", "/pay/withdrawals", map[string]string{"amount": "3000", "bank_code": "50211", "account_number": "0123456789", "provider": "flutterwave"}, user, &res); code != 201 {
		t.Fatalf("withdrawal: %d", code)
	}
	if res.Withdrawal.AccountName != "TUNDE ADE" || res.Withdrawal.BankName != "Kuda Bank" {
		t.Errorf("quote: %+v", res.Withdrawal)
	}
	burn(t, e, user, 3000*types.Unit, res.Withdrawal.Memo())
	e.g.processWithdrawals()
	var list []Withdrawal
	e.call("GET", "/pay/withdrawals", nil, user, &list)
	if list[0].Status != "processing" {
		t.Fatalf("after payout request: %s", list[0].Status)
	}
	post("hash123", map[string]any{"event": "transfer.completed", "data": map[string]any{"reference": res.Withdrawal.ID, "status": "SUCCESSFUL"}})
	e.eventually("payout settled", func() bool {
		var l []Withdrawal
		e.call("GET", "/pay/withdrawals", nil, user, &l)
		return l[0].Status == "paid"
	})

	// Reserve sums every provider.
	var reserve map[string]any
	e.call("GET", "/pay/reserve", nil, nil, &reserve)
	if reserve["fully_backed"] != true || len(reserve["provider_balances_kobo"].(map[string]any)) != 2 {
		t.Errorf("reserve: %v", reserve)
	}
}
