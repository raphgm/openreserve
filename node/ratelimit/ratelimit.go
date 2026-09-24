// Package ratelimit provides per-client token-bucket rate limiting for HTTP APIs.
package ratelimit

import (
	"encoding/json"
	"math"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Limiter allows Rate events per second per key, with bursts up to Burst.
type Limiter struct {
	Rate  float64
	Burst float64

	mu      sync.Mutex
	buckets map[string]*bucket
	now     func() time.Time
	sweep   time.Time
}

type bucket struct {
	tokens float64
	last   time.Time
}

// New returns a limiter allowing perMinute events per minute, with bursts of burst.
func New(perMinute, burst int) *Limiter {
	return &Limiter{
		Rate: float64(perMinute) / 60, Burst: float64(burst),
		buckets: map[string]*bucket{}, now: time.Now,
	}
}

// Allow takes one token for key. If none is available it returns false and
// how long until one will be.
func (l *Limiter) Allow(key string) (bool, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	l.gc(now)
	b, ok := l.buckets[key]
	if !ok {
		b = &bucket{tokens: l.Burst, last: now}
		l.buckets[key] = b
	}
	b.tokens = math.Min(l.Burst, b.tokens+now.Sub(b.last).Seconds()*l.Rate)
	b.last = now
	if b.tokens >= 1 {
		b.tokens--
		return true, 0
	}
	return false, time.Duration((1 - b.tokens) / l.Rate * float64(time.Second))
}

// gc drops buckets that have refilled completely, so memory stays bounded by
// the number of recently active clients.
func (l *Limiter) gc(now time.Time) {
	if now.Sub(l.sweep) < time.Minute {
		return
	}
	l.sweep = now
	full := time.Duration(l.Burst / l.Rate * float64(time.Second))
	for k, b := range l.buckets {
		if now.Sub(b.last) > full {
			delete(l.buckets, k)
		}
	}
}

// ClientIP returns the caller's IP. When trustProxy is set (the service sits
// behind a reverse proxy such as Caddy), it uses the last X-Forwarded-For
// entry, which the proxy appends and the client cannot forge.
func ClientIP(r *http.Request, trustProxy bool) string {
	if trustProxy {
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			parts := strings.Split(xff, ",")
			return strings.TrimSpace(parts[len(parts)-1])
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// Middleware rejects requests over the limit with 429 and a Retry-After header.
func Middleware(l *Limiter, trustProxy bool, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if ok, wait := l.Allow(ClientIP(r, trustProxy)); !ok {
			TooMany(w, wait)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// TooMany writes a 429 JSON error.
func TooMany(w http.ResponseWriter, wait time.Duration) {
	secs := int(math.Ceil(wait.Seconds()))
	w.Header().Set("Retry-After", strconv.Itoa(secs))
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusTooManyRequests)
	json.NewEncoder(w).Encode(map[string]string{"error": "too many requests, retry in " + strconv.Itoa(secs) + "s"})
}
