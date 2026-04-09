//go:build integration

package handler_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"

	"github.com/erfianugrah/composer/internal/api"
	"github.com/erfianugrah/composer/internal/app"
	"github.com/erfianugrah/composer/internal/infra/docker"
	"github.com/erfianugrah/composer/internal/infra/store"
)

func setupStackServer(t *testing.T) *api.Server {
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

	// Docker client for stack operations
	dockerClient, err := docker.NewClient("")
	require.NoError(t, err)
	t.Cleanup(func() { dockerClient.Close() })

	compose := docker.NewCompose(dockerClient.Host())
	stacksDir := t.TempDir()

	stackSvc := app.NewStackService(
		store.NewStackRepo(db.SQL),
		store.NewGitConfigRepo(db.SQL),
		dockerClient,
		compose,
		nil, // no event bus in tests
		stacksDir,
	)

	jobManager := app.NewJobManager()

	return api.NewServer(api.Deps{
		AuthService:  authSvc,
		StackService: stackSvc,
		DockerClient: dockerClient,
		Jobs:         jobManager,
	})
}

// loginAndGetCookie bootstraps a user, logs in, and returns the session cookie header.
func loginAndGetCookie(t *testing.T, srv *api.Server) string {
	t.Helper()
	doRequest(srv, http.MethodPost, "/api/v1/auth/bootstrap",
		`{"email":"admin@test.com","password":"strongpassword1"}`)
	loginResp := doRequest(srv, http.MethodPost, "/api/v1/auth/login",
		`{"email":"admin@test.com","password":"strongpassword1"}`)
	return loginResp.Header().Get("Set-Cookie")
}

func doAuthRequest(srv *api.Server, method, path, body, cookie string) *httptest.ResponseRecorder {
	return doRequest(srv, method, path, body, "Cookie: "+cookie)
}

func TestStack_CRUD(t *testing.T) {
	srv := setupStackServer(t)
	cookie := loginAndGetCookie(t, srv)

	composeContent := `services:
  web:
    image: nginx:alpine
    ports:
      - "29876:80"
`

	// Create
	createResp := doAuthRequest(srv, http.MethodPost, "/api/v1/stacks",
		`{"name":"test-stack","compose":"`+escapeJSON(composeContent)+`"}`, cookie)
	assert.Equal(t, http.StatusOK, createResp.Code, "create body: %s", createResp.Body.String())

	var createBody map[string]any
	json.Unmarshal(createResp.Body.Bytes(), &createBody)
	assert.Equal(t, "test-stack", createBody["name"])
	assert.Equal(t, "local", createBody["source"])

	// List
	listResp := doAuthRequest(srv, http.MethodGet, "/api/v1/stacks", "", cookie)
	assert.Equal(t, http.StatusOK, listResp.Code)

	var listBody map[string]any
	json.Unmarshal(listResp.Body.Bytes(), &listBody)
	stacks := listBody["stacks"].([]any)
	assert.Len(t, stacks, 1)

	// Get
	getResp := doAuthRequest(srv, http.MethodGet, "/api/v1/stacks/test-stack", "", cookie)
	assert.Equal(t, http.StatusOK, getResp.Code)

	var getBody map[string]any
	json.Unmarshal(getResp.Body.Bytes(), &getBody)
	assert.Equal(t, "test-stack", getBody["name"])
	assert.Contains(t, getBody["compose_content"], "nginx:alpine")

	// Update
	newCompose := `services:
  web:
    image: httpd:alpine
    ports:
      - "29877:80"
`
	updateResp := doAuthRequest(srv, http.MethodPut, "/api/v1/stacks/test-stack",
		`{"compose":"`+escapeJSON(newCompose)+`"}`, cookie)
	assert.Equal(t, http.StatusOK, updateResp.Code)

	// Verify update persisted
	getResp2 := doAuthRequest(srv, http.MethodGet, "/api/v1/stacks/test-stack", "", cookie)
	var getBody2 map[string]any
	json.Unmarshal(getResp2.Body.Bytes(), &getBody2)
	assert.Contains(t, getBody2["compose_content"], "httpd:alpine")

	// Delete (huma returns 204 for nil body or 200 depending on return type)
	delResp := doAuthRequest(srv, http.MethodDelete, "/api/v1/stacks/test-stack", "", cookie)
	assert.True(t, delResp.Code == http.StatusNoContent || delResp.Code == http.StatusOK,
		"delete expected 200 or 204, got %d: %s", delResp.Code, delResp.Body.String())

	// Verify gone
	getResp3 := doAuthRequest(srv, http.MethodGet, "/api/v1/stacks/test-stack", "", cookie)
	assert.Equal(t, http.StatusNotFound, getResp3.Code)
}

func TestStack_DeployAndStop(t *testing.T) {
	srv := setupStackServer(t)
	cookie := loginAndGetCookie(t, srv)

	composeContent := `services:
  web:
    image: nginx:alpine
`

	// Create stack
	doAuthRequest(srv, http.MethodPost, "/api/v1/stacks",
		`{"name":"deploy-test","compose":"`+escapeJSON(composeContent)+`"}`, cookie)

	// Deploy
	upResp := doAuthRequest(srv, http.MethodPost, "/api/v1/stacks/deploy-test/up", "", cookie)
	assert.Equal(t, http.StatusOK, upResp.Code, "up body: %s", upResp.Body.String())

	// Verify running via get (status should be "running")
	getResp := doAuthRequest(srv, http.MethodGet, "/api/v1/stacks/deploy-test", "", cookie)
	var getBody map[string]any
	json.Unmarshal(getResp.Body.Bytes(), &getBody)
	assert.Equal(t, "running", getBody["status"])

	// Stop
	downResp := doAuthRequest(srv, http.MethodPost, "/api/v1/stacks/deploy-test/down", "", cookie)
	assert.Equal(t, http.StatusOK, downResp.Code, "down body: %s", downResp.Body.String())

	// Cleanup
	doAuthRequest(srv, http.MethodDelete, "/api/v1/stacks/deploy-test?remove_volumes=true", "", cookie)
}

func TestStack_NotFound(t *testing.T) {
	srv := setupStackServer(t)
	cookie := loginAndGetCookie(t, srv)

	resp := doAuthRequest(srv, http.MethodGet, "/api/v1/stacks/nonexistent", "", cookie)
	assert.Equal(t, http.StatusNotFound, resp.Code)
}

func TestStack_Unauthenticated(t *testing.T) {
	srv := setupStackServer(t)

	// No cookie -- should be 401
	resp := doRequest(srv, http.MethodGet, "/api/v1/stacks", "")
	assert.Equal(t, http.StatusUnauthorized, resp.Code)
}

func TestStack_OpenAPI_StackEndpoints(t *testing.T) {
	srv := setupStackServer(t)

	resp := doRequest(srv, http.MethodGet, "/openapi.json", "")
	var spec map[string]any
	json.Unmarshal(resp.Body.Bytes(), &spec)

	paths := spec["paths"].(map[string]any)
	assert.Contains(t, paths, "/api/v1/stacks")
	assert.Contains(t, paths, "/api/v1/stacks/{name}")
	assert.Contains(t, paths, "/api/v1/stacks/{name}/up")
	assert.Contains(t, paths, "/api/v1/stacks/{name}/down")
	assert.Contains(t, paths, "/api/v1/stacks/{name}/restart")
	assert.Contains(t, paths, "/api/v1/stacks/{name}/pull")
}

// escapeJSON escapes a string for embedding in a JSON string value.
func escapeJSON(s string) string {
	b, _ := json.Marshal(s)
	// Strip surrounding quotes
	return string(b[1 : len(b)-1])
}
