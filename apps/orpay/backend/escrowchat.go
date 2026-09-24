package main

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/openreserve/node/client"
	"github.com/openreserve/node/reqauth"
	"github.com/openreserve/node/types"
)

// ----- Escrow terms -----
//
// Returns are not supported, so buyers must accept the seller's terms
// before locking funds. Terms are stored by their SHA-256; the buyer's
// escrow create tx carries "terms:<sha256>" in its memo, which is signed,
// on-chain proof of exactly what they agreed to.

type Terms struct {
	Text      string    `json:"text"`
	CreatedAt time.Time `json:"created_at"`
}

const maxTermsLen = 4000

func termsHash(text string) string {
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:])
}

// storeTerms saves terms text and returns its hash (idempotent).
func (s *server) storeTerms(text string) (string, error) {
	text = strings.TrimSpace(text)
	if text == "" || len(text) > maxTermsLen {
		return "", fmt.Errorf("terms must be 1-%d characters", maxTermsLen)
	}
	h := termsHash(text)
	err := s.terms.Update(func(m map[string]*Terms) error {
		if _, ok := m[h]; !ok {
			m[h] = &Terms{Text: text, CreatedAt: s.now()}
		}
		return nil
	})
	return h, err
}

func (s *server) postTerms(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Text string `json:"text"`
	}
	if err := decodeBody(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	h, err := s.storeTerms(req.Text)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"hash": h, "memo": "terms:" + h})
}

func (s *server) getTerms(w http.ResponseWriter, r *http.Request) {
	var t *Terms
	s.terms.Read(func(m map[string]*Terms) { t = m[r.PathValue("hash")] })
	if t == nil {
		writeErr(w, http.StatusNotFound, errors.New("terms not found"))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"hash": r.PathValue("hash"), "text": t.Text, "created_at": t.CreatedAt})
}

// ----- Escrow chat and evidence -----
//
// Buyer, seller and arbiter can message each other and attach photos, e.g.
// the goods on delivery or proof of dispatch. Only those three can read
// the thread. Each photo's SHA-256 is recorded so the arbiter can rely on
// it not having changed. Photos are served through short-lived signed URLs.

type EscrowMessage struct {
	ID    string        `json:"id"`
	From  types.Address `json:"from"`
	Role  string        `json:"role"`
	Text  string        `json:"text,omitempty"`
	Files []EvidenceRef `json:"files,omitempty"`
	At    time.Time     `json:"at"`
}

type EvidenceRef struct {
	ID     string `json:"id"`
	Type   string `json:"type"`
	Size   int    `json:"size"`
	SHA256 string `json:"sha256"`
	URL    string `json:"url,omitempty"` // filled per request, expires
}

const (
	maxMessageLen   = 2000
	maxPhotoBytes   = 3 << 20
	maxPhotos       = 4
	maxThreadLength = 500
	fileURLTTL      = time.Hour
)

var photoTypes = map[string]bool{"image/jpeg": true, "image/png": true, "image/webp": true}

// escrowRole returns the caller's role in an escrow, or an error.
func (s *server) escrowRole(id string, who types.Address) (string, *client.Escrow, error) {
	e, err := s.node.Escrow(id)
	if err != nil || e == nil {
		return "", nil, errors.New("escrow not found")
	}
	switch who {
	case e.Buyer:
		return "buyer", e, nil
	case e.Seller:
		return "seller", e, nil
	case e.Arbiter:
		return "arbiter", e, nil
	}
	return "", nil, errors.New("only the buyer, seller and arbiter can see this conversation")
}

