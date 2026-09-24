package main

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"syscall"
	"time"
)

// Webhooks tell an app its invoice was paid or expired. Each delivery is
// signed so the app can reject forgeries:
//
//	ORPay-Signature: t=<unix seconds>,v1=<hex HMAC-SHA256(secret, "<t>.<body>")>
//
// Failed deliveries are retried with backoff for about a day.

var hookBackoff = []time.Duration{0, 10 * time.Second, time.Minute, 5 * time.Minute, 30 * time.Minute, 2 * time.Hour, 6 * time.Hour, 12 * time.Hour}

// SignWebhook computes the ORPay-Signature header value.
func SignWebhook(secret string, ts int64, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	fmt.Fprintf(mac, "%d.", ts)
	mac.Write(body)
	return fmt.Sprintf("t=%d,v1=%s", ts, hex.EncodeToString(mac.Sum(nil)))
}

func (s *server) deliverWebhooks() {
	type job struct {
		inv Invoice
		url string
		sec string
	}
	var jobs []job
	now := s.now()
	s.invoices.read(func(m map[string]*Invoice) {
		for _, inv := range m {
			if inv.HookPending && !now.Before(inv.HookNextAt) {
				jobs = append(jobs, job{inv: *inv})
			}
		}
	})
	for i := range jobs {
		s.apps.read(func(apps map[string]*App) {
			if a, ok := apps[jobs[i].inv.AppID]; ok {
				jobs[i].url, jobs[i].sec = a.WebhookURL, a.WebhookSecret
			}
		})
	}
	for _, j := range jobs {
		err := errors.New("no webhook URL configured")
		if j.url != "" {
			err = s.postWebhook(j.url, j.sec, &j.inv)
		}
		s.invoices.update(func(m map[string]*Invoice) error {
			inv := m[j.inv.ID]
			if inv == nil {
				return nil
			}
			if err == nil || j.url == "" {
				inv.HookPending = false
				return nil
			}
			inv.HookTries++
			if inv.HookTries >= len(hookBackoff) {
				inv.HookPending = false
				log.Printf("webhook for %s abandoned after %d tries: %v", s.invoiceSummary(inv), inv.HookTries, err)
				return nil
			}
			inv.HookNextAt = s.now().Add(hookBackoff[inv.HookTries])
			return nil
		})
	}
}

func (s *server) postWebhook(target, secret string, inv *Invoice) error {
	event := "invoice." + inv.Status
	body, _ := json.Marshal(map[string]any{"type": event, "created": s.now().Unix(), "data": s.invoiceView(inv, true)})
	ts := s.now().Unix()
	req, _ := http.NewRequest(http.MethodPost, target, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "ORPay-Webhooks/1")
	req.Header.Set("ORPay-Event", event)
	req.Header.Set("ORPay-Signature", SignWebhook(secret, ts, body))
	resp, err := s.hookClient.Do(req)
	if err != nil {
		return err
	}
	resp.Body.Close()
	if resp.StatusCode >= 300 {
		return errors.New("endpoint returned " + strconv.Itoa(resp.StatusCode))
	}
	return nil
}

// checkWebhookURL requires https unless private webhooks are allowed (dev).
func (s *server) checkWebhookURL(raw string) error {
	if err := validURL(raw, !s.privateHooks); err != nil {
		return err
	}
	u, _ := url.Parse(raw)
	if !s.privateHooks {
		ips, err := net.LookupIP(u.Hostname())
		if err != nil {
			return fmt.Errorf("webhook host %s does not resolve", u.Hostname())
		}
		for _, ip := range ips {
			if !publicIP(ip) {
				return errors.New("webhook must point to a public address")
			}
		}
	}
	return nil
}

func publicIP(ip net.IP) bool {
	return !(ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		ip.IsUnspecified() || ip.IsMulticast() || ip.IsInterfaceLocalMulticast())
}

// webhookClient refuses to connect to private addresses at dial time too, so
// DNS changes after validation cannot redirect webhooks into our network.
func webhookClient(allowPrivate bool) *http.Client {
	dialer := &net.Dialer{Timeout: 5 * time.Second}
	if !allowPrivate {
		dialer.Control = func(_, address string, _ syscall.RawConn) error {
			host, _, _ := net.SplitHostPort(address)
			if ip := net.ParseIP(host); ip == nil || !publicIP(ip) {
				return fmt.Errorf("refusing to connect to non-public address %s", host)
			}
			return nil
		}
	}
	return &http.Client{
		Timeout: 10 * time.Second,
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
				return dialer.DialContext(ctx, network, addr)
			},
			TLSHandshakeTimeout: 5 * time.Second,
		},
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
}
