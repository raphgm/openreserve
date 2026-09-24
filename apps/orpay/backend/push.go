package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	webpush "github.com/SherClockHolmes/webpush-go"

	"github.com/openreserve/node/client"
	"github.com/openreserve/node/reqauth"
	"github.com/openreserve/node/types"
)

// Push notifications reach people even when ORPay is closed: money
// received, ajo payments due, the pot ready to collect, escrow progress and
// new escrow messages. Standard Web Push (VAPID); on iPhone it works for
// ORPay added to the Home Screen.

type vapidKeys struct {
	Public  string `json:"public"`
	Private string `json:"private"`
}

type pushState struct {
	Subs    []webpush.Subscription `json:"subs"`
	LastTx  string                 `json:"last_tx,omitempty"` // newest history entry already seen
	Sent    map[string]int64       `json:"sent,omitempty"`    // dedupe key -> unix time sent
	Escrows map[string]string      `json:"escrows,omitempty"` // escrow id -> last seen "status/released/dispatched"
	Primed  bool                   `json:"primed,omitempty"`  // first scan only records state
}

type notification struct {
	Title string `json:"title"`
	Body  string `json:"body"`
	URL   string `json:"url"`
	Tag   string `json:"tag,omitempty"`
}

func loadVAPID(dir string) (vapidKeys, error) {
	p := filepath.Join(dir, "orpay-vapid.json")
	var k vapidKeys
	if b, err := os.ReadFile(p); err == nil && json.Unmarshal(b, &k) == nil && k.Public != "" {
		return k, nil
	}
	priv, pub, err := webpush.GenerateVAPIDKeys()
	if err != nil {
		return k, err
	}
	k = vapidKeys{Public: pub, Private: priv}
	b, _ := json.Marshal(k)
	return k, os.WriteFile(p, b, 0o600)
}

func (s *server) pushKey(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"public_key": s.vapid.Public})
}

func (s *server) pushSubscribe(w http.ResponseWriter, r *http.Request) {
	var sub webpush.Subscription
	if err := decodeBody(r, &sub); err != nil || sub.Endpoint == "" || sub.Keys.P256dh == "" || sub.Keys.Auth == "" {
		writeErr(w, http.StatusBadRequest, errors.New("invalid push subscription"))
		return
	}
	if err := validURL(sub.Endpoint, true); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	me := reqauth.Caller(r)
	err := s.pushes.Update(func(m map[types.Address]*pushState) error {
		st := m[me]
		if st == nil {
			st = &pushState{}
			m[me] = st
		}
		st.Subs = slices.DeleteFunc(st.Subs, func(x webpush.Subscription) bool { return x.Endpoint == sub.Endpoint })
		st.Subs = append(st.Subs, sub)
		if len(st.Subs) > 5 { // a few devices per person
			st.Subs = st.Subs[len(st.Subs)-5:]
		}
		return nil
	})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]bool{"subscribed": true})
}

func (s *server) pushUnsubscribe(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Endpoint string `json:"endpoint"`
	}
	decodeBody(r, &req)
	me := reqauth.Caller(r)
	s.pushes.Update(func(m map[types.Address]*pushState) error {
		if st := m[me]; st != nil {
			st.Subs = slices.DeleteFunc(st.Subs, func(x webpush.Subscription) bool { return req.Endpoint == "" || x.Endpoint == req.Endpoint })
		}
		return nil
	})
	writeJSON(w, http.StatusOK, map[string]bool{"subscribed": false})
}

// notify sends n to every device of addr, dropping subscriptions the push
// service says are gone.
func (s *server) notify(addr types.Address, n notification) {
	var subs []webpush.Subscription
	s.pushes.Read(func(m map[types.Address]*pushState) {
		if st := m[addr]; st != nil {
			subs = slices.Clone(st.Subs)
		}
	})
	payload, _ := json.Marshal(n)
	var gone []string
	for _, sub := range subs {
		resp, err := webpush.SendNotification(payload, &sub, &webpush.Options{
			Subscriber: s.publicURL, VAPIDPublicKey: s.vapid.Public, VAPIDPrivateKey: s.vapid.Private,
			TTL: 24 * 3600, Urgency: webpush.UrgencyNormal, HTTPClient: s.hookClient,
		})
		if err != nil {
			continue
		}
		resp.Body.Close()
		if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusGone {
			gone = append(gone, sub.Endpoint)
		}
	}
	if len(gone) > 0 {
		s.pushes.Update(func(m map[types.Address]*pushState) error {
			if st := m[addr]; st != nil {
				st.Subs = slices.DeleteFunc(st.Subs, func(x webpush.Subscription) bool { return slices.Contains(gone, x.Endpoint) })
			}
			return nil
		})
	}
}

// watchPush scans subscribed wallets for things worth telling them.
func (s *server) watchPush(every time.Duration) {
	for range time.Tick(every) {
		var addrs []types.Address
		s.pushes.Read(func(m map[types.Address]*pushState) {
			for a, st := range m {
				if len(st.Subs) > 0 {
					addrs = append(addrs, a)
				}
			}
		})
		for _, a := range addrs {
			for _, n := range s.pushEvents(a) {
				s.notify(a, n)
			}
		}
	}
}

func (s *server) nameOrShort(a types.Address) string {
	if n, ok := s.dir.byAddress(a); ok {
		return "@" + n
	}
	return string(a)[:6] + "…"
}

func money(amt types.Amount, asset string) string {
	if asset == "NGN" {
		return "₦" + types.FormatAmount(amt)
	}
	return types.FormatAmount(amt) + " " + types.AssetName(asset)
}

