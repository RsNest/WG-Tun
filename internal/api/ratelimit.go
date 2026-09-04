package api

import (
	"sync"
	"time"
)

type limiter struct {
	rps   float64
	burst int
	mu    sync.Mutex
	m     map[string]*bucket
}

type bucket struct {
	tokens float64
	last   time.Time
}

func newLimiter(rps float64, burst int) *limiter {
	if rps <= 0 {
		rps = 10
	}
	if burst <= 0 {
		burst = 20
	}
	return &limiter{rps: rps, burst: burst, m: map[string]*bucket{}}
}

func (l *limiter) allow(key string) bool {
	now := time.Now()
	l.mu.Lock()
	defer l.mu.Unlock()
	b := l.m[key]
	if b == nil {
		b = &bucket{tokens: float64(l.burst), last: now}
		l.m[key] = b
	}
	elapsed := now.Sub(b.last).Seconds()
	b.tokens += elapsed * l.rps
	if b.tokens > float64(l.burst) {
		b.tokens = float64(l.burst)
	}
	b.last = now
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}
