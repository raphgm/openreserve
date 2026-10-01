package main

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/openreserve/node/reqauth"
	"github.com/openreserve/node/types"
)

// Six-word sign-in. Signing in on a new phone with 24 recovery words is too
// much, so a wallet can also be opened with its @username and six words.
//
// The six words never reach ORPay. The wallet derives two keys from them
// (PBKDF2, 600k rounds): one encrypts the wallet key in the browser, the
// other is a login token. ORPay stores the encrypted wallet key and a hash
// of the token, and hands the encrypted key back only to someone who
// presents the right token, with a strict limit on wrong guesses. Six words
// are 66 random bits, so guessing through the API is hopeless, and even a
// copy of ORPay's database would need billions of years of PBKDF2 work.
// The 24 recovery words stay the offline backup.

type signIn struct {
	Salt      string    `json:"salt"`
	IV        string    `json:"iv"`
	CT        string    `json:"ct"`
	AuthHash  string    `json:"auth_hash"`
	Fails     int       `json:"fails"`
	LockedTil time.Time `json:"locked_til"`
	UpdatedAt time.Time `json:"updated_at"`
}

var hexRe = regexp.MustCompile(`^[0-9a-f]+$`)

func authHash(auth string) string {
	sum := sha256.Sum256([]byte("orpay-signin|" + auth))
	return hex.EncodeToString(sum[:])
}

// setSignIn turns on (or replaces) six-word sign-in for the caller.
func (s *server) setSignIn(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Salt, IV, CT, Auth string
	}
	if err := decodeBody(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	for _, v := range []struct {
		val  string
		n, m int
	}{{req.Salt, 32, 32}, {req.IV, 24, 24}, {req.CT, 64, 200}, {req.Auth, 64, 64}} {
		if len(v.val) < v.n || len(v.val) > v.m || !hexRe.MatchString(v.val) {
			writeErr(w, http.StatusBadRequest, errors.New("invalid sign-in data"))
			return
		}
	}
	me := reqauth.Caller(r)
	if _, ok := s.dir.byAddress(me); !ok {
		writeErr(w, http.StatusBadRequest, errors.New("claim a @username first: you sign in with it"))
		return
	}
	err := s.signins.Update(func(m map[types.Address]*signIn) error {
		m[me] = &signIn{Salt: req.Salt, IV: req.IV, CT: req.CT, AuthHash: authHash(req.Auth), UpdatedAt: s.now()}
		return nil
	})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"enabled": true})
}

func (s *server) deleteSignIn(w http.ResponseWriter, r *http.Request) {
	me := reqauth.Caller(r)
	s.signins.Update(func(m map[types.Address]*signIn) error { delete(m, me); return nil })
	writeJSON(w, http.StatusOK, map[string]bool{"enabled": false})
}

func (s *server) signInStatus(w http.ResponseWriter, r *http.Request) {
	me := reqauth.Caller(r)
	var on bool
	s.signins.Read(func(m map[types.Address]*signIn) { _, on = m[me] })
	writeJSON(w, http.StatusOK, map[string]bool{"enabled": on})
}

func (s *server) signInFor(name string) (types.Address, error) {
	addr, ok := s.dir.byName(strings.TrimPrefix(strings.ToLower(strings.TrimSpace(name)), "@"))
	if !ok {
		return "", errors.New("no ORPay user with that @username")
	}
	return addr, nil
}

// signInSalt returns the salt a device needs to derive the login token.
func (s *server) signInSalt(w http.ResponseWriter, r *http.Request) {
	var req struct{ Username string }
	if err := decodeBody(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	addr, err := s.signInFor(req.Username)
	if err != nil {
		writeErr(w, http.StatusNotFound, err)
		return
	}
	var rec *signIn
	s.signins.Read(func(m map[types.Address]*signIn) { rec = m[addr] })
	if rec == nil {
		writeErr(w, http.StatusNotFound, errors.New("six-word sign-in is not set up for this wallet; use your 24 recovery words"))
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"salt": rec.Salt})
}

// openSignIn checks the login token and returns the encrypted wallet key.
// Wrong tokens lock the account for longer each time (5 tries, then
// 15 min, 30 min, 1 h ... up to a day).
func (s *server) openSignIn(w http.ResponseWriter, r *http.Request) {
	var req struct{ Username, Auth string }
	if err := decodeBody(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	addr, err := s.signInFor(req.Username)
	if err != nil {
		writeErr(w, http.StatusNotFound, err)
		return
	}
	var out *signIn
	var status int
	err = s.signins.Update(func(m map[types.Address]*signIn) error {
		rec := m[addr]
		now := s.now()
		switch {
		case rec == nil:
			status = http.StatusNotFound
			return errors.New("six-word sign-in is not set up for this wallet; use your 24 recovery words")
		case now.Before(rec.LockedTil):
			status = http.StatusTooManyRequests
			return errors.New("too many wrong attempts; try again after " + rec.LockedTil.Format("15:04") + " or use your 24 recovery words")
		}
		if subtle.ConstantTimeCompare([]byte(rec.AuthHash), []byte(authHash(req.Auth))) != 1 {
			rec.Fails++
			if rec.Fails >= 5 {
				d := 15 * time.Minute << min(rec.Fails-5, 6)
				rec.LockedTil = now.Add(min(d, 24*time.Hour))
			}
			status = http.StatusUnauthorized
			return errors.New("those words don't match this @username")
		}
		rec.Fails, rec.LockedTil = 0, time.Time{}
		c := *rec
		out = &c
		return nil
	})
	if err != nil {
		writeErr(w, status, err)
		return
	}
	go s.notify(addr, notification{Title: "New sign-in", Body: "Your wallet was opened on a new device with your six words. If this wasn't you, move your funds and turn off six-word sign-in.", URL: "/"})
	writeJSON(w, http.StatusOK, map[string]string{"address": string(addr), "salt": out.Salt, "iv": out.IV, "ct": out.CT})
}
