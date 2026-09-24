package ratelimit

import (
	"net/http/httptest"
	"testing"
	"time"
)

func TestAllow(t *testing.T) {
	now := time.Unix(0, 0)
	l := New(60, 3) // 1 per second, burst 3
	l.now = func() time.Time { return now }

	for i := range 3 {
		if ok, _ := l.Allow("a"); !ok {
			t.Fatalf("burst request %d denied", i)
		}
	}
	ok, wait := l.Allow("a")
	if ok || wait <= 0 || wait > time.Second {
		t.Fatalf("over burst: ok=%v wait=%v", ok, wait)
	}
	if ok, _ := l.Allow("b"); !ok {
		t.Error("other key should have its own bucket")
	}
	now = now.Add(time.Second)
	if ok, _ := l.Allow("a"); !ok {
		t.Error("token not refilled after 1s")
	}
	now = now.Add(time.Hour)
	l.Allow("c") // triggers gc
	if _, ok := l.buckets["a"]; ok {
		t.Error("idle bucket not collected")
	}
}

func TestClientIP(t *testing.T) {
	r := httptest.NewRequest("GET", "/", nil)
	r.RemoteAddr = "10.0.0.1:5555"
	r.Header.Set("X-Forwarded-For", "6.6.6.6, 203.0.113.9")
	if ip := ClientIP(r, false); ip != "10.0.0.1" {
		t.Errorf("untrusted proxy: %s", ip)
	}
	if ip := ClientIP(r, true); ip != "203.0.113.9" {
		t.Errorf("trusted proxy: %s", ip)
	}
}
