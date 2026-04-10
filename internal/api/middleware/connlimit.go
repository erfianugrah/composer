package middleware

import (
	"net/http"
	"sync"
	"sync/atomic"
)

// ConnLimiter limits concurrent connections per user.
type ConnLimiter struct {
	mu         sync.Mutex
	counters   map[string]*int32
	maxPerUser int32
}

// NewConnLimiter creates a limiter with the given per-user max.
func NewConnLimiter(maxPerUser int) *ConnLimiter {
	return &ConnLimiter{
		counters:   make(map[string]*int32),
		maxPerUser: int32(maxPerUser),
	}
}

// Acquire increments the counter for a user. Returns false if at limit.
func (l *ConnLimiter) Acquire(userID string) bool {
	l.mu.Lock()
	counter, ok := l.counters[userID]
	if !ok {
		var c int32
		counter = &c
		l.counters[userID] = counter
	}
	l.mu.Unlock()

	if atomic.AddInt32(counter, 1) > l.maxPerUser {
		atomic.AddInt32(counter, -1)
		return false
	}
	return true
}

// Release decrements the counter for a user.
func (l *ConnLimiter) Release(userID string) {
	l.mu.Lock()
	counter, ok := l.counters[userID]
	l.mu.Unlock()
	if ok {
		atomic.AddInt32(counter, -1)
	}
}

// LimitConnections returns middleware that limits per-user concurrent connections.
// Uses the user ID from the auth context.
func LimitConnections(limiter *ConnLimiter) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			userID := UserIDFromContext(r.Context())
			if userID == "" {
				next.ServeHTTP(w, r)
				return
			}
			if !limiter.Acquire(userID) {
				http.Error(w, "too many concurrent connections", http.StatusTooManyRequests)
				return
			}
			defer limiter.Release(userID)
			next.ServeHTTP(w, r)
		})
	}
}
