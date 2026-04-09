//go:build integration

package handler_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"

	"github.com/erfianugrah/composer/internal/api"
	"github.com/erfianugrah/composer/internal/app"
	"github.com/erfianugrah/composer/internal/infra/store"
)

func setupTestServer(t *testing.T) *api.Server {
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

	return api.NewServer(api.Deps{AuthService: authSvc})
}

func doRequest(srv *api.Server, method, path string, body string, headers ...string) *httptest.ResponseRecorder {
	var reader *strings.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	var req *http.Request
	if reader != nil {
		req = httptest.NewRequest(method, path, reader)
	} else {
		req = httptest.NewRequest(method, path, nil)
	}
	req.Header.Set("Content-Type", "application/json")
	// CSRF: mutating requests with cookies need X-Requested-With
	if method != http.MethodGet && method != http.MethodHead {
		req.Header.Set("X-Requested-With", "XMLHttpRequest")
	}
	for _, h := range headers {
		parts := strings.SplitN(h, ": ", 2)
		if len(parts) == 2 {
			req.Header.Set(parts[0], parts[1])
		}
	}
	w := httptest.NewRecorder()
	srv.Router.ServeHTTP(w, req)
	return w
}

func TestHandler_HealthCheck(t *testing.T) {
	srv := setupTestServer(t)

	resp := doRequest(srv, http.MethodGet, "/api/v1/system/health", "")
	assert.Equal(t, http.StatusOK, resp.Code)

	var body map[string]any
	json.Unmarshal(resp.Body.Bytes(), &body)
	assert.Equal(t, "healthy", body["status"])
	assert.Equal(t, "0.2.2", body["version"])
}

func TestHandler_Bootstrap(t *testing.T) {
	srv := setupTestServer(t)

	// First bootstrap succeeds
	resp := doRequest(srv, http.MethodPost, "/api/v1/auth/bootstrap",
		`{"email":"admin@test.com","password":"strongpassword1"}`)
	assert.Equal(t, http.StatusOK, resp.Code)

	var body map[string]any
	json.Unmarshal(resp.Body.Bytes(), &body)
	assert.Equal(t, "admin@test.com", body["email"])
	assert.Equal(t, "admin", body["role"])
	assert.NotEmpty(t, body["id"])

	// Second bootstrap fails with 409
	resp2 := doRequest(srv, http.MethodPost, "/api/v1/auth/bootstrap",
		`{"email":"other@test.com","password":"strongpassword1"}`)
	assert.Equal(t, http.StatusConflict, resp2.Code)
}

func TestHandler_Bootstrap_Validation(t *testing.T) {
	srv := setupTestServer(t)

	// Short password -- huma validates minLength:8 from the DTO tag
	resp := doRequest(srv, http.MethodPost, "/api/v1/auth/bootstrap",
		`{"email":"admin@test.com","password":"short"}`)
	assert.True(t, resp.Code == http.StatusUnprocessableEntity || resp.Code == http.StatusBadRequest,
		"expected 422 or 400, got %d: %s", resp.Code, resp.Body.String())
}

func TestHandler_LoginLogout(t *testing.T) {
	srv := setupTestServer(t)

	// Bootstrap first
	doRequest(srv, http.MethodPost, "/api/v1/auth/bootstrap",
		`{"email":"user@test.com","password":"mypassword123"}`)

	// Login
	loginResp := doRequest(srv, http.MethodPost, "/api/v1/auth/login",
		`{"email":"user@test.com","password":"mypassword123"}`)
	assert.Equal(t, http.StatusOK, loginResp.Code)

	var loginBody map[string]any
	json.Unmarshal(loginResp.Body.Bytes(), &loginBody)
	assert.Equal(t, "admin", loginBody["role"])
	assert.NotEmpty(t, loginBody["user_id"])

	// Verify Set-Cookie header
	setCookie := loginResp.Header().Get("Set-Cookie")
	assert.Contains(t, setCookie, "composer_session=")
	assert.Contains(t, setCookie, "HttpOnly")
}

func TestHandler_Login_InvalidCredentials(t *testing.T) {
	srv := setupTestServer(t)

	doRequest(srv, http.MethodPost, "/api/v1/auth/bootstrap",
		`{"email":"user@test.com","password":"mypassword123"}`)

	// Wrong password
	resp := doRequest(srv, http.MethodPost, "/api/v1/auth/login",
		`{"email":"user@test.com","password":"wrongpassword"}`)
	assert.Equal(t, http.StatusUnauthorized, resp.Code)
}

func TestHandler_Session_Unauthenticated(t *testing.T) {
	srv := setupTestServer(t)

	// No session cookie, no API key -> 401
	resp := doRequest(srv, http.MethodGet, "/api/v1/auth/session", "")
	assert.Equal(t, http.StatusUnauthorized, resp.Code)
}

func TestHandler_Session_WithCookie(t *testing.T) {
	srv := setupTestServer(t)

	// Bootstrap + login
	doRequest(srv, http.MethodPost, "/api/v1/auth/bootstrap",
		`{"email":"user@test.com","password":"mypassword123"}`)
	loginResp := doRequest(srv, http.MethodPost, "/api/v1/auth/login",
		`{"email":"user@test.com","password":"mypassword123"}`)

	// Extract session cookie
	setCookie := loginResp.Header().Get("Set-Cookie")
	require.Contains(t, setCookie, "composer_session=")

	// Use cookie to access session endpoint
	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/session", nil)
	req.Header.Set("Cookie", setCookie)
	w := httptest.NewRecorder()
	srv.Router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var body map[string]any
	json.Unmarshal(w.Body.Bytes(), &body)
	assert.Equal(t, "admin", body["role"])
	assert.NotEmpty(t, body["user_id"])
}

func TestHandler_OpenAPI(t *testing.T) {
	srv := setupTestServer(t)

	resp := doRequest(srv, http.MethodGet, "/openapi.json", "")
	assert.Equal(t, http.StatusOK, resp.Code)

	var spec map[string]any
	err := json.Unmarshal(resp.Body.Bytes(), &spec)
	require.NoError(t, err)

	// Verify it's an OpenAPI 3.1 spec
	assert.Equal(t, "3.1.0", spec["openapi"])

	info := spec["info"].(map[string]any)
	assert.Equal(t, "Composer", info["title"])
	assert.Equal(t, "0.2.2", info["version"])

	// Verify our endpoints are in the spec
	paths := spec["paths"].(map[string]any)
	assert.Contains(t, paths, "/api/v1/auth/bootstrap")
	assert.Contains(t, paths, "/api/v1/auth/login")
	assert.Contains(t, paths, "/api/v1/auth/session")
	assert.Contains(t, paths, "/api/v1/system/health")
}
