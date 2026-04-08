//go:build integration

package middleware_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"

	"github.com/erfianugrah/composer/internal/api/middleware"
	"github.com/erfianugrah/composer/internal/app"
	"github.com/erfianugrah/composer/internal/domain/auth"
	"github.com/erfianugrah/composer/internal/infra/store"
)

func setupMiddlewareTest(t *testing.T) (*app.AuthService, chi.Router) {
	t.Helper()
	ctx := context.Background()

	pgCtr, err := tcpostgres.Run(ctx,
		"postgres:17-alpine",
		tcpostgres.WithDatabase("composer_test"),
		tcpostgres.WithUsername("test"),
		tcpostgres.WithPassword("test"),
		tcpostgres.BasicWaitStrategies(),
		tcpostgres.WithSQLDriver("pgx"),
	)
	require.NoError(t, err)
	t.Cleanup(func() { pgCtr.Terminate(context.Background()) })

	connStr, err := pgCtr.ConnectionString(ctx, "sslmode=disable")
	require.NoError(t, err)

	db, err := store.New(ctx, connStr, "")
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })

	authSvc := app.NewAuthService(
		store.NewUserRepo(db.SQL),
		store.NewSessionRepo(db.SQL),
		store.NewAPIKeyRepo(db.SQL),
	)

	router := chi.NewMux()
	router.Use(middleware.Auth(authSvc))

	// Test endpoint that returns the role from context
	router.Get("/api/v1/test/protected", func(w http.ResponseWriter, r *http.Request) {
		role := middleware.RoleFromContext(r.Context())
		userID := middleware.UserIDFromContext(r.Context())
		w.Write([]byte(string(role) + ":" + userID))
	})

	// Public bypass path
	router.Get("/api/v1/system/health", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("ok"))
	})
	router.Post("/api/v1/auth/login", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("login"))
	})

	return authSvc, router
}

func TestAuthMiddleware_BypassPaths(t *testing.T) {
	_, router := setupMiddlewareTest(t)

	tests := []struct {
		method string
		path   string
	}{
		{"GET", "/api/v1/system/health"},
		{"POST", "/api/v1/auth/login"},
		{"POST", "/api/v1/auth/bootstrap"},
		{"GET", "/openapi.json"},
		{"GET", "/openapi.yaml"},
		{"GET", "/docs"},
	}

	for _, tt := range tests {
		t.Run(tt.method+" "+tt.path, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, tt.path, nil)
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)
			// Should NOT be 401 (bypass paths skip auth)
			assert.NotEqual(t, http.StatusUnauthorized, w.Code, "path %s should bypass auth", tt.path)
		})
	}
}

