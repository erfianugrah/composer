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

	"github.com/erfianugrah/composer/internal/api"
	"github.com/erfianugrah/composer/internal/app"
	"github.com/erfianugrah/composer/internal/domain/auth"
	"github.com/erfianugrah/composer/internal/infra/store"
)

// newRotateTestServer builds a real server (SQLite, real auth + rotate
// services) for the encryption-key endpoint tests.
func newRotateTestServer(t *testing.T) (*api.Server, *store.DB) {
	t.Helper()
	ctx := context.Background()
	dataDir := t.TempDir()

	db, err := store.New(ctx, "", dataDir)
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })

	authSvc := app.NewAuthService(
		store.NewUserRepo(db.SQL),
		store.NewSessionRepo(db.SQL),
		store.NewAPIKeyRepo(db.SQL),
	)
	rotateSvc := app.NewRotateService(db.SQL, dataDir, nil, nil)

	srv := api.NewServer(api.Deps{
		AuthService:   authSvc,
		RotateService: rotateSvc,
		DataDir:       dataDir,
	})
	return srv, db
}

func rotateDo(t *testing.T, srv *api.Server, method, path, body, cookie string) *httptest.ResponseRecorder {
	t.Helper()
	var req *http.Request
	if body != "" {
		req = httptest.NewRequest(method, path, strings.NewReader(body))
	} else {
		req = httptest.NewRequest(method, path, nil)
	}
	req.Header.Set("Content-Type", "application/json")
	// CSRF: mutating requests with cookies need X-Requested-With
	if method != http.MethodGet && method != http.MethodHead {
		req.Header.Set("X-Requested-With", "XMLHttpRequest")
	}
	if cookie != "" {
		req.Header.Set("Cookie", cookie)
	}
	w := httptest.NewRecorder()
	srv.Router.ServeHTTP(w, req)
	return w
}

func rotateLoginCookie(t *testing.T, srv *api.Server, email, password string) string {
	t.Helper()
	resp := rotateDo(t, srv, http.MethodPost, "/api/v1/auth/login",
		`{"email":"`+email+`","password":"`+password+`"}`, "")
	require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())
	cookie := resp.Header().Get("Set-Cookie")
	require.Contains(t, cookie, "composer_session=")
	return cookie
}

func rotateMustUser(t *testing.T, email string, role auth.Role) *auth.User {
	t.Helper()
	u, err := auth.NewUser(email, "password123", role)
	require.NoError(t, err)
	return u
}

// TestRotateEndpointViewerForbidden: the rotate endpoint is admin-only; a
// viewer session gets 403 and no key material is returned.
func TestRotateEndpointViewerForbidden(t *testing.T) {
	ctx := context.Background()
	srv, db := newRotateTestServer(t)

	require.NoError(t, store.NewUserRepo(db.SQL).Create(ctx, rotateMustUser(t, "admin-rotate@test.com", auth.RoleAdmin)))
	viewer := rotateMustUser(t, "viewer-rotate@test.com", auth.RoleViewer)
	require.NoError(t, store.NewUserRepo(db.SQL).Create(ctx, viewer))

	cookie := rotateLoginCookie(t, srv, viewer.Email, "password123")
	resp := rotateDo(t, srv, http.MethodPost, "/api/v1/system/config/encryption-key/rotate", "{}", cookie)
	assert.Equal(t, http.StatusForbidden, resp.Code, resp.Body.String())
	assert.NotContains(t, resp.Body.String(), "new_key")
}

// TestRotateEndpointUnauthenticated: no session -> 401.
func TestRotateEndpointUnauthenticated(t *testing.T) {
	srv, _ := newRotateTestServer(t)
	resp := rotateDo(t, srv, http.MethodPost, "/api/v1/system/config/encryption-key/rotate", "{}", "")
	assert.Equal(t, http.StatusUnauthorized, resp.Code, resp.Body.String())
}

// TestRotateEndpointAdmin: an admin rotation with an empty body generates a
// fresh key and returns it exactly once.
func TestRotateEndpointAdmin(t *testing.T) {
	ctx := context.Background()
	srv, db := newRotateTestServer(t)
	require.NoError(t, store.NewUserRepo(db.SQL).Create(ctx, rotateMustUser(t, "admin-rotate@test.com", auth.RoleAdmin)))

	cookie := rotateLoginCookie(t, srv, "admin-rotate@test.com", "password123")
	resp := rotateDo(t, srv, http.MethodPost, "/api/v1/system/config/encryption-key/rotate", "{}", cookie)
	require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())

	var body struct {
		Rotated bool   `json:"rotated"`
		NewKey  string `json:"new_key"`
	}
	require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &body))
	assert.True(t, body.Rotated)
	assert.Len(t, body.NewKey, 64)
}

// TestRotateEndpointAdminBadKey: malformed key hex -> 422, and no key
// material in the error.
func TestRotateEndpointAdminBadKey(t *testing.T) {
	ctx := context.Background()
	srv, db := newRotateTestServer(t)
	require.NoError(t, store.NewUserRepo(db.SQL).Create(ctx, rotateMustUser(t, "admin-rotate@test.com", auth.RoleAdmin)))

	cookie := rotateLoginCookie(t, srv, "admin-rotate@test.com", "password123")
	resp := rotateDo(t, srv, http.MethodPost, "/api/v1/system/config/encryption-key/rotate", `{"key":"not-hex"}`, cookie)
	assert.Equal(t, http.StatusUnprocessableEntity, resp.Code, resp.Body.String())
	assert.NotContains(t, resp.Body.String(), "not-hex")
}

// TestRotateEndpointConfigSource: after a rotation the config endpoint
// reports the key source as "file" (new precedence: file > env > generated).
func TestRotateEndpointConfigSource(t *testing.T) {
	ctx := context.Background()
	srv, db := newRotateTestServer(t)
	require.NoError(t, store.NewUserRepo(db.SQL).Create(ctx, rotateMustUser(t, "admin-rotate@test.com", auth.RoleAdmin)))

	cookie := rotateLoginCookie(t, srv, "admin-rotate@test.com", "password123")

	// Rotate first (writes the key file)
	resp := rotateDo(t, srv, http.MethodPost, "/api/v1/system/config/encryption-key/rotate", "{}", cookie)
	require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())

	// Config now reports the file as the source
	cfg := rotateDo(t, srv, http.MethodGet, "/api/v1/system/config", "", cookie)
	require.Equal(t, http.StatusOK, cfg.Code, cfg.Body.String())
	var body struct {
		EncryptionKey string `json:"encryption_key"`
	}
	require.NoError(t, json.Unmarshal(cfg.Body.Bytes(), &body))
	assert.Equal(t, "file", body.EncryptionKey)
}
