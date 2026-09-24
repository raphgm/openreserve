// Command gateway connects OpenReserve to real naira through Paystack.
//
// It is the only holder of the NGN issuer key. Every NGN unit on-chain is
// minted only after Paystack confirms the matching naira was collected, and
// every withdrawal destroys NGN on-chain before naira is paid out:
//
//	Deposit:    Paystack checkout -> charge.success -> verify -> mint NGN to wallet
//	Withdrawal: user burns NGN (memo wd:<id>) -> Paystack transfer to bank
//	            -> transfer.success (done) | transfer.failed (NGN minted back)
//	Card pay:   partner checkout paid by card -> mint NGN to the merchant with
//	            memo inv:<id>, which the ORPay backend treats as payment
//
// Unit conversion: 1 kobo = 10,000 on-chain micro-units (6 decimals).
package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/openreserve/node/client"
	"github.com/openreserve/node/jsonstore"
	"github.com/openreserve/node/keys"
	"github.com/openreserve/node/ratelimit"
	"github.com/openreserve/node/reqauth"
	"github.com/openreserve/node/types"
)

const microPerKobo = 10_000

func toMicro(kobo int64) types.Amount { return types.Amount(kobo) * microPerKobo }
func toKobo(m types.Amount) int64     { return int64(m / microPerKobo) }

func main() {
	var (
		listen      = flag.String("listen", ":4100", "listen address")
		dataDir     = flag.String("data", "gateway-data", "directory for deposit/withdrawal records")
		nodeURL     = flag.String("node", "http://localhost:8080", "OpenReserve node API")
		backendURL  = flag.String("backend", "http://localhost:4000", "ORPay backend (for partner invoices)")
		publicURL   = flag.String("public-url", "http://localhost:5173", "public URL of the ORPay web app (Paystack returns here)")
		paystackAPI = flag.String("paystack-api", "https://api.paystack.co", "Paystack API base URL")
		asset       = flag.String("asset", "NGN", "on-chain asset this gateway issues")
		wdFee       = flag.String("withdraw-fee", "50", "flat fee in naira kept from each withdrawal (covers the Paystack transfer fee)")
		trustProxy  = flag.Bool("trust-proxy", false, "use X-Forwarded-For for client IPs (only behind a reverse proxy)")
	)
	flag.Parse()
	secret := os.Getenv("PAYSTACK_SECRET_KEY")
	if secret == "" {
		log.Fatal("set PAYSTACK_SECRET_KEY (sk_test_... for test mode)")
	}
	issuer, err := keys.Resolve("", "ORP_ISSUER_SEED")
	if err != nil || issuer == nil {
		log.Fatal("set ORP_ISSUER_SEED to the NGN issuer's seed (orctl export-seed)")
	}
	fee, err := types.ParseAmount(*wdFee)
	if err != nil {
		log.Fatal(err)
	}
	g, err := newGateway(config{
		dataDir: *dataDir, node: client.New(*nodeURL), backend: strings.TrimRight(*backendURL, "/"),
		publicURL: strings.TrimRight(*publicURL, "/"), ps: newPaystack(*paystackAPI, secret),
		issuer: issuer, asset: *asset, withdrawFee: fee,
	})
	if err != nil {
		log.Fatal(err)
	}
	if strings.HasPrefix(secret, "sk_test_") {
		log.Print("Paystack TEST mode: no real money moves")
	}
	go g.watch(3 * time.Second)
	log.Printf("gateway for %s listening on %s (issuer %s)", *asset, *listen, keys.Address(issuer))
	srv := &http.Server{Addr: *listen, Handler: ratelimit.Middleware(ratelimit.New(300, 60), *trustProxy, g.routes(*trustProxy)), ReadHeaderTimeout: 10 * time.Second}
	log.Fatal(srv.ListenAndServe())
}

type config struct {
	dataDir, backend, publicURL, asset string
	node                               *client.Client
	ps                                 *paystack
	issuer                             ed25519.PrivateKey
	withdrawFee                        types.Amount
}

type gateway struct {
	config
	deposits    *jsonstore.Store[map[string]*Deposit]
	withdrawals *jsonstore.Store[map[string]*Withdrawal]
	mintMu      sync.Mutex // one mint decision at a time: no double mints
	now         func() time.Time
	banksCache  struct {
		sync.Mutex
		list []psBank
		at   time.Time
	}
}

