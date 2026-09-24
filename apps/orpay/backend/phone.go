package main

import (
	"bytes"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math/big"
	"net/http"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/openreserve/node/reqauth"
	"github.com/openreserve/node/types"
)

// Phone numbers make wallets easy to find and to recover:
//
//   - A wallet links its phone number (verified by SMS code).
//   - On a new phone, the person proves the number by SMS code, the new
//     device makes a new key, and ORPay asks their guardians (push
//     notification) to approve moving the wallet to it. Approvals happen
//     on-chain; ORPay never holds keys or funds.
//   - After recovery the username and phone follow the wallet to its new key.

// SMSSender delivers one text message.
type SMSSender interface {
	Send(phone, text string) error
}

// devSMS logs codes instead of sending them (development only).
type devSMS struct{}

func (devSMS) Send(phone, text string) error {
	log.Printf("DEV SMS to %s: %s", phone, text)
	return nil
}

// termiiSMS sends through Termii (https://developers.termii.com), common in Nigeria.
type termiiSMS struct {
	apiKey, sender string
	http           *http.Client
}

func (t termiiSMS) Send(phone, text string) error {
	body, _ := json.Marshal(map[string]string{
		"to": strings.TrimPrefix(phone, "+"), "from": t.sender, "sms": text,
		"type": "plain", "channel": "generic", "api_key": t.apiKey,
	})
	resp, err := t.http.Post("https://api.ng.termii.com/api/sms/send", "application/json", bytes.NewReader(body))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("SMS provider returned %s", resp.Status)
	}
	return nil
}

var phoneRe = regexp.MustCompile(`^\+[1-9]\d{9,14}$`)

// normalizePhone accepts Nigerian local numbers (0803...) and E.164.
func normalizePhone(p string) (string, error) {
	p = strings.Map(func(r rune) rune {
		if r == ' ' || r == '-' || r == '(' || r == ')' {
			return -1
		}
		return r
	}, strings.TrimSpace(p))
	if strings.HasPrefix(p, "0") && len(p) == 11 {
		p = "+234" + p[1:]
	}
	if strings.HasPrefix(p, "234") {
		p = "+" + p
	}
	if !phoneRe.MatchString(p) {
		return "", errors.New("enter a phone number like 0803 123 4567 or +2348031234567")
	}
	return p, nil
}

func maskPhone(p string) string {
	if len(p) < 8 {
		return p
	}
	return p[:4] + strings.Repeat("•", len(p)-8) + p[len(p)-4:]
}

type otp struct {
	Phone    string        `json:"phone"`
	Purpose  string        `json:"purpose"` // "link" or "recover"
	Address  types.Address `json:"address,omitempty"`
	CodeHash string        `json:"code_hash"`
	Expires  time.Time     `json:"expires"`
	Tries    int           `json:"tries"`
}

type recoveryRequest struct {
	ID        string        `json:"id"`
	Account   types.Address `json:"account"`
	NewOwner  types.Address `json:"new_owner"`
	Phone     string        `json:"phone"`
	CreatedAt time.Time     `json:"created_at"`
}

type phoneData struct {
	Phones   map[string]types.Address    `json:"phones"` // E.164 -> address
	OTPs     map[string]*otp             `json:"otps"`
	Requests map[string]*recoveryRequest `json:"requests"`
	Sent     map[string][]time.Time      `json:"sent"` // phone -> recent sends (rate limit)
}

func (s *server) codeHash(code string) string {
	mac := hmac.New(sha256.New, s.secret)
	mac.Write([]byte("otp|" + code))
	return hex.EncodeToString(mac.Sum(nil))
}

