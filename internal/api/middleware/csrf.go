package middleware

import (
	"net/http"
	"strings"
)

// CSRF returns middleware that enforces X-Requested-With header on
// mutating requests that use cookie-based authentication.
// This prevents cross-site form submissions because browsers cannot
// send custom headers cross-origin without CORS preflight.
func CSRF(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Only enforce on mutating methods
		if r.Method == http.MethodGet || r.Method == http.MethodHead || r.Method == http.MethodOptions {
			next.ServeHTTP(w, r)
			return
		}

		// Only enforce on /api/ paths
		if !strings.HasPrefix(r.URL.Path, "/api/") {
			next.ServeHTTP(w, r)
			return
		}

		// Skip for API key auth (has Authorization or X-API-Key header)
		if r.Header.Get("Authorization") != "" || r.Header.Get("X-API-Key") != "" {
			next.ServeHTTP(w, r)
			return
		}

		// Skip for webhook receiver (signature-validated, not cookie-based)
		if strings.HasPrefix(r.URL.Path, "/api/v1/hooks/") {
			next.ServeHTTP(w, r)
			return
		}

		// Skip for public endpoints (login, bootstrap)
		if r.URL.Path == "/api/v1/auth/login" || r.URL.Path == "/api/v1/auth/bootstrap" {
			next.ServeHTTP(w, r)
			return
		}

		// Cookie-based mutating request: require X-Requested-With header
		if _, hasCookie := r.Cookie("composer_session"); hasCookie == nil {
			if r.Header.Get("X-Requested-With") == "" {
				http.Error(w,
					`{"status":403,"title":"Forbidden","detail":"X-Requested-With header required for cookie-based mutations"}`,
					http.StatusForbidden)
				return
			}
		}

		next.ServeHTTP(w, r)
	})
}