func TestAuthMiddleware_Unauthenticated(t *testing.T) {
	_, router := setupMiddlewareTest(t)

	req := httptest.NewRequest("GET", "/api/v1/test/protected", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestAuthMiddleware_ValidSessionCookie(t *testing.T) {
	authSvc, router := setupMiddlewareTest(t)
	ctx := context.Background()

	// Create user + session
	authSvc.Bootstrap(ctx, "test@example.com", "strongpassword1")
	session, err := authSvc.Login(ctx, "test@example.com", "strongpassword1", 24*time.Hour)
	require.NoError(t, err)

	req := httptest.NewRequest("GET", "/api/v1/test/protected", nil)
	req.AddCookie(&http.Cookie{Name: "composer_session", Value: session.ID})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "admin:")
}

func TestAuthMiddleware_ExpiredSessionCookie(t *testing.T) {
	authSvc, router := setupMiddlewareTest(t)
	ctx := context.Background()

	// Create user + session with very short TTL
	authSvc.Bootstrap(ctx, "test@example.com", "strongpassword1")
	session, _ := authSvc.Login(ctx, "test@example.com", "strongpassword1", time.Millisecond)

	// Wait for it to expire
	time.Sleep(10 * time.Millisecond)

	req := httptest.NewRequest("GET", "/api/v1/test/protected", nil)
	req.AddCookie(&http.Cookie{Name: "composer_session", Value: session.ID})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestAuthMiddleware_InvalidSessionCookie(t *testing.T) {
	_, router := setupMiddlewareTest(t)

	req := httptest.NewRequest("GET", "/api/v1/test/protected", nil)
	req.AddCookie(&http.Cookie{Name: "composer_session", Value: "totally-invalid-token"})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestAuthMiddleware_ValidAPIKey_BearerHeader(t *testing.T) {
	authSvc, router := setupMiddlewareTest(t)
	ctx := context.Background()

	user, _ := authSvc.Bootstrap(ctx, "test@example.com", "strongpassword1")
	key, err := authSvc.CreateAPIKey(ctx, "test-key", auth.RoleOperator, user.ID, nil)
	require.NoError(t, err)

	req := httptest.NewRequest("GET", "/api/v1/test/protected", nil)
	req.Header.Set("Authorization", "Bearer "+key.PlaintextKey)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "operator:")
}

func TestAuthMiddleware_ValidAPIKey_XAPIKeyHeader(t *testing.T) {
	authSvc, router := setupMiddlewareTest(t)
	ctx := context.Background()

	user, _ := authSvc.Bootstrap(ctx, "test@example.com", "strongpassword1")
	key, _ := authSvc.CreateAPIKey(ctx, "test-key", auth.RoleViewer, user.ID, nil)

	req := httptest.NewRequest("GET", "/api/v1/test/protected", nil)
	req.Header.Set("X-API-Key", key.PlaintextKey)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "viewer:")
}

func TestAuthMiddleware_InvalidAPIKey(t *testing.T) {
	_, router := setupMiddlewareTest(t)

	req := httptest.NewRequest("GET", "/api/v1/test/protected", nil)
	req.Header.Set("Authorization", "Bearer ck_invalid_key_value_here_00000000")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestAuthMiddleware_ExpiredAPIKey(t *testing.T) {
	authSvc, router := setupMiddlewareTest(t)
	ctx := context.Background()

	user, _ := authSvc.Bootstrap(ctx, "test@example.com", "strongpassword1")
	past := time.Now().UTC().Add(-time.Hour)
	key, _ := authSvc.CreateAPIKey(ctx, "expired-key", auth.RoleAdmin, user.ID, &past)

	req := httptest.NewRequest("GET", "/api/v1/test/protected", nil)
	req.Header.Set("Authorization", "Bearer "+key.PlaintextKey)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestAuthMiddleware_WebhookBypassPrefix(t *testing.T) {
	_, router := setupMiddlewareTest(t)

	// Register a handler for webhook path
	router.Post("/api/v1/hooks/{id}", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("webhook"))
	})

	req := httptest.NewRequest("POST", "/api/v1/hooks/some-webhook-id", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	// Should NOT be 401 -- webhooks bypass auth (validated by signature instead)
	assert.NotEqual(t, http.StatusUnauthorized, w.Code)
}

func TestRequireRole_Enforcement(t *testing.T) {
	authSvc, _ := setupMiddlewareTest(t)
	ctx := context.Background()

	// Create a viewer user
	authSvc.Bootstrap(ctx, "viewer@example.com", "strongpassword1")
	// Change role to viewer (bootstrap creates admin)
	// We'll test via the CheckRole helper directly

	tests := []struct {
		name    string
		role    auth.Role
		minRole auth.Role
		wantErr bool
	}{
		{"admin accessing admin", auth.RoleAdmin, auth.RoleAdmin, false},
		{"admin accessing operator", auth.RoleAdmin, auth.RoleOperator, false},
		{"admin accessing viewer", auth.RoleAdmin, auth.RoleViewer, false},
		{"operator accessing admin", auth.RoleOperator, auth.RoleAdmin, true},
		{"operator accessing operator", auth.RoleOperator, auth.RoleOperator, false},
		{"operator accessing viewer", auth.RoleOperator, auth.RoleViewer, false},
		{"viewer accessing admin", auth.RoleViewer, auth.RoleAdmin, true},
		{"viewer accessing operator", auth.RoleViewer, auth.RoleOperator, true},
		{"viewer accessing viewer", auth.RoleViewer, auth.RoleViewer, false},
		{"empty role", auth.Role(""), auth.RoleViewer, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Build a context with the role
			ctx := context.Background()
			if tt.role != "" {
				ctx = context.WithValue(ctx, middleware.TestRoleKey(), tt.role)
			}
			err := middleware.CheckRole(ctx, tt.minRole)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}
