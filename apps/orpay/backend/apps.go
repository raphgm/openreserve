package main

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/openreserve/node/reqauth"
	"github.com/openreserve/node/types"
)

// Partner apps (e.g. Gabis, Paynautik, or any approved company) integrate
// OpenReserve payments through API keys. A company requests access with its
// wallet; an admin approves it; the owner then issues API keys and receives
// signed webhooks. Payments always settle on-chain to the app's settlement
// address, which only the company controls.

const (
	AppPending   = "pending"
	AppApproved  = "approved"
	AppRejected  = "rejected"
	AppSuspended = "suspended"
)

type App struct {
	ID          string        `json:"id"`
	Name        string        `json:"name"`
	Website     string        `json:"website"`
	Contact     string        `json:"contact_email"`
	Description string        `json:"description"`
	Owner       types.Address `json:"owner"`
	Settlement  types.Address `json:"settlement_address"`
	Status      string        `json:"status"`
	WebhookURL  string        `json:"webhook_url,omitempty"`
	// Co-branding shown next to ORPay on this app's checkout and escrow pages.
	BrandName  string `json:"brand_name,omitempty"`  // defaults to Name
	BrandColor string `json:"brand_color,omitempty"` // #rrggbb
	// ArbiterAddr settles disputes on this app's escrows (default: Owner).
	ArbiterAddr types.Address `json:"arbiter_address,omitempty"`
	CreatedAt   time.Time     `json:"created_at"`
	ReviewedAt  time.Time     `json:"reviewed_at,omitzero"`
	ReviewNote  string        `json:"review_note,omitempty"`

	// Secrets: only the owner ever sees these, and the API key only once.
	KeyHash       string `json:"key_hash,omitempty"`
	KeyPrefix     string `json:"key_prefix,omitempty"`
	WebhookSecret string `json:"webhook_secret,omitempty"`
}

// Arbiter is who settles disputes on this app's escrows.
func (a *App) Arbiter() types.Address {
	if a.ArbiterAddr != "" {
		return a.ArbiterAddr
	}
	return a.Owner
}

// public is what anyone may see about an app (shown on its checkout page).
func (a *App) public() map[string]any {
	brand := a.BrandName
	if brand == "" {
		brand = a.Name
	}
	return map[string]any{"id": a.ID, "name": a.Name, "website": a.Website, "status": a.Status,
		"brand_name": brand, "brand_color": a.BrandColor}
}

// forOwner hides the key hash but includes the webhook secret.
func (a *App) forOwner() App {
	c := *a
	c.KeyHash = ""
	return c
}

var (
	appNameRe = regexp.MustCompile(`^[\p{L}\p{N} .&'-]{2,40}$`)
	colorRe   = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)
)

func randToken(prefix string, n int) string {
	b := make([]byte, n)
	rand.Read(b)
	return prefix + hex.EncodeToString(b)
}

func hashKey(k string) string {
	sum := sha256.Sum256([]byte(k))
	return hex.EncodeToString(sum[:])
}

func (s *server) isAdmin(a types.Address) bool { return slices.Contains(s.admins, a) }

func validURL(raw string, requireHTTPS bool) error {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") {
		return fmt.Errorf("%q is not a valid URL", raw)
	}
	if requireHTTPS && u.Scheme != "https" {
		return fmt.Errorf("%q must use https", raw)
	}
	return nil
}

func (s *server) requestApp(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name        string        `json:"name"`
		Website     string        `json:"website"`
		Contact     string        `json:"contact_email"`
		Description string        `json:"description"`
		Settlement  types.Address `json:"settlement_address"`
	}
	if err := decodeBody(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	switch {
	case !appNameRe.MatchString(req.Name):
		writeErr(w, http.StatusBadRequest, errors.New("app name must be 2-40 letters, numbers, spaces or .&'-"))
		return
	case validURL(req.Website, false) != nil:
		writeErr(w, http.StatusBadRequest, errors.New("enter your website, e.g. https://gabis.app"))
		return
	case !strings.Contains(req.Contact, "@") || len(req.Contact) > 120:
		writeErr(w, http.StatusBadRequest, errors.New("enter a contact email"))
		return
	case len(req.Description) > 1000:
		writeErr(w, http.StatusBadRequest, errors.New("description is too long"))
		return
	}
	owner := reqauth.Caller(r)
	if req.Settlement == "" {
		req.Settlement = owner
	} else if err := req.Settlement.Validate(); err != nil {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("settlement address: %w", err))
		return
	}
	app := &App{
		ID: randToken("app_", 8), Name: req.Name, Website: req.Website, Contact: req.Contact,
		Description: req.Description, Owner: owner, Settlement: req.Settlement,
		Status: AppPending, CreatedAt: s.now(),
	}
	err := s.apps.Update(func(apps map[string]*App) error {
		for _, a := range apps {
			if strings.EqualFold(a.Name, app.Name) {
				return fmt.Errorf("an app named %q already exists", a.Name)
			}
		}
		apps[app.ID] = app
		return nil
	})
	if err != nil {
		writeErr(w, http.StatusConflict, err)
		return
	}
	writeJSON(w, http.StatusCreated, app.forOwner())
}

// listApps returns the caller's apps, or every app for an admin.
func (s *server) listApps(w http.ResponseWriter, r *http.Request) {
	me := reqauth.Caller(r)
	admin := s.isAdmin(me)
	out := []App{}
	s.apps.Read(func(apps map[string]*App) {
		for _, a := range apps {
			if admin || a.Owner == me {
				c := a.forOwner()
				if a.Owner != me {
					c.WebhookSecret = "" // admins review apps but never see their secrets
				}
				out = append(out, c)
			}
		}
	})
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	writeJSON(w, http.StatusOK, map[string]any{"apps": out, "is_admin": admin})
}