// pushEvents works out new notifications for one wallet and records what
// it has seen, so each event is sent once.
func (s *server) pushEvents(a types.Address) []notification {
	hist, err := s.node.History(a)
	if err != nil {
		return nil
	}
	var pools []struct {
		ID           string          `json:"id"`
		Name         string          `json:"name"`
		Asset        string          `json:"asset"`
		Members      []types.Address `json:"members"`
		Contribution types.Amount    `json:"contribution"`
		Status       string          `json:"status"`
		Round        int             `json:"round"`
		Paid         []bool          `json:"paid"`
		RoundSecs    int64           `json:"round_secs"`
		StartedAt    int64           `json:"started_at"`
	}
	s.node.GetJSON("/v1/accounts/"+string(a)+"/pools", &pools)
	escrows, _ := s.node.EscrowsOf(a)

	var out []notification
	now := s.now()
	s.pushes.Update(func(m map[types.Address]*pushState) error {
		st := m[a]
		if st == nil {
			return nil
		}
		if st.Sent == nil {
			st.Sent = map[string]int64{}
		}
		if st.Escrows == nil {
			st.Escrows = map[string]string{}
		}
		once := func(key string, n notification) {
			if _, done := st.Sent[key]; done {
				return
			}
			st.Sent[key] = now.Unix()
			if st.Primed {
				out = append(out, n)
			}
		}

		// Money in: new committed txs paying this wallet.
		for _, e := range hist {
			if e.ID == st.LastTx {
				break
			}
			if st.Primed && e.Tx.To == a && e.Tx.Pool == nil && e.Tx.Escrow == nil {
				from := s.nameOrShort(e.Tx.From)
				if e.Tx.Kind == types.KindMint {
					from = "your top-up"
				}
				out = append(out, notification{Title: "Money received", Body: fmt.Sprintf("%s from %s", money(e.Tx.Amount, e.Tx.Asset), from), URL: "/", Tag: "rx-" + e.ID})
			}
		}
		if len(hist) > 0 {
			st.LastTx = hist[0].ID
		}

		// Ajo: payment due within a day, or your pot ready.
		for _, p := range pools {
			i := slices.Index(p.Members, a)
			if i < 0 || p.Status != "active" {
				continue
			}
			url := "/?pool=" + p.ID
			if !p.Paid[i] && p.RoundSecs > 0 {
				due := time.UnixMilli(p.StartedAt + int64(p.Round+1)*p.RoundSecs*1000)
				if due.Sub(now) < 24*time.Hour {
					when := "is due " + due.Format("Mon 2 Jan")
					if due.Before(now) {
						when = "is overdue"
					}
					once(fmt.Sprintf("due-%s-%d", p.ID, p.Round), notification{Title: p.Name, Body: fmt.Sprintf("Your %s ajo payment %s.", money(p.Contribution, p.Asset), when), URL: url, Tag: "ajo-" + p.ID})
				}
			}
			if i == p.Round && !slices.Contains(p.Paid, false) {
				once(fmt.Sprintf("pot-%s-%d", p.ID, p.Round), notification{Title: p.Name, Body: "Everyone has paid. Your ajo pot is ready to collect.", URL: url, Tag: "ajo-" + p.ID})
			}
		}

		// Escrow progress.
		for _, e := range escrows {
			key := fmt.Sprintf("%s/%d/%d", e.Status, e.Released, e.DispatchedAt)
			prev, seen := st.Escrows[e.ID]
			st.Escrows[e.ID] = key
			if !seen || prev == key || !st.Primed {
				continue
			}
			url := "/?escrow=" + e.ID
			title := "Escrow"
			if e.Ref != "" && !strings.HasPrefix(e.Ref, "esr_") {
				title = "Escrow #" + e.Ref
			}
			var body string
			switch {
			case e.Status == "disputed" && a == e.Arbiter:
				body = "A dispute needs your decision."
			case e.Status == "disputed":
				body = "A dispute was opened. The arbiter will decide."
			case e.Status == "dispatched" && a == e.Buyer && !strings.HasPrefix(prev, "dispatched"):
				body = "The seller marked your order as dispatched. Inspect it on arrival before releasing payment."
			case e.Status == "completed" && a == e.Seller:
				body = "All funds were released to you."
			case e.Status == "refunded" && a == e.Buyer:
				body = "Your funds were returned."
			case e.Status == "resolved":
				body = fmt.Sprintf("The arbiter decided: %s to the seller, %s back to the buyer.", money(e.PaidSeller, e.Asset), money(e.PaidBuyer, e.Asset))
			case a == e.Seller:
				body = "The buyer released a milestone payment to you."
			default:
				continue
			}
			out = append(out, notification{Title: title, Body: body, URL: url, Tag: "escrow-" + e.ID})
		}
		// Forget dedupe keys after a month.
		for k, t := range st.Sent {
			if now.Unix()-t > 30*24*3600 {
				delete(st.Sent, k)
			}
		}
		st.Primed = true
		return nil
	})
	return out
}

// notifyEscrowParties tells the other parties about a new escrow message.
func (s *server) notifyEscrowParties(e *client.Escrow, from types.Address, text string) {
	if len(text) > 120 {
		text = text[:117] + "…"
	}
	if text == "" {
		text = "sent a photo"
	}
	for _, a := range []types.Address{e.Buyer, e.Seller, e.Arbiter} {
		if a != from {
			go s.notify(a, notification{Title: "New escrow message", Body: s.nameOrShort(from) + ": " + text, URL: "/?escrow=" + e.ID, Tag: "chat-" + e.ID})
		}
	}
}
