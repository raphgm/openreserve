package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Partner logos are taken automatically from the partner's own website, so
// co-branded pages show "<their logo> Gabis × ORPay" without any upload.
// The logo is fetched server-side (public addresses only, size-capped),
// checked to really be a PNG/JPEG/WebP/ICO image, and served from our own
// origin. SVG is refused: an SVG can carry script.

const maxLogoBytes = 512 << 10

var (
	linkTagRe = regexp.MustCompile(`(?is)<link\b[^>]*>`)
	metaTagRe = regexp.MustCompile(`(?is)<meta\b[^>]*>`)
	attrRe    = regexp.MustCompile(`(?is)([a-z:-]+)\s*=\s*("[^"]*"|'[^']*'|[^\s>]+)`)
)

var allowedLogoTypes = map[string]string{
	"image/png": "png", "image/jpeg": "jpg", "image/webp": "webp",
	"image/x-icon": "ico", "image/vnd.microsoft.icon": "ico",
}

func attrs(tag string) map[string]string {
	out := map[string]string{}
	for _, m := range attrRe.FindAllStringSubmatch(tag, -1) {
		out[strings.ToLower(m[1])] = strings.Trim(m[2], `"'`)
	}
	return out
}

// logoCandidates lists image URLs found in a page, best first.
func logoCandidates(page *url.URL, html string) []string {
	type cand struct {
		url   string
		score int
	}
	var cs []cand
	add := func(href string, score int) {
		if href == "" || strings.HasPrefix(href, "data:") {
			return
		}
		u, err := page.Parse(href)
		if err != nil || (u.Scheme != "https" && u.Scheme != "http") {
			return
		}
		cs = append(cs, cand{u.String(), score})
	}
	for _, tag := range linkTagRe.FindAllString(html, -1) {
		a := attrs(tag)
		rel := strings.ToLower(a["rel"])
		if strings.Contains(a["type"], "svg") || strings.HasSuffix(strings.ToLower(a["href"]), ".svg") {
			continue
		}
		size := 0
		if s := strings.SplitN(strings.ToLower(a["sizes"]), "x", 2); len(s) == 2 {
			size, _ = strconv.Atoi(s[0])
		}
		switch {
		case strings.Contains(rel, "apple-touch-icon"):
			add(a["href"], 1000+size)
		case strings.Contains(rel, "icon"):
			add(a["href"], 500+size)
		}
	}
	for _, tag := range metaTagRe.FindAllString(html, -1) {
		a := attrs(tag)
		if p := strings.ToLower(a["property"] + a["name"]); p == "og:image" || p == "og:logo" {
			add(a["content"], 100)
		}
	}
	add("/favicon.ico", 1)
	// Best first; keep order stable among equals.
	for i := 1; i < len(cs); i++ {
		for j := i; j > 0 && cs[j].score > cs[j-1].score; j-- {
			cs[j], cs[j-1] = cs[j-1], cs[j]
		}
	}
	out := make([]string, len(cs))
	for i, c := range cs {
		out[i] = c.url
	}
	return out
}

func (s *server) logoClient() *http.Client {
	c := *s.hookClient // same public-only dialer as webhooks
	c.CheckRedirect = func(_ *http.Request, via []*http.Request) error {
		if len(via) >= 3 {
			return errors.New("too many redirects")
		}
		return nil
	}
	return &c
}

func (s *server) get(client *http.Client, u string, limit int64) ([]byte, string, error) {
	req, _ := http.NewRequest(http.MethodGet, u, nil)
	req.Header.Set("User-Agent", "ORPay-LogoFetcher/1 (+co-branding)")
	resp, err := client.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("%s returned %s", u, resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, "", err
	}
	if int64(len(body)) > limit {
		return nil, "", errors.New("too large")
	}
	return body, resp.Header.Get("Content-Type"), nil
}

// fetchLogo finds and downloads a logo for a website.
func (s *server) fetchLogo(website string) ([]byte, string, string, error) {
	page, err := url.Parse(website)
	if err != nil || page.Host == "" {
		return nil, "", "", errors.New("invalid website")
	}
	client := s.logoClient()
	html, _, err := s.get(client, page.String(), 1<<20)
	cands := []string{page.ResolveReference(&url.URL{Path: "/favicon.ico"}).String()}
	if err == nil {
		cands = logoCandidates(page, string(html))
	}
	var lastErr error = errors.New("no logo found")
	for _, u := range cands {
		img, _, err := s.get(client, u, maxLogoBytes)
		if err != nil {
			lastErr = err
			continue
		}
		ct := http.DetectContentType(img)
		if bytes.HasPrefix(img, []byte{0, 0, 1, 0}) {
			ct = "image/x-icon" // DetectContentType reports ICO as application/octet-stream
		}
		if _, ok := allowedLogoTypes[ct]; !ok || len(img) < 64 {
			lastErr = fmt.Errorf("%s is not a PNG/JPEG/WebP/ICO image", u)
			continue
		}
		return img, ct, u, nil
	}
	return nil, "", "", lastErr
}

// refreshLogo fetches and stores an app's logo. Failures keep the previous
// logo and are only logged: co-branding falls back to the initial.
func (s *server) refreshLogo(appID string) error {
	var website string
	s.apps.Read(func(apps map[string]*App) {
		if a, ok := apps[appID]; ok {
			website = a.Website
		}
	})
	if website == "" {
		return errors.New("app not found")
	}
	img, ct, src, err := s.fetchLogo(website)
	if err != nil {
		log.Printf("logo for %s (%s): %v", appID, website, err)
		return err
	}
	dir := filepath.Join(s.stateDir, "logos")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, appID), img, 0o644); err != nil {
		return err
	}
	return s.apps.Update(func(apps map[string]*App) error {
		if a, ok := apps[appID]; ok {
			a.LogoType, a.LogoSource, a.LogoAt = ct, src, s.now()
		}
		return nil
	})
}

// serveLogo returns an app's stored logo from our own origin.
func (s *server) serveLogo(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var ct string
	s.apps.Read(func(apps map[string]*App) {
		if a, ok := apps[id]; ok && a.Status == AppApproved {
			ct = a.LogoType
		}
	})
	if _, ok := allowedLogoTypes[ct]; !ok {
		http.NotFound(w, r)
		return
	}
	img, err := os.ReadFile(filepath.Join(s.stateDir, "logos", filepath.Base(id)))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", ct)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", "default-src 'none'")
	w.Header().Set("Cache-Control", "public, max-age=86400")
	w.Write(img)
}

func (s *server) refreshLogoHandler(w http.ResponseWriter, r *http.Request) {
	var id string
	err := s.ownedApp(r, func(a *App) error {
		if a.Status != AppApproved {
			return errors.New("logos are fetched once the app is approved")
		}
		id = a.ID
		return nil
	})
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if err := s.refreshLogo(id); err != nil {
		writeErr(w, http.StatusBadGateway, fmt.Errorf("could not fetch a logo from your website: %v", err))
		return
	}
	var out App
	s.apps.Read(func(apps map[string]*App) { out = apps[id].forOwner() })
	writeJSON(w, http.StatusOK, out)
}

// refreshLogos keeps approved apps' logos current (sites change them).
func (s *server) refreshLogos(every time.Duration) {
	for range time.Tick(every) {
		var ids []string
		s.apps.Read(func(apps map[string]*App) {
			for id, a := range apps {
				if a.Status == AppApproved && s.now().Sub(a.LogoAt) > every {
					ids = append(ids, id)
				}
			}
		})
		for _, id := range ids {
			s.refreshLogo(id)
		}
	}
}
