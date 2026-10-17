package rest

import (
	"fmt"
	"net/http/httptest"
	"testing"
	"time"
)

func TestLoginLimiterWindow(t *testing.T) {
	now := time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)
	l := NewLoginLimiter(2, 15*time.Minute)
	l.now = func() time.Time { return now }

	for i := range 2 {
		if !l.Allow("1.1.1.1") {
			t.Fatalf("attempt %d, within the limit, was refused", i+1)
		}
	}
	if l.Allow("1.1.1.1") {
		t.Error("the attempt over the limit was allowed")
	}
	if !l.Allow("2.2.2.2") {
		t.Error("another IP was limited by the first one's attempts")
	}

	now = now.Add(15 * time.Minute)
	if !l.Allow("1.1.1.1") {
		t.Error("the limit did not lift when the window passed")
	}
}

func TestLoginLimiterReset(t *testing.T) {
	l := NewLoginLimiter(1, time.Hour)
	l.Allow("1.1.1.1")
	l.Reset("1.1.1.1")
	if !l.Allow("1.1.1.1") {
		t.Error("an attempt after Reset was refused")
	}
}

func TestLoginLimiterForgetsExpiredIPs(t *testing.T) {
	now := time.Now()
	l := NewLoginLimiter(5, time.Minute)
	l.now = func() time.Time { return now }

	for i := range sweepThreshold {
		l.Allow(fmt.Sprintf("10.0.%d.%d", i/256, i%256))
	}
	now = now.Add(time.Minute)
	l.Allow("1.1.1.1")

	if len(l.attempts) != 1 {
		t.Errorf("tracking %d IPs after the window passed, want 1", len(l.attempts))
	}
}

func TestClientIP(t *testing.T) {
	tests := []struct {
		name       string
		header     string
		headerVal  string
		remoteAddr string
		want       string
	}{
		{"direct", "", "", "203.0.113.7:51234", "203.0.113.7"},
		{"direct ignores a spoofed header", "", "6.6.6.6", "203.0.113.7:51234", "203.0.113.7"},
		{"behind the proxy", "Fly-Client-IP", "198.51.100.2", "172.16.0.1:443", "198.51.100.2"},
		{"proxy header missing", "Fly-Client-IP", "", "172.16.0.1:443", "172.16.0.1"},
		{"IPv6", "", "", "[2001:db8::1]:443", "2001:db8::1"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := httptest.NewRequest("POST", "/login", nil)
			r.RemoteAddr = tt.remoteAddr
			if tt.headerVal != "" {
				r.Header.Set("Fly-Client-IP", tt.headerVal)
			}
			if got := clientIP(r, tt.header); got != tt.want {
				t.Errorf("clientIP = %q, want %q", got, tt.want)
			}
		})
	}
}