func newGateway(cfg config) (*gateway, error) {
	if err := os.MkdirAll(cfg.dataDir, 0o700); err != nil {
		return nil, err
	}
	d, err := jsonstore.Open(filepath.Join(cfg.dataDir, "deposits.json"), map[string]*Deposit{})
	if err != nil {
		return nil, err
	}
	w, err := jsonstore.Open(filepath.Join(cfg.dataDir, "withdrawals.json"), map[string]*Withdrawal{})
	if err != nil {
		return nil, err
	}
	return &gateway{config: cfg, deposits: d, withdrawals: w, now: time.Now}, nil
}

func (g *gateway) routes(trustProxy bool) http.Handler {
	strict := func(perMin, burst int, h http.HandlerFunc) http.Handler {
		return ratelimit.Middleware(ratelimit.New(perMin, burst), trustProxy, h)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /pay/config", g.configInfo)
	mux.Handle("POST /pay/deposits", strict(20, 5, reqauth.Signed(g.createDeposit)))
	mux.HandleFunc("GET /pay/deposits/{ref}", g.getDeposit)
	mux.HandleFunc("GET /pay/banks", g.listBanks)
	mux.Handle("POST /pay/banks/resolve", strict(30, 10, http.HandlerFunc(g.resolveBank)))
	mux.Handle("POST /pay/withdrawals", strict(10, 3, reqauth.Signed(g.createWithdrawal)))
	mux.HandleFunc("GET /pay/withdrawals", reqauth.Signed(g.listWithdrawals))
	mux.Handle("POST /pay/invoices/{id}/card", strict(20, 5, http.HandlerFunc(g.payInvoiceByCard)))
	mux.HandleFunc("POST /pay/webhooks/paystack", g.paystackWebhook)
	mux.HandleFunc("GET /pay/reserve", g.reserve)
	return cors(mux)
}

// ----- deposits -----

type Deposit struct {
	Reference string        `json:"reference"`
	Kind      string        `json:"kind"`    // "deposit" or "invoice"
	Address   types.Address `json:"address"` // who receives the minted NGN
	Invoice   string        `json:"invoice,omitempty"`
	Net       types.Amount  `json:"net"` // NGN to mint
	ChargedKb int64         `json:"charged_kobo"`
	Status    string        `json:"status"` // pending, minted, failed
	MintTx    string        `json:"mint_tx,omitempty"`
	Error     string        `json:"error,omitempty"`
	CreatedAt time.Time     `json:"created_at"`
	MintedAt  time.Time     `json:"minted_at,omitzero"`
}

var emailRe = regexp.MustCompile(`^[^@\s]+@[^@\s]+\.[^@\s]+$`)

func (g *gateway) configInfo(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"asset": g.asset, "issuer": keys.Address(g.issuer), "withdraw_fee": g.withdrawFee,
		"test_mode": strings.HasPrefix(g.ps.secret, "sk_test_"),
	})
}

// createDeposit starts a Paystack checkout that credits the caller's wallet.
func (g *gateway) createDeposit(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Amount string `json:"amount"` // naira, e.g. "5000"
		Email  string `json:"email"`
	}
	if err := decodeBody(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	net, err := types.ParseAmount(req.Amount)
	if err != nil || net < 100*types.Unit || net > 10_000_000*types.Unit || net%microPerKobo != 0 {
		writeErr(w, http.StatusBadRequest, errors.New("amount must be between ₦100 and ₦10,000,000, at most 2 decimals"))
		return
	}
	if !emailRe.MatchString(req.Email) {
		writeErr(w, http.StatusBadRequest, errors.New("enter a valid email for your Paystack receipt"))
		return
	}
	d := &Deposit{Reference: "dep_" + randHex(10), Kind: "deposit", Address: reqauth.Caller(r), Net: net, Status: "pending", CreatedAt: g.now()}
	g.startCheckout(w, d, req.Email, g.publicURL+"/?deposit="+d.Reference)
}

func (g *gateway) startCheckout(w http.ResponseWriter, d *Deposit, email, callback string) {
	d.ChargedKb = grossUp(toKobo(d.Net))
	init, err := g.ps.initialize(email, d.ChargedKb, d.Reference, callback, map[string]string{
		"kind": d.Kind, "address": string(d.Address), "invoice": d.Invoice,
	})
	if err != nil {
		writeErr(w, http.StatusBadGateway, err)
		return
	}
	if err := g.deposits.Update(func(m map[string]*Deposit) error { m[d.Reference] = d; return nil }); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"reference": d.Reference, "authorization_url": init.AuthorizationURL,
		"amount": d.Net, "charge_kobo": d.ChargedKb, "fee_kobo": d.ChargedKb - toKobo(d.Net),
	})
}

