package main

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func pngBytes(t *testing.T, c color.Color) []byte {
	img := image.NewRGBA(image.Rect(0, 0, 32, 32))
	for x := range 32 {
		for y := range 32 {
			img.Set(x, y, c)
		}
	}
	var b bytes.Buffer
	png.Encode(&b, img)
	return b.Bytes()
}

func TestLogoCandidatesOrder(t *testing.T) {
	page, _ := url.Parse("https://gabis.pages.dev/app/")
	html := `<head>
	  <link rel="icon" href="/favicon-16.png" sizes="16x16">
	  <link rel="icon" type="image/png" href="icon-192.png" sizes="192x192">
	  <link rel="icon" href="/logo.svg" type="image/svg+xml">
	  <link rel="apple-touch-icon" href="https://cdn.gabis.example/touch.png">
	  <meta property="og:image" content="/og.jpg">
	</head>`
	got := logoCandidates(page, html)
	want := []string{
		"https://cdn.gabis.example/touch.png",
		"https://gabis.pages.dev/app/icon-192.png",
		"https://gabis.pages.dev/favicon-16.png",
		"https://gabis.pages.dev/og.jpg",
		"https://gabis.pages.dev/favicon.ico",
	}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Fatalf("candidates:\n got  %v\n want %v", got, want)
	}
}

func TestPartnerLogoInherited(t *testing.T) {
	logo := pngBytes(t, color.RGBA{249, 115, 22, 255})
	site := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/":
			io.WriteString(w, `<html><head><link rel="icon" href="/evil.svg"><link rel="apple-touch-icon" href="/fake.png"><link rel="icon" href="/brand.png" sizes="64x64"></head></html>`)
		case "/fake.png": // claims to be an image but is HTML
			io.WriteString(w, "<html><script>alert(1)</script></html>")
		case "/brand.png":
			w.Write(logo)
		default:
			http.NotFound(w, r)
		}
	}))
	defer site.Close()

	e := newEnv(t)
	owner := newWallet(t)
	var app App
	e.call("POST", "/api/apps", map[string]string{"name": "Gabis", "website": site.URL, "contact_email": "a@gabis.example"}, &owner, "", &app)
	e.call("POST", "/api/apps/"+app.ID+"/review", map[string]string{"decision": "approve"}, &e.admin, "", nil)

	// Approval fetches the logo in the background.
	var pub map[string]any
	for range 100 {
		e.call("GET", "/api/apps/"+app.ID, nil, nil, "", &pub)
		if pub["logo_url"] != nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	logoURL, _ := pub["logo_url"].(string)
	if logoURL == "" {
		t.Fatal("no logo after approval")
	}
	resp, err := http.Get(e.api.URL + logoURL)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if !bytes.Equal(body, logo) || resp.Header.Get("Content-Type") != "image/png" || resp.Header.Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("served logo: %s %q", resp.Header.Get("Content-Type"), body[:8])
	}
}

func TestLogoFallsBackToFaviconAndRejectsNonImages(t *testing.T) {
	ico := append([]byte{0, 0, 1, 0, 1, 0}, bytes.Repeat([]byte{7}, 200)...)
	var serveIco bool
	site := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/":
			io.WriteString(w, `<link rel="icon" href="/only.svg" type="image/svg+xml">`)
		case r.URL.Path == "/favicon.ico" && serveIco:
			w.Write(ico)
		case r.URL.Path == "/favicon.ico":
			io.WriteString(w, `<svg xmlns="http://www.w3.org/2000/svg" onload="alert(1)"/>`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer site.Close()
	e := newEnv(t)
	if _, _, _, err := e.srv.fetchLogo(site.URL); err == nil {
		t.Fatal("accepted an SVG disguised as favicon.ico")
	}
	serveIco = true
	img, ct, src, err := e.srv.fetchLogo(site.URL)
	if err != nil || ct != "image/x-icon" || !strings.HasSuffix(src, "/favicon.ico") || !bytes.Equal(img, ico) {
		t.Fatalf("ico fallback: %v %s %s", err, ct, src)
	}
}
