package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

// Courier tracking. When a seller marks an escrow dispatched with a courier
// and tracking number ("DHL · 1234567890"), ORPay follows the parcel and
// tells the buyer the moment it is delivered, so they inspect it and
// release (or dispute) within the review period.
//
// Two ways in:
//   - Polling a courier's public tracking API. DHL is built in: set
//     DHL_API_KEY (free key from developer.dhl.com).
//   - A signed webhook any courier or aggregator can call (GIG, Kwik, Bolt
//     Send...): POST /api/couriers/{name}/webhook with
//     {"tracking": "...", "status": "delivered", "detail": "..."} and header
//     X-Courier-Signature: hex(HMAC-SHA256(COURIER_<NAME>_SECRET, body)).

type Delivery struct {
	Courier string    `json:"courier"`
	Ref     string    `json:"tracking"`
	State   string    `json:"state"` // pre_transit, in_transit, out_for_delivery, delivered, failed
	Detail  string    `json:"detail,omitempty"`
	At      time.Time `json:"at"`
}

type tracker interface {
	Track(ref string) (state, detail string, err error)
}

type dhlTracker struct {
	key  string
	http *http.Client
}

func (d dhlTracker) Track(ref string) (string, string, error) {
	req, _ := http.NewRequest(http.MethodGet, "https://api-eu.dhl.com/track/shipments?trackingNumber="+url.QueryEscape(ref), nil)
	req.Header.Set("DHL-API-Key", d.key)
	resp, err := d.http.Do(req)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("DHL returned %s", resp.Status)
	}
	var body struct {
		Shipments []struct {
			Status struct {
				StatusCode  string `json:"statusCode"`
				Description string `json:"description"`
			} `json:"status"`
		} `json:"shipments"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&body); err != nil || len(body.Shipments) == 0 {
		return "", "", errors.New("no DHL shipment with that number")
	}
	st := body.Shipments[0].Status
	return normaliseState(st.StatusCode), st.Description, nil
}

func normaliseState(s string) string {
	s = strings.ToLower(strings.ReplaceAll(s, "-", "_"))
	switch {
	case strings.Contains(s, "deliver") && !strings.Contains(s, "out"):
		return "delivered"
	case strings.Contains(s, "out_for") || strings.Contains(s, "out for"):
		return "out_for_delivery"
	case strings.Contains(s, "fail") || strings.Contains(s, "return"):
		return "failed"
	case strings.Contains(s, "pre"):
		return "pre_transit"
	default:
		return "in_transit"
	}
}

// trackers lists couriers ORPay can poll, by the name sellers pick.
func trackersFromEnv(c *http.Client) map[string]tracker {
	t := map[string]tracker{}
	if k := os.Getenv("DHL_API_KEY"); k != "" {
		t["DHL"] = dhlTracker{key: k, http: c}
	}
	return t
}

// splitTracking reads "Courier · REF" from the on-chain delivery note.
func splitTracking(note string) (courier, ref string) {
	if c, r, ok := strings.Cut(note, " · "); ok {
		return c, strings.TrimSpace(r)
	}
	return "", strings.TrimSpace(note)
}

// recordDelivery stores a courier update on the matching escrow and tells
// both sides when the parcel arrives.
func (s *server) recordDelivery(courier, ref, state, detail string) bool {
	var hit *EscrowRequest
	var changed bool
	s.escrows.Update(func(m map[string]*EscrowRequest) error {
		for _, er := range m {
			if er.Chain == nil {
				continue
			}
			c, r := splitTracking(er.Chain.Tracking)
			if r == "" || r != ref || !strings.EqualFold(c, courier) {
				continue
			}
			if er.Delivery == nil || er.Delivery.State != state {
				changed = true
			}
			er.Delivery = &Delivery{Courier: c, Ref: r, State: state, Detail: detail, At: s.now()}
			x := *er
			hit = &x
			return nil
		}
		return nil
	})
	if hit == nil {
		return false
	}
	if changed && state == "delivered" {
		go s.notify(hit.Chain.Buyer, notification{Title: "Your order was delivered", Body: "Inspect it now, then release payment or open a dispute before the review period ends.", URL: "/?escrow=" + hit.EscrowID, Tag: "escrow-" + hit.EscrowID})
		go s.notify(hit.Seller, notification{Title: "Delivered", Body: courier + " confirmed delivery. The buyer can now inspect and release.", URL: "/?escrow=" + hit.EscrowID, Tag: "escrow-" + hit.EscrowID})
	}
	return true
}

// courierWebhook accepts signed status updates from couriers.
func (s *server) courierWebhook(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	secret := os.Getenv("COURIER_" + strings.ToUpper(strings.ReplaceAll(name, " ", "_")) + "_SECRET")
	if secret == "" {
		writeErr(w, http.StatusNotFound, errors.New("unknown courier"))
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 64<<10))
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	if !hmac.Equal([]byte(hex.EncodeToString(mac.Sum(nil))), []byte(r.Header.Get("X-Courier-Signature"))) {
		writeErr(w, http.StatusUnauthorized, errors.New("bad signature"))
		return
	}
	var ev struct {
		Tracking, Status, Detail string
	}
	if err := json.Unmarshal(body, &ev); err != nil || ev.Tracking == "" {
		writeErr(w, http.StatusBadRequest, errors.New("send tracking and status"))
		return
	}
	courier := map[string]string{"gig": "GIG Logistics", "kwik": "Kwik", "dhl": "DHL", "bolt": "Bolt Send"}[strings.ToLower(name)]
	if courier == "" {
		courier = name
	}
	matched := s.recordDelivery(courier, ev.Tracking, normaliseState(ev.Status), ev.Detail)
	writeJSON(w, http.StatusOK, map[string]bool{"matched": matched})
}

// watchDeliveries polls couriers with an API for dispatched escrows.
func (s *server) watchDeliveries(every time.Duration) {
	for range time.Tick(every) {
		if len(s.trackers) == 0 {
			continue
		}
		type job struct{ courier, ref string }
		var jobs []job
		s.escrows.Read(func(m map[string]*EscrowRequest) {
			for _, er := range m {
				if er.Chain == nil || er.Chain.Status != "dispatched" || (er.Delivery != nil && er.Delivery.State == "delivered") {
					continue
				}
				if c, r := splitTracking(er.Chain.Tracking); r != "" && s.trackers[c] != nil {
					jobs = append(jobs, job{c, r})
				}
			}
		})
		for _, j := range jobs {
			state, detail, err := s.trackers[j.courier].Track(j.ref)
			if err != nil {
				log.Printf("tracking %s %s: %v", j.courier, j.ref, err)
				continue
			}
			s.recordDelivery(j.courier, j.ref, state, detail)
		}
	}
}