// getDeposit reports status; if still pending it asks Paystack directly, so
// a missed webhook never strands a payment.
func (g *gateway) getDeposit(w http.ResponseWriter, r *http.Request) {
	ref := r.PathValue("ref")
	d := g.deposit(ref)
	if d == nil {
		writeErr(w, http.StatusNotFound, errors.New("deposit not found"))
		return
	}
	if d.Status == "pending" {
		g.settleDeposit(ref)
		d = g.deposit(ref)
	}
	writeJSON(w, http.StatusOK, d)
}

func (g *gateway) deposit(ref string) *Deposit {
	var out *Deposit
	g.deposits.Read(func(m map[string]*Deposit) {
		if d, ok := m[ref]; ok {
			c := *d
			out = &c
		}
	})
	return out
}

// settleDeposit verifies a charge with Paystack and mints exactly once.
func (g *gateway) settleDeposit(ref string) {
	g.mintMu.Lock()
	defer g.mintMu.Unlock()
	d := g.deposit(ref)
	if d == nil || d.Status != "pending" {
		return
	}
	tx, err := g.ps.verify(ref)
	if err != nil {
		log.Printf("verify %s: %v", ref, err)
		return
	}
	switch tx.Status {
	case "success":
	case "failed", "abandoned", "reversed":
		if g.now().Sub(d.CreatedAt) > time.Hour || tx.Status != "abandoned" {
			g.setDeposit(ref, func(d *Deposit) { d.Status, d.Error = "failed", "payment "+tx.Status })
		}
		return
	default:
		return // ongoing, pending, queued...
	}
	// Never mint more than the naira actually received after Paystack fees.
	received := toMicro(tx.Amount - tx.Fees)
	if tx.Currency != "NGN" || tx.Reference != ref || received < d.Net {
		g.setDeposit(ref, func(d *Deposit) {
			d.Status, d.Error = "failed", fmt.Sprintf("amount mismatch: received %d kobo net, expected %d", tx.Amount-tx.Fees, toKobo(d.Net))
		})
		log.Printf("ALERT deposit %s: paystack says %+v, expected net %d", ref, tx, d.Net)
		return
	}
	memo := "paystack:" + ref
	if d.Kind == "invoice" {
		memo = "inv:" + d.Invoice
	}
	// Idempotency across crashes: if a mint for this reference already
	// landed on-chain, record it instead of minting again.
	if id := g.findMint(d.Address, ref); id != "" {
		g.setDeposit(ref, func(d *Deposit) { d.Status, d.MintTx, d.MintedAt = "minted", id, g.now() })
		return
	}
	id, err := g.node.SignAndSubmit(g.issuer, &types.Tx{Kind: types.KindMint, Asset: g.asset, To: d.Address, Amount: d.Net, Memo: memo + " ref:" + ref})
	if err != nil {
		log.Printf("mint %s: %v", ref, err)
		return
	}
	if err := g.node.WaitCommitted(id, 20*time.Second); err != nil {
		log.Printf("mint %s: %v", ref, err)
	}
	g.setDeposit(ref, func(d *Deposit) { d.Status, d.MintTx, d.MintedAt = "minted", id, g.now() })
	log.Printf("minted %s %s to %s for %s", types.FormatAmount(d.Net), g.asset, d.Address, ref)
}

func (g *gateway) findMint(to types.Address, ref string) string {
	h, err := g.node.History(to)
	if err != nil {
		return ""
	}
	for _, e := range h {
		if e.Tx.Kind == types.KindMint && e.Tx.From == keys.Address(g.issuer) && strings.HasSuffix(e.Tx.Memo, "ref:"+ref) {
			return e.ID
		}
	}
	return ""
}

func (g *gateway) setDeposit(ref string, fn func(*Deposit)) {
	g.deposits.Update(func(m map[string]*Deposit) error {
		if d, ok := m[ref]; ok {
			fn(d)
		}
		return nil
	})
}

