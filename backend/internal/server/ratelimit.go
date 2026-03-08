package server

import (
	"net"
	"net/http"
	"sync"
	"time"
)

type fixedWindowRateLimiter struct {
	mu      sync.Mutex
	window  time.Duration
	buckets map[string]bucket
}

type bucket struct {
	windowIndex int64
	count       int
}

func newFixedWindowRateLimiter(window time.Duration) *fixedWindowRateLimiter {
	return &fixedWindowRateLimiter{
		window:  window,
		buckets: make(map[string]bucket),
	}
}

func (l *fixedWindowRateLimiter) allow(key string, limit int, now time.Time) bool {
	if limit <= 0 {
		return true
	}
	windowNanos := l.window.Nanoseconds()
	if windowNanos <= 0 {
		return true
	}
	index := now.UnixNano() / windowNanos

	l.mu.Lock()
	defer l.mu.Unlock()

	b := l.buckets[key]
	if b.windowIndex != index {
		b.windowIndex = index
		b.count = 0
	}
	if b.count >= limit {
		l.buckets[key] = b
		return false
	}
	b.count++
	l.buckets[key] = b
	return true
}

func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	if host == "" {
		return "unknown"
	}
	return host
}