func (s *server) publicApp(w http.ResponseWriter, r *http.Request) {
	var info map[string]any
	s.apps.Read(func(apps map[string]*App) {
		if a, ok := apps[r.PathValue("id")]; ok {
			info = a.public()
		}
	})
	if info == nil {
		writeErr(w, http.StatusNotFound, errors.New("app not found"))
		return
	}
	writeJSON(w, http.StatusOK, info)
}

// reviewApp lets an admin approve, reject or suspend an app.
func (s *server) reviewApp(w http.ResponseWriter, r *http.Request) {
	if !s.isAdmin(reqauth.Caller(r)) {
		writeErr(w, http.StatusForbidden, errors.New("only admins can review apps"))
		return
	}
	var req struct {
		Decision string `json:"decision"` // approve | reject | suspend
		Note     string `json:"note"`
	}
	if err := decodeBody(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	status := map[string]string{"approve": AppApproved, "reject": AppRejected, "suspend": AppSuspended}[req.Decision]
	if status == "" {
		writeErr(w, http.StatusBadRequest, errors.New("decision must be approve, reject or suspend"))
		return
	}
	var out App
	err := s.apps.Update(func(apps map[string]*App) error {
		a, ok := apps[r.PathValue("id")]
		if !ok {
			return errors.New("app not found")
		}
		a.Status, a.ReviewNote, a.ReviewedAt = status, req.Note, s.now()
		if status == AppApproved && a.WebhookSecret == "" {
			a.WebhookSecret = randToken("whsec_", 24)
		}
		out = *a
		return nil
	})
	if err != nil {
		writeErr(w, http.StatusNotFound, err)
		return
	}
	out.KeyHash, out.WebhookSecret = "", ""
	writeJSON(w, http.StatusOK, out)
}

// ownedApp runs fn on an app owned by the caller.
func (s *server) ownedApp(r *http.Request, fn func(*App) error) error {
	return s.apps.Update(func(apps map[string]*App) error {
		a, ok := apps[r.PathValue("id")]
		if !ok || a.Owner != reqauth.Caller(r) {
			return errors.New("app not found")
		}
		return fn(a)
	})
}

// rotateKey issues a new API key, invalidating the previous one. The key is
// returned exactly once; only its hash is stored.
func (s *server) rotateKey(w http.ResponseWriter, r *http.Request) {
	var key string
	err := s.ownedApp(r, func(a *App) error {
		if a.Status != AppApproved {
			return fmt.Errorf("app is %s; keys are issued after approval", a.Status)
		}
		key = randToken("orp_sk_", 24)
		a.KeyHash, a.KeyPrefix = hashKey(key), key[:14]
		return nil
	})
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"api_key": key, "note": "Store this key now; it will not be shown again."})
}

func (s *server) updateApp(w http.ResponseWriter, r *http.Request) {
	var req struct {
		WebhookURL *string        `json:"webhook_url"`
		Settlement *types.Address `json:"settlement_address"`
		Arbiter    *types.Address `json:"arbiter_address"`
		BrandName  *string        `json:"brand_name"`
		BrandColor *string        `json:"brand_color"`
	}
	if err := decodeBody(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	var out App
	err := s.ownedApp(r, func(a *App) error {
		if req.WebhookURL != nil {
			if *req.WebhookURL != "" {
				if err := s.checkWebhookURL(*req.WebhookURL); err != nil {
					return err
				}
			}
			a.WebhookURL = *req.WebhookURL
		}
		if req.BrandName != nil {
			n := strings.TrimSpace(*req.BrandName)
			if n != "" && !appNameRe.MatchString(n) {
				return errors.New("brand name must be 2-40 letters, numbers, spaces or .&'-")
			}
			a.BrandName = n
		}
		if req.BrandColor != nil {
			if *req.BrandColor != "" && !colorRe.MatchString(*req.BrandColor) {
				return errors.New("brand colour must look like #1a73e8")
			}
			a.BrandColor = strings.ToLower(*req.BrandColor)
		}
		if req.Arbiter != nil {
			if *req.Arbiter != "" {
				if err := req.Arbiter.Validate(); err != nil {
					return fmt.Errorf("arbiter address: %w", err)
				}
			}
			a.ArbiterAddr = *req.Arbiter
		}
		if req.Settlement != nil {
			if err := req.Settlement.Validate(); err != nil {
				return fmt.Errorf("settlement address: %w", err)
			}
			a.Settlement = *req.Settlement
		}
		out = a.forOwner()
		return nil
	})
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

type appKey struct{}

// apiKeyAuth authenticates server-to-server calls from an approved app with
// "Authorization: Bearer orp_sk_...".
func (s *server) apiKeyAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		key, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		if !ok || !strings.HasPrefix(key, "orp_sk_") {
			writeErr(w, http.StatusUnauthorized, errors.New("missing API key (Authorization: Bearer orp_sk_...)"))
			return
		}
		h := hashKey(key)
		var app *App
		s.apps.Read(func(apps map[string]*App) {
			for _, a := range apps {
				if a.KeyHash != "" && subtle.ConstantTimeCompare([]byte(a.KeyHash), []byte(h)) == 1 {
					c := *a
					app = &c
				}
			}
		})
		switch {
		case app == nil:
			writeErr(w, http.StatusUnauthorized, errors.New("invalid API key"))
			return
		case app.Status != AppApproved:
			writeErr(w, http.StatusForbidden, fmt.Errorf("app is %s", app.Status))
			return
		}
		next(w, r.WithContext(withApp(r.Context(), app)))
	}
}