// payInvoiceByCard lets a customer pay a partner app's NGN checkout by
// card/bank via Paystack; the NGN is minted straight to the merchant.
func (g *gateway) payInvoiceByCard(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Email string `json:"email"`
	}
	if err := decodeBody(r, &req); err != nil || !emailRe.MatchString(req.Email) {
		writeErr(w, http.StatusBadRequest, errors.New("enter a valid email for your receipt"))
		return
	}
	id := r.PathValue("id")
	var inv struct {
		ID       string        `json:"id"`
		Merchant types.Address `json:"merchant"`
		Amount   types.Amount  `json:"amount"`
		Asset    string        `json:"asset"`
		Status   string        `json:"status"`
	}
	resp, err := http.Get(g.backend + "/api/invoices/" + id)
	if err != nil {
		writeErr(w, http.StatusBadGateway, err)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 || json.NewDecoder(resp.Body).Decode(&inv) != nil {
		writeErr(w, http.StatusNotFound, errors.New("checkout not found"))
		return
	}
	if inv.Status != "pending" || inv.Asset != g.asset || inv.Amount%microPerKobo != 0 {
		writeErr(w, http.StatusBadRequest, errors.New("this checkout cannot be paid by card"))
		return
	}
	d := &Deposit{
		Reference: "inv_" + randHex(10), Kind: "invoice", Address: inv.Merchant, Invoice: inv.ID,
		Net: inv.Amount, Status: "pending", CreatedAt: g.now(),
	}
	g.startCheckout(w, d, req.Email, g.publicURL+"/?invoice="+inv.ID+"&card="+d.Reference)
}

// ----- withdrawals -----

