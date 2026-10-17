package rest

import (
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// LoginLimiter caps login attempts per client IP in a fixed window.
//
// It lives in memory: there is one machine, and when it stops for being
// idle the counts go with it, which only ever makes the limit more
// lenient. Every attempt counts, not only failures, because each one
// costs an argon2id hash and the machine has little memory to spare.
type LoginLimiter struct {
	max    int
	window time.Duration
	now    func() time.Time

	mu       sync.Mutex
	attempts map[string]attemptWindow
}

type attemptWindow struct {
	count int
	start time.Time
}

// sweepThreshold is how many tracked IPs trigger a pass that forgets the
// expired ones, so a scan from many addresses cannot grow the map forever.
const sweepThreshold = 1024

func NewLoginLimiter(max int, window time.Duration) *LoginLimiter {
	return &LoginLimiter{
		max:      max,
		window:   window,
		now:      time.Now,
		attempts: map[string]attemptWindow{},
	}
}

// Allow records an attempt from key and reports whether it may proceed.
func (l *LoginLimiter) Allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := l.now()
	if len(l.attempts) >= sweepThreshold {
		for k, a := range l.attempts {
			if now.Sub(a.start) >= l.window {
				delete(l.attempts, k)
			}
		}
	}

	a, ok := l.attempts[key]
	if !ok || now.Sub(a.start) >= l.window {
		a = attemptWindow{start: now}
	}
	a.count++
	l.attempts[key] = a
	return a.count <= l.max
}

// Reset forgets key, so someone who mistyped a few times and then got in
// starts afresh next time.
func (l *LoginLimiter) Reset(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.attempts, key)
}

// clientIP returns the address the request came from. Behind Fly's proxy
// RemoteAddr is the proxy itself, so header names the one the proxy sets
// (Fly-Client-IP). It must only be configured when a proxy is in front:
// otherwise any client could pick its own IP and dodge the limit.
func clientIP(r *http.Request, header string) string {
	if header != "" {
		if v := strings.TrimSpace(r.Header.Get(header)); v != "" {
			return v
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