func (s *server) postEscrowMessage(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	me := reqauth.Caller(r)
	role, _, err := s.escrowRole(id, me)
	if err != nil {
		writeErr(w, http.StatusForbidden, err)
		return
	}
	var req struct {
		Text   string   `json:"text"`
		Photos []string `json:"photos"` // base64 JPEG/PNG/WebP
	}
	if err := decodeBodyN(r, &req, 16<<20); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	req.Text = strings.TrimSpace(req.Text)
	if (req.Text == "" && len(req.Photos) == 0) || len(req.Text) > maxMessageLen || len(req.Photos) > maxPhotos {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("write a message (max %d chars) and/or attach up to %d photos", maxMessageLen, maxPhotos))
		return
	}
	msg := &EscrowMessage{ID: randToken("", 8), From: me, Role: role, Text: req.Text, At: s.now()}
	dir := filepath.Join(s.stateDir, "evidence")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	for i, b64 := range req.Photos {
		img, err := base64.StdEncoding.DecodeString(b64)
		if err != nil || len(img) == 0 || len(img) > maxPhotoBytes {
			writeErr(w, http.StatusBadRequest, fmt.Errorf("photo %d must be an image under 3 MB", i+1))
			return
		}
		ct := http.DetectContentType(img)
		if !photoTypes[ct] {
			writeErr(w, http.StatusBadRequest, fmt.Errorf("photo %d must be JPEG, PNG or WebP", i+1))
			return
		}
		sum := sha256.Sum256(img)
		ref := EvidenceRef{ID: randToken("", 12), Type: ct, Size: len(img), SHA256: hex.EncodeToString(sum[:])}
		if err := os.WriteFile(filepath.Join(dir, ref.ID), img, 0o600); err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
		msg.Files = append(msg.Files, ref)
	}
	err = s.chats.Update(func(m map[string][]*EscrowMessage) error {
		if len(m[id]) >= maxThreadLength {
			return errors.New("this conversation is full")
		}
		m[id] = append(m[id], msg)
		return nil
	})
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusCreated, s.withURLs(*msg))
}

func (s *server) listEscrowMessages(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if _, _, err := s.escrowRole(id, reqauth.Caller(r)); err != nil {
		writeErr(w, http.StatusForbidden, err)
		return
	}
	var msgs []EscrowMessage
	s.chats.Read(func(m map[string][]*EscrowMessage) {
		for _, msg := range m[id] {
			msgs = append(msgs, *msg)
		}
	})
	out := make([]EscrowMessage, len(msgs))
	for i, msg := range msgs {
		out[i] = s.withURLs(msg)
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *server) withURLs(m EscrowMessage) EscrowMessage {
	m.Files = slices.Clone(m.Files)
	exp := s.now().Add(fileURLTTL).Unix()
	for i := range m.Files {
		m.Files[i].URL = fmt.Sprintf("/api/escrow-files/%s?exp=%d&sig=%s", m.Files[i].ID, exp, s.fileSig(m.Files[i].ID, exp))
	}
	return m
}

func (s *server) fileSig(id string, exp int64) string {
	mac := hmac.New(sha256.New, s.secret)
	fmt.Fprintf(mac, "%s|%d", id, exp)
	return hex.EncodeToString(mac.Sum(nil))
}

// serveEvidence returns a photo for a valid, unexpired signed URL.
func (s *server) serveEvidence(w http.ResponseWriter, r *http.Request) {
	id := filepath.Base(r.PathValue("id"))
	exp, _ := strconv.ParseInt(r.URL.Query().Get("exp"), 10, 64)
	if exp < s.now().Unix() || !hmac.Equal([]byte(r.URL.Query().Get("sig")), []byte(s.fileSig(id, exp))) {
		http.Error(w, "link expired", http.StatusForbidden)
		return
	}
	img, err := os.ReadFile(filepath.Join(s.stateDir, "evidence", id))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	ct := http.DetectContentType(img)
	if !photoTypes[ct] {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", ct)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", "default-src 'none'")
	w.Header().Set("Cache-Control", "private, max-age=3600")
	w.Write(img)
}

// loadSecret returns the key for signing file URLs, creating it once.
func loadSecret(dir string) ([]byte, error) {
	p := filepath.Join(dir, "orpay-secret")
	if b, err := os.ReadFile(p); err == nil && len(b) == 32 {
		return b, nil
	}
	b := make([]byte, 32)
	rand.Read(b)
	return b, os.WriteFile(p, b, 0o600)
}
