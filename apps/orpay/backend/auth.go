package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/openreserve/node/types"
)

// Signed requests prove the caller controls an address without accounts or
// passwords. The wallet signs:
//
//	orpay/req/v1\n<METHOD>\n<path>\n<unix ms>\n<hex sha256 of body>
//
// and sends X-ORP-Address, X-ORP-Time and X-ORP-Sig headers. Requests older
// than maxSkew are rejected; creations carry a client-chosen id, so a
// replayed request cannot create a duplicate.
const maxSkew = 5 * time.Minute

type addrKey struct{}

// RequestMessage is the string a wallet signs for a request.
func RequestMessage(method, path string, ts int64, body []byte) string {
	sum := sha256.Sum256(body)
	return fmt.Sprintf("orpay/req/v1\n%s\n%s\n%d\n%s", method, path, ts, hex.EncodeToString(sum[:]))
}

// signed wraps a handler so it only runs for a valid signed request. The
// caller's address is available via caller(r).
func signed(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		addr, body, err := verifyRequest(w, r)
		if err != nil {
			writeErr(w, http.StatusUnauthorized, err)
			return
		}
		r.Body = io.NopCloser(bytes.NewReader(body))
		next(w, r.WithContext(context.WithValue(r.Context(), addrKey{}, addr)))
	}
}

func verifyRequest(w http.ResponseWriter, r *http.Request) (types.Address, []byte, error) {
	addr := types.Address(r.Header.Get("X-ORP-Address"))
	pub, err := addr.PubKey()
	if err != nil {
		return "", nil, errors.New("missing or invalid X-ORP-Address")
	}
	ts, err := strconv.ParseInt(r.Header.Get("X-ORP-Time"), 10, 64)
	if err != nil {
		return "", nil, errors.New("missing X-ORP-Time")
	}
	if skew := time.Since(time.UnixMilli(ts)); skew > maxSkew || skew < -maxSkew {
		return "", nil, errors.New("request time is too far from server time; check your clock")
	}
	sig, err := hex.DecodeString(r.Header.Get("X-ORP-Sig"))
	if err != nil {
		return "", nil, errors.New("invalid X-ORP-Sig")
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 16<<10))
	if err != nil {
		return "", nil, err
	}
	if !ed25519.Verify(pub, []byte(RequestMessage(r.Method, r.URL.Path, ts, body)), sig) {
		return "", nil, errors.New("signature does not match address")
	}
	return addr, body, nil
}

func caller(r *http.Request) types.Address {
	a, _ := r.Context().Value(addrKey{}).(types.Address)
	return a
}
