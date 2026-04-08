package middleware

import (
	"context"
	"net/http"
	"strings"

	"github.com/erfianugrah/composer/internal/app"
	"github.com/erfianugrah/composer/internal/domain/auth"
)

type contextKey string

const (
	ctxSession   contextKey = "session"
	ctxSessionID contextKey = "session_id"
	ctxAPIKey    contextKey = "apikey"
	ctxRole      contextKey = "role"
	ctxUserID    contextKey = "user_id"
)

// bypassPaths are public endpoints that skip authentication.
var bypassPaths = map[string]bool{
	"/api/v1/system/health":  true,
	"/api/v1/auth/bootstrap": true,
	"/api/v1/auth/login":     true,
	"/openapi.json":          true,
	"/openapi.yaml":          true,
	"/docs":                  true,
}

// bypassPrefixes are path prefixes that skip authentication.
var bypassPrefixes = []string{
	"/api/v1/hooks/", // inbound webhooks (validated by signature, not session)
}

// Auth returns middleware that validates session cookies or API key headers.
func Auth(authSvc *app.AuthService) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Bypass paths
			if shouldBypass(r) {
				next.ServeHTTP(w, r)
				return
			}

			// Try session cookie first
			if cookie, err := r.Cookie("composer_session"); err == nil && cookie.Value != "" {
				session, err := authSvc.ValidateSession(r.Context(), cookie.Value)
				if err == nil && session != nil {
					ctx := withAuthContext(r.Context(), session.UserID, session.Role)
					ctx = context.WithValue(ctx, ctxSession, session)
					ctx = context.WithValue(ctx, ctxSessionID, session.ID)
					next.ServeHTTP(w, r.WithContext(ctx))
					return
				}
			}

			// Try API key header
			if key := extractAPIKey(r); key != "" {
				apiKey, err := authSvc.ValidateAPIKey(r.Context(), key)
				if err == nil && apiKey != nil {
					ctx := withAuthContext(r.Context(), apiKey.CreatedBy, apiKey.Role)
					ctx = context.WithValue(ctx, ctxAPIKey, apiKey)
					next.ServeHTTP(w, r.WithContext(ctx))
					return
				}
			}

			// Unauthenticated
			http.Error(w, `{"status":401,"title":"Unauthorized","detail":"Valid session or API key required"}`, http.StatusUnauthorized)
		})
	}
}

// RequireRole returns middleware that checks the authenticated user has at least the given role.
func RequireRole(minRole auth.Role) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			role := RoleFromContext(r.Context())
			if !role.AtLeast(minRole) {
				http.Error(w,
					`{"status":403,"title":"Forbidden","detail":"Insufficient permissions"}`,
					http.StatusForbidden)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// RoleFromContext retrieves the authenticated user's role from the request context.
func RoleFromContext(ctx context.Context) auth.Role {
	if v, ok := ctx.Value(ctxRole).(auth.Role); ok {
		return v
	}
	return ""
}

// UserIDFromContext retrieves the authenticated user's ID from the request context.
func UserIDFromContext(ctx context.Context) string {
	if v, ok := ctx.Value(ctxUserID).(string); ok {
		return v
	}
	return ""
}

// SessionIDFromContext retrieves the session token from the request context.
// Empty string if authenticated via API key or not authenticated.
func SessionIDFromContext(ctx context.Context) string {
	if v, ok := ctx.Value(ctxSessionID).(string); ok {
		return v
	}
	return ""
}

// TestRoleKey returns the context key used for role storage, for use in tests.
func TestRoleKey() contextKey {
	return ctxRole
}

func withAuthContext(ctx context.Context, userID string, role auth.Role) context.Context {
	ctx = context.WithValue(ctx, ctxUserID, userID)
	ctx = context.WithValue(ctx, ctxRole, role)
	return ctx
}

func shouldBypass(r *http.Request) bool {
	path := r.URL.Path
	if bypassPaths[path] {
		return true
	}
	for _, prefix := range bypassPrefixes {
		if strings.HasPrefix(path, prefix) {
			return true
		}
	}
	return false
}

func extractAPIKey(r *http.Request) string {
	// Authorization: Bearer ck_...
	if authHeader := r.Header.Get("Authorization"); authHeader != "" {
		if strings.HasPrefix(authHeader, "Bearer ") {
			return strings.TrimPrefix(authHeader, "Bearer ")
		}
	}
	// X-API-Key: ck_...
	if key := r.Header.Get("X-API-Key"); key != "" {
		return key
	}
	return ""
}