type Withdrawal struct {
	ID            string        `json:"id"`
	Address       types.Address `json:"address"`
	Amount        types.Amount  `json:"amount"` // NGN burned
	Fee           types.Amount  `json:"fee"`
	PayoutKobo    int64         `json:"payout_kobo"`
	BankCode      string        `json:"bank_code"`
	BankName      string        `json:"bank_name"`
	AccountNumber string        `json:"account_number"`
	AccountName   string        `json:"account_name"`
	// awaiting_burn -> processing -> paid | refunded ; awaiting_burn -> expired
	Status    string    `json:"status"`
	BurnTx    string    `json:"burn_tx,omitempty"`
	RefundTx  string    `json:"refund_tx,omitempty"`
	Error     string    `json:"error,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (w *Withdrawal) Memo() string { return "wd:" + w.ID }

func (g *gateway) listBanks(w http.ResponseWriter, _ *http.Request) {
	g.banksCache.Lock()
	defer g.banksCache.Unlock()
	if g.banksCache.list == nil || g.now().Sub(g.banksCache.at) > 24*time.Hour {
		list, err := g.ps.banks()
		if err != nil {
			writeErr(w, http.StatusBadGateway, err)
			return
		}
		g.banksCache.list, g.banksCache.at = list, g.now()
	}
	writeJSON(w, http.StatusOK, g.banksCache.list)
}

var acctRe = regexp.MustCompile(`^\d{10}$`)

func (g *gateway) resolveBank(w http.ResponseWriter, r *http.Request) {
	var req struct {
		AccountNumber string `json:"account_number"`
		BankCode      string `json:"bank_code"`
	}
	if err := decodeBody(r, &req); err != nil || !acctRe.MatchString(req.AccountNumber) || req.BankCode == "" {
		writeErr(w, http.StatusBadRequest, errors.New("enter a 10-digit account number and choose a bank"))
		return
	}
	name, err := g.ps.resolveAccount(req.AccountNumber, req.BankCode)
	if err != nil {
		writeErr(w, http.StatusBadRequest, errors.New("could not verify that account"))
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"account_name": name})
}

// createWithdrawal quotes a payout and returns the memo the wallet must put
// on its burn tx. Naira is only paid once that burn is on-chain.
func (g *gateway) createWithdrawal(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Amount        string `json:"amount"`
		BankCode      string `json:"bank_code"`
		AccountNumber string `json:"account_number"`
	}
	if err := decodeBody(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	amt, err := types.ParseAmount(req.Amount)
	if err != nil || amt%microPerKobo != 0 || amt < g.withdrawFee+100*types.Unit {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("minimum withdrawal is ₦%s (includes ₦%s fee)", types.FormatAmount(g.withdrawFee+100*types.Unit), types.FormatAmount(g.withdrawFee)))
		return
	}
	if !acctRe.MatchString(req.AccountNumber) {
		writeErr(w, http.StatusBadRequest, errors.New("account number must be 10 digits"))
		return
	}
	name, err := g.ps.resolveAccount(req.AccountNumber, req.BankCode)
	if err != nil {
		writeErr(w, http.StatusBadRequest, errors.New("could not verify that bank account"))
		return
	}
	bankName := req.BankCode
	g.banksCache.Lock()
	for _, b := range g.banksCache.list {
		if b.Code == req.BankCode {
			bankName = b.Name
		}
	}
	g.banksCache.Unlock()
	wd := &Withdrawal{
		ID: randHex(10), Address: reqauth.Caller(r), Amount: amt, Fee: g.withdrawFee,
		PayoutKobo: toKobo(amt - g.withdrawFee), BankCode: req.BankCode, BankName: bankName,
		AccountNumber: req.AccountNumber, AccountName: name, Status: "awaiting_burn",
		CreatedAt: g.now(), UpdatedAt: g.now(),
	}
	if err := g.withdrawals.Update(func(m map[string]*Withdrawal) error { m[wd.ID] = wd; return nil }); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"withdrawal": wd, "burn": map[string]any{
		"kind": types.KindBurn, "asset": g.asset, "amount": wd.Amount, "memo": wd.Memo(),
	}})
}

func (g *gateway) listWithdrawals(w http.ResponseWriter, r *http.Request) {
	me := reqauth.Caller(r)
	out := []*Withdrawal{}
	g.withdrawals.Read(func(m map[string]*Withdrawal) {
		for _, wd := range m {
			if wd.Address == me {
				c := *wd
				out = append(out, &c)
			}
		}
	})
	writeJSON(w, http.StatusOK, out)
}

// processWithdrawals pays out withdrawals whose burn is on-chain. Burns are
// matched by memo, asset and exact amount; a burn for an expired quote is
// still honoured so user funds are never lost.
func (g *gateway) processWithdrawals() {
	var todo []Withdrawal
	g.withdrawals.Read(func(m map[string]*Withdrawal) {
		for _, wd := range m {
			if wd.Status == "awaiting_burn" || (wd.Status == "expired" && g.now().Sub(wd.CreatedAt) < 7*24*time.Hour) {
				todo = append(todo, *wd)
			}
		}
	})
	for _, wd := range todo {
		h, err := g.node.History(wd.Address)
		if err != nil {
			return
		}
		var burn string
		for _, e := range h {
			if e.Tx.Kind == types.KindBurn && e.Tx.From == wd.Address && e.Tx.Asset == g.asset && e.Tx.Memo == wd.Memo() && e.Tx.Amount == wd.Amount {
				burn = e.ID
			}
		}
		if burn == "" {
			if wd.Status == "awaiting_burn" && g.now().Sub(wd.CreatedAt) > 30*time.Minute {
				g.setWithdrawal(wd.ID, func(w *Withdrawal) { w.Status = "expired" })
			}
			continue
		}
		// Mark processing before calling Paystack so a crash can never pay twice;
		// the transfer reference is the withdrawal id, which Paystack dedupes.
		g.setWithdrawal(wd.ID, func(w *Withdrawal) { w.Status, w.BurnTx = "processing", burn })
		recipient, err := g.ps.createRecipient(wd.AccountName, wd.AccountNumber, wd.BankCode)
		if err == nil {
			var st string
			st, err = g.ps.transfer(wd.PayoutKobo, recipient, wd.ID, "ORPay withdrawal")
			if err == nil && st == "success" {
				g.setWithdrawal(wd.ID, func(w *Withdrawal) { w.Status = "paid" })
			} else if err == nil && st == "otp" {
				err = errors.New("Paystack requires OTP for transfers; disable it in the dashboard")
			}
		}
		if err != nil {
			log.Printf("withdrawal %s transfer: %v", wd.ID, err)
			// A timeout may hide a transfer that did go through: only refund
			// once Paystack confirms there is no live transfer for this id.
			if st, verr := g.ps.transferStatus(wd.ID); verr == nil && st != "failed" && st != "reversed" {
				continue // still processing; the transfer webhook settles it
			}
			g.refund(wd.ID, err.Error())
		}
	}
}

// refund mints the burned NGN back to the user when a payout fails.
func (g *gateway) refund(id, reason string) {
	g.mintMu.Lock()
	defer g.mintMu.Unlock()
	var wd Withdrawal
	g.withdrawals.Read(func(m map[string]*Withdrawal) { wd = *m[id] })
	if wd.Status == "refunded" || wd.Status == "paid" {
		return
	}
	ref := "refund-" + wd.ID
	txid := g.findMint(wd.Address, ref)
	if txid == "" {
		var err error
		txid, err = g.node.SignAndSubmit(g.issuer, &types.Tx{Kind: types.KindMint, Asset: g.asset, To: wd.Address, Amount: wd.Amount, Memo: "withdrawal refund ref:" + ref})
		if err != nil {
			log.Printf("ALERT refund %s failed: %v", wd.ID, err)
			g.setWithdrawal(id, func(w *Withdrawal) { w.Error = "refund pending: " + err.Error() })
			return
		}
	}
	g.setWithdrawal(id, func(w *Withdrawal) { w.Status, w.RefundTx, w.Error = "refunded", txid, reason })
	log.Printf("refunded withdrawal %s: %s", id, reason)
}

func (g *gateway) setWithdrawal(id string, fn func(*Withdrawal)) {
	g.withdrawals.Update(func(m map[string]*Withdrawal) error {
		if w, ok := m[id]; ok {
			fn(w)
			w.UpdatedAt = g.now()
		}
		return nil
	})
}

// ----- webhook, watcher, reserve -----

func (g *gateway) paystackWebhook(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
	if err != nil || !validSignature(g.ps.secret, body, r.Header.Get("x-paystack-signature")) {
		writeErr(w, http.StatusUnauthorized, errors.New("invalid signature"))
		return
	}
	var ev struct {
		Event string `json:"event"`
		Data  struct {
			Reference string `json:"reference"`
		} `json:"data"`
	}
	json.Unmarshal(body, &ev)
	w.WriteHeader(http.StatusOK) // acknowledge fast; work below is idempotent
	switch ev.Event {
	case "charge.success":
		go g.settleDeposit(ev.Data.Reference) // always re-verified with Paystack
	case "transfer.success":
		g.setWithdrawal(ev.Data.Reference, func(w *Withdrawal) {
			if w.Status == "processing" {
				w.Status = "paid"
			}
		})
	case "transfer.failed", "transfer.reversed":
		go g.refund(ev.Data.Reference, "bank transfer "+strings.TrimPrefix(ev.Event, "transfer."))
	}
}

func (g *gateway) watch(every time.Duration) {
	for range time.Tick(every) {
		var pending []string
		g.deposits.Read(func(m map[string]*Deposit) {
			for ref, d := range m {
				if d.Status == "pending" && g.now().Sub(d.CreatedAt) < 48*time.Hour {
					pending = append(pending, ref)
				}
			}
		})
		for _, ref := range pending {
			g.settleDeposit(ref)
		}
		g.processWithdrawals()
	}
}

// reserve compares NGN on-chain with naira held at Paystack. Supply must
// never exceed the reserve; pending payouts are naira already owed.
func (g *gateway) reserve(w http.ResponseWriter, _ *http.Request) {
	st, err := g.node.Status()
	if err != nil {
		writeErr(w, http.StatusBadGateway, err)
		return
	}
	var supply types.Amount
	for _, a := range st.Assets {
		if a.Symbol == g.asset {
			supply = a.Supply
		}
	}
	bal, err := g.ps.balanceNGN()
	if err != nil {
		writeErr(w, http.StatusBadGateway, err)
		return
	}
	var owed int64
	g.withdrawals.Read(func(m map[string]*Withdrawal) {
		for _, wd := range m {
			if wd.Status == "processing" {
				owed += wd.PayoutKobo
			}
		}
	})
	available := toMicro(bal - owed)
	coverage := 1.0
	if supply > 0 {
		coverage = float64(available) / float64(supply)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"asset": g.asset, "onchain_supply": supply, "paystack_balance_kobo": bal,
		"payouts_in_flight_kobo": owed, "reserve": available, "coverage": coverage,
		"fully_backed": available >= supply, "checked_at": g.now(),
	})
}

// ----- helpers -----

func decodeBody(r *http.Request, v any) error {
	dec := json.NewDecoder(http.MaxBytesReader(nil, r.Body, 16<<10))
	dec.DisallowUnknownFields()
	return dec.Decode(v)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, err error) {
	writeJSON(w, status, map[string]string{"error": err.Error()})
}

func cors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, X-ORP-Address, X-ORP-Time, X-ORP-Sig")
		if r.Method == http.MethodOptions {
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func randHex(n int) string {
	b := make([]byte, n)
	rand.Read(b)
	return hex.EncodeToString(b)
}
