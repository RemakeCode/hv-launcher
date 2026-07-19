package server

import (
	"sync"
	"time"
)

type transitionLimiter struct {
	mu      sync.Mutex
	limit   int
	window  time.Duration
	started time.Time
	count   int
}

func newTransitionLimiter(limit int, window time.Duration) *transitionLimiter {
	return &transitionLimiter{limit: limit, window: window, started: time.Now()}
}

func (l *transitionLimiter) Allow() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	if now.Sub(l.started) >= l.window {
		l.started, l.count = now, 0
	}
	if l.count >= l.limit {
		return false
	}
	l.count++
	return true
}
