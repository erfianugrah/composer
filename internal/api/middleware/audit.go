package middleware

import (
	"context"
	"crypto/rand"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/erfianugrah/composer/internal/infra/store"
)

// Audit returns middleware that logs mutating API operations.
func Audit(repo *store.AuditRepo) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Only audit mutating requests on API paths
			if !strings.HasPrefix(r.URL.Path, "/api/") {
				next.ServeHTTP(w, r)
				return
			}
			if r.Method == http.MethodGet || r.Method == http.MethodHead || r.Method == http.MethodOptions {
				next.ServeHTTP(w, r)
				return
			}

			// Capture response status
			sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}
			next.ServeHTTP(sw, r)

			// Log the action asynchronously (fire-and-forget).
			// Use a detached context because r.Context() is cancelled after the handler returns.
			go func() {
				userID := UserIDFromContext(r.Context())
				action := deriveAction(r.Method, r.URL.Path)
				ip := r.RemoteAddr
				if fwd := r.Header.Get("X-Real-IP"); fwd != "" {
					ip = fwd
				}

				var buf [8]byte
				rand.Read(buf[:])
				id := fmt.Sprintf("aud_%x", buf)

				auditCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				repo.Log(auditCtx, store.AuditEntry{
					ID:        id,
					UserID:    userID,
					Action:    action,
					Resource:  r.URL.Path,
					Detail:    map[string]any{"method": r.Method, "status": sw.status},
					IPAddress: ip,
					CreatedAt: time.Now().UTC(),
				})
			}()
		})
	}
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

func deriveAction(method, path string) string {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	// /api/v1/stacks/mystack/up -> "stack.up"
	// /api/v1/auth/login -> "auth.login"
	if len(parts) >= 3 {
		resource := parts[2]                         // stacks, auth, users, etc.
		resource = strings.TrimSuffix(resource, "s") // stacks -> stack

		if len(parts) >= 5 {
			// /api/v1/stacks/mystack/up -> stack.up
			// /api/v1/users/abc/password -> user.password
			// /api/v1/containers/abc/start -> container.start
			return resource + "." + parts[len(parts)-1]
		}
		if len(parts) == 4 {
			lastPart := parts[3]
			// Check if last part is an action word (not an ID)
			// Auth paths: /api/v1/auth/login, /api/v1/auth/bootstrap, /api/v1/auth/logout
			// These have meaningful last segments, not IDs
			if resource == "auth" || resource == "hook" {
				return resource + "." + lastPart
			}
			switch method {
			case http.MethodPost:
				return resource + ".create"
			case http.MethodPut:
				return resource + ".update"
			case http.MethodDelete:
				return resource + ".delete"
			}
		}
		if len(parts) == 3 {
			return resource + ".create"
		}
	}
	return method + " " + path
}
