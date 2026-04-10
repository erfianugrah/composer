package middleware

import (
	"net/http"
	"os"
	"sync"
	"time"
)

// SecurityHeaders adds standard security headers to all responses.
func SecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("X-XSS-Protection", "0") // S31: obsolete header, set to 0 per modern guidance
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
		w.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self' 'unsafe-inline' https://unpkg.com; style-src 'self' 'unsafe-inline' https://unpkg.com; connect-src 'self'; img-src 'self' data:; font-src 'self'; frame-ancestors 'none'")

		// HSTS: only trust X-Forwarded-Proto when behind a trusted proxy (S29)
		isHTTPS := r.TLS != nil
		if os.Getenv("COMPOSER_TRUSTED_PROXIES") != "" {
			isHTTPS = isHTTPS || r.Header.Get("X-Forwarded-Proto") == "https"
		}
		if isHTTPS {
			w.Header().Set("Strict-Transport-Security", "max-age=63072000; includeSubDomains; preload")
		}

		next.ServeHTTP(w, r)
	})
}

// RateLimiter implements per-IP token bucket rate limiting.
type RateLimiter struct {
	mu      sync.Mutex
	buckets map[string]*bucket
	rate    float64 // tokens per second
	burst   int     // max tokens
	done    chan struct{}
}

type bucket struct {
	tokens   float64
	lastTime time.Time
}

// NewRateLimiter creates a rate limiter. rate is requests/second, burst is max burst.
func NewRateLimiter(rate float64, burst int) *RateLimiter {
	rl := &RateLimiter{
		buckets: make(map[string]*bucket),
		rate:    rate,
		burst:   burst,
		done:    make(chan struct{}),
	}

	// Cleanup old buckets every 60 seconds
	go func() {
		ticker := time.NewTicker(60 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				rl.cleanup()
			case <-rl.done:
				return
			}
		}
	}()

	return rl
}

// Close stops the cleanup goroutine.
func (rl *RateLimiter) Close() {
	close(rl.done)
}

// Allow returns true if the request from this IP is allowed.
func (rl *RateLimiter) Allow(ip string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	b, ok := rl.buckets[ip]
	if !ok {
		rl.buckets[ip] = &bucket{tokens: float64(rl.burst) - 1, lastTime: now}
		return true
	}

	// Refill tokens
	elapsed := now.Sub(b.lastTime).Seconds()
	b.tokens += elapsed * rl.rate
	if b.tokens > float64(rl.burst) {
		b.tokens = float64(rl.burst)
	}
	b.lastTime = now

	if b.tokens >= 1 {
		b.tokens--
		return true
	}
	return false
}

func (rl *RateLimiter) cleanup() {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	cutoff := time.Now().Add(-5 * time.Minute)
	for ip, b := range rl.buckets {
		if b.lastTime.Before(cutoff) {
			delete(rl.buckets, ip)
		}
	}
}

// RateLimit returns middleware that limits requests per IP.
// Uses RemoteAddr directly -- X-Real-IP/X-Forwarded-For are set by the
// Chi RealIP middleware which should only be used behind a trusted reverse proxy.
func RateLimit(limiter *RateLimiter) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Use RemoteAddr as-is. Chi's RealIP middleware (applied before this)
			// replaces RemoteAddr with X-Real-IP when configured. This prevents
			// direct clients from spoofing IPs via headers.
			ip := r.RemoteAddr

			if !limiter.Allow(ip) {
				http.Error(w, `{"status":429,"title":"Too Many Requests","detail":"Rate limit exceeded"}`, http.StatusTooManyRequests)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// LoginRateLimit returns a stricter rate limiter for login endpoints (5 req/min).
func LoginRateLimit() *RateLimiter {
	return NewRateLimiter(5.0/60.0, 5) // 5 per minute, burst of 5
}

// GeneralRateLimit returns a general rate limiter (60 req/sec).
func GeneralRateLimit() *RateLimiter {
	return NewRateLimiter(60, 120) // 60/sec, burst 120
}