// startPhone sends a code. "link" needs a signed request from the wallet;
// "recover" is for a new device that has no key for the account yet.
func (s *server) startPhone(w http.ResponseWriter, r *http.Request, purpose string, addr types.Address) {
	var req struct {
		Phone string `json:"phone"`
	}
	if err := decodeBody(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	phone, err := normalizePhone(req.Phone)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if purpose == "recover" {
		var known bool
		s.phones.Read(func(d *phoneData) { _, known = d.Phones[phone] })
		if !known {
			writeErr(w, http.StatusNotFound, errors.New("no wallet is linked to this number"))
			return
		}
	}
	n, _ := rand.Int(rand.Reader, big.NewInt(1_000_000))
	code := fmt.Sprintf("%06d", n.Int64())
	id := randToken("otp_", 10)
	err = s.phones.Update(func(d *phoneData) error {
		now := s.now()
		recent := slices.DeleteFunc(d.Sent[phone], func(t time.Time) bool { return now.Sub(t) > time.Hour })
		if len(recent) >= 5 {
			return errors.New("too many codes sent to this number; try again in an hour")
		}
		d.Sent[phone] = append(recent, now)
		for k, o := range d.OTPs { // drop expired codes
			if now.After(o.Expires) {
				delete(d.OTPs, k)
			}
		}
		d.OTPs[id] = &otp{Phone: phone, Purpose: purpose, Address: addr, CodeHash: s.codeHash(code), Expires: now.Add(10 * time.Minute)}
		return nil
	})
	if err != nil {
		writeErr(w, http.StatusTooManyRequests, err)
		return
	}
	if err := s.sms.Send(phone, "Your ORPay code is "+code+". It expires in 10 minutes. Never share it."); err != nil {
		writeErr(w, http.StatusBadGateway, fmt.Errorf("could not send SMS: %v", err))
		return
	}
	resp := map[string]any{"otp_id": id, "phone": maskPhone(phone)}
	if s.devOTP {
		resp["dev_code"] = code // development only: no SMS provider configured
	}
	writeJSON(w, http.StatusCreated, resp)
}

func (s *server) startLink(w http.ResponseWriter, r *http.Request) {
	s.startPhone(w, r, "link", reqauth.Caller(r))
}

func (s *server) startRecover(w http.ResponseWriter, r *http.Request) {
	s.startPhone(w, r, "recover", "")
}

// checkOTP verifies and consumes a code.
func (s *server) checkOTP(id, code, purpose string) (*otp, error) {
	var out *otp
	err := s.phones.Update(func(d *phoneData) error {
		o := d.OTPs[id]
		if o == nil || o.Purpose != purpose || s.now().After(o.Expires) {
			return errors.New("this code has expired; request a new one")
		}
		o.Tries++
		if o.Tries > 5 {
			delete(d.OTPs, id)
			return errors.New("too many wrong codes; request a new one")
		}
		if !hmac.Equal([]byte(o.CodeHash), []byte(s.codeHash(strings.TrimSpace(code)))) {
			return errors.New("wrong code")
		}
		delete(d.OTPs, id)
		c := *o
		out = &c
		return nil
	})
	return out, err
}

func (s *server) verifyLink(w http.ResponseWriter, r *http.Request) {
	var req struct {
		OTP  string `json:"otp_id"`
		Code string `json:"code"`
	}
	if err := decodeBody(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	o, err := s.checkOTP(req.OTP, req.Code, "link")
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	me := reqauth.Caller(r)
	if o.Address != me {
		writeErr(w, http.StatusForbidden, errors.New("this code was for a different wallet"))
		return
	}
	err = s.phones.Update(func(d *phoneData) error {
		if owner, ok := d.Phones[o.Phone]; ok && owner != me {
			return errors.New("this number is linked to another wallet")
		}
		for p, a := range d.Phones { // one number per wallet
			if a == me {
				delete(d.Phones, p)
			}
		}
		d.Phones[o.Phone] = me
		return nil
	})
	if err != nil {
		writeErr(w, http.StatusConflict, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"phone": maskPhone(o.Phone)})
}

// verifyRecover proves the number on a new device and asks the wallet's
// guardians to approve moving it to the new key.
func (s *server) verifyRecover(w http.ResponseWriter, r *http.Request) {
	var req struct {
		OTP      string        `json:"otp_id"`
		Code     string        `json:"code"`
		NewOwner types.Address `json:"new_owner"`
	}
	if err := decodeBody(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if err := req.NewOwner.Validate(); err != nil {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("new key: %w", err))
		return
	}
	o, err := s.checkOTP(req.OTP, req.Code, "recover")
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	var account types.Address
	s.phones.Read(func(d *phoneData) { account = d.Phones[o.Phone] })
	var rec struct {
		Guardianship *struct {
			Guardians []types.Address `json:"guardians"`
			Threshold int             `json:"threshold"`
			DelaySecs int64           `json:"delay_secs"`
		} `json:"guardianship"`
	}
	if err := s.node.GetJSON("/v1/accounts/"+string(account)+"/recovery", &rec); err != nil || rec.Guardianship == nil {
		writeErr(w, http.StatusConflict, errors.New("this wallet has no guardians set up, so it cannot be recovered this way"))
		return
	}
	rr := &recoveryRequest{ID: randToken("rec_", 8), Account: account, NewOwner: req.NewOwner, Phone: o.Phone, CreatedAt: s.now()}
	s.phones.Update(func(d *phoneData) error { d.Requests[rr.ID] = rr; return nil })

	name := s.nameOrShort(account)
	for _, g := range rec.Guardianship.Guardians {
		go s.notify(g, notification{
			Title: "Wallet recovery request",
			Body:  fmt.Sprintf("%s (%s) is asking you to restore their wallet on a new phone. Call them to confirm before approving.", name, maskPhone(o.Phone)),
			URL:   "/?recovery=1", Tag: "recover-" + string(account),
		})
	}
	go s.notify(account, notification{
		Title: "Someone is recovering your wallet",
		Body:  "A recovery to a new phone was requested with your number. If this wasn't you, open ORPay and cancel it.",
		URL:   "/?recovery=1", Tag: "recover-" + string(account),
	})
	writeJSON(w, http.StatusCreated, map[string]any{
		"account": account, "new_owner": req.NewOwner, "guardians": rec.Guardianship.Guardians,
		"threshold": rec.Guardianship.Threshold, "delay_secs": rec.Guardianship.DelaySecs,
	})
}

// guardianRequests lists recovery requests for accounts the caller guards.
func (s *server) guardianRequests(w http.ResponseWriter, r *http.Request) {
	me := reqauth.Caller(r)
	var rec struct {
		Guarding []types.Address `json:"guarding"`
	}
	s.node.GetJSON("/v1/accounts/"+string(me)+"/recovery", &rec)
	out := []map[string]any{}
	s.phones.Read(func(d *phoneData) {
		for _, rr := range d.Requests {
			if slices.Contains(rec.Guarding, rr.Account) && s.now().Sub(rr.CreatedAt) < 14*24*time.Hour {
				out = append(out, map[string]any{"id": rr.ID, "account": rr.Account, "new_owner": rr.NewOwner,
					"phone": maskPhone(rr.Phone), "created_at": rr.CreatedAt, "name": s.nameOrShort(rr.Account)})
			}
		}
	})
	writeJSON(w, http.StatusOK, out)
}

// resolvePhone finds the wallet for a phone number (e.g. to pay someone).
func (s *server) resolvePhone(w http.ResponseWriter, r *http.Request) {
	phone, err := normalizePhone(r.PathValue("phone"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	var addr types.Address
	s.phones.Read(func(d *phoneData) { addr = d.Phones[phone] })
	if addr == "" {
		writeErr(w, http.StatusNotFound, errors.New("no ORPay wallet uses this number"))
		return
	}
	name, _ := s.dir.byAddress(addr)
	writeJSON(w, http.StatusOK, map[string]any{"address": addr, "username": name})
}

// migrateIdentity moves username and phone to a wallet's new key once the
// chain shows the recovery finished.
func (s *server) migrateIdentity(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Old types.Address `json:"old"`
	}
	if err := decodeBody(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	me := reqauth.Caller(r)
	var rec struct {
		RecoveredTo types.Address `json:"recovered_to"`
	}
	if err := s.node.GetJSON("/v1/accounts/"+string(req.Old)+"/recovery", &rec); err != nil || rec.RecoveredTo != me {
		writeErr(w, http.StatusForbidden, errors.New("the chain does not show this wallet recovered to you"))
		return
	}
	s.phones.Update(func(d *phoneData) error {
		for p, a := range d.Phones {
			if a == req.Old {
				d.Phones[p] = me
			}
		}
		return nil
	})
	var moved string
	s.dir.store.Update(func(users map[string]types.Address) error {
		if name, ok := s.dir.byAddr[req.Old]; ok {
			if _, has := s.dir.byAddr[me]; !has {
				users[name] = me
				delete(s.dir.byAddr, req.Old)
				s.dir.byAddr[me] = name
				moved = name
			}
		}
		return nil
	})
	writeJSON(w, http.StatusOK, map[string]string{"username": moved})
}

// myPhone returns the caller's linked number (masked).
func (s *server) myPhone(w http.ResponseWriter, r *http.Request) {
	me := reqauth.Caller(r)
	var phone string
	s.phones.Read(func(d *phoneData) {
		for p, a := range d.Phones {
			if a == me {
				phone = p
			}
		}
	})
	writeJSON(w, http.StatusOK, map[string]string{"phone": maskPhone(phone)})
}
