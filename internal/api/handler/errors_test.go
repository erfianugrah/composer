package handler

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"

	chimiddleware "github.com/go-chi/chi/v5/middleware"
)

func TestServerError_ReturnsGenericMessage(t *testing.T) {
	// Should never leak internal error details
	err := serverError(context.Background(), errors.New("pq: connection refused to 10.0.0.5:5432"))
	assert.NotNil(t, err)
	assert.Contains(t, err.Error(), "internal error")
	assert.NotContains(t, err.Error(), "10.0.0.5")
	assert.NotContains(t, err.Error(), "connection refused")
	assert.NotContains(t, err.Error(), "pq:")
}

func TestServerError_NilError(t *testing.T) {
	err := serverError(context.Background(), nil)
	assert.NotNil(t, err)
	assert.Contains(t, err.Error(), "internal error")
}

func TestServerError_DockerSocketError(t *testing.T) {
	err := serverError(context.Background(), errors.New("dial unix /var/run/docker.sock: permission denied"))
	assert.NotContains(t, err.Error(), "docker.sock")
	assert.NotContains(t, err.Error(), "permission denied")
}

func TestServerError_FilesystemPathError(t *testing.T) {
	err := serverError(context.Background(), errors.New("open /opt/stacks/mystack/compose.yaml: no such file"))
	assert.NotContains(t, err.Error(), "/opt/stacks")
	assert.NotContains(t, err.Error(), "compose.yaml")
}

func TestServerError_IncludesRequestID(t *testing.T) {
	// When request ID is in context, it's surfaced in the client message
	// so operators can correlate with server logs.
	ctx := context.WithValue(context.Background(), chimiddleware.RequestIDKey, "req_abc123")
	err := serverError(ctx, errors.New("db: disk I/O error"))
	assert.Contains(t, err.Error(), "request_id: req_abc123")
	// But never leaks the underlying error
	assert.NotContains(t, err.Error(), "disk I/O")
}

func TestDockerError_SurfacesOperationalMessage(t *testing.T) {
	err := dockerError(context.Background(), errors.New(
		`Error response from daemon: could not select device driver "nvidia" with capabilities: [[gpu compute video]]`,
	))
	assert.NotNil(t, err)
	assert.Contains(t, err.Error(), `could not select device driver "nvidia"`)
	assert.NotContains(t, err.Error(), "Error response from daemon:")
}

func TestDockerError_PortConflict(t *testing.T) {
	err := dockerError(context.Background(), errors.New(
		"Error response from daemon: driver failed programming external connectivity: Bind for 0.0.0.0:8080 failed: port is already allocated",
	))
	assert.Contains(t, err.Error(), "port is already allocated")
}

func TestDockerError_NoPrefix(t *testing.T) {
	// Non-daemon errors pass through as-is
	err := dockerError(context.Background(), errors.New("container is not running"))
	assert.Contains(t, err.Error(), "container is not running")
}

func TestComposeError_SurfacesStderr(t *testing.T) {
	// docker compose subprocess stderr should reach authenticated operators
	// so they can diagnose pull denials / port conflicts / bad YAML from the UI.
	err := composeError(
		context.Background(),
		errors.New("exit status 1"),
		"ERROR: pull access denied for foo/bar, repository does not exist",
	)
	assert.Contains(t, err.Error(), "pull access denied")
	// When stderr is present we don't need the request_id correlation —
	// the user can already see what went wrong.
	assert.NotContains(t, err.Error(), "request_id")
	assert.NotContains(t, err.Error(), "an internal error occurred")
	// Underlying go error string never leaks (only stderr does).
	assert.NotContains(t, err.Error(), "exit status 1")
}

func TestComposeError_TrimsWhitespace(t *testing.T) {
	err := composeError(
		context.Background(),
		errors.New("exit status 1"),
		"\n\nERROR: invalid compose file\n\n",
	)
	assert.Contains(t, err.Error(), "ERROR: invalid compose file")
	// Extra surrounding newlines should be stripped.
	assert.NotContains(t, err.Error(), "\n\nERROR")
}

func TestComposeError_EmptyStderr_FallsBackToRequestID(t *testing.T) {
	// Failures before compose runs (sops decrypt, registry auth, lock acquisition)
	// produce no stderr — fall back to the sanitized + request_id shape so the
	// underlying error doesn't leak.
	ctx := context.WithValue(context.Background(), chimiddleware.RequestIDKey, "req_xyz")
	err := composeError(ctx, errors.New("sops: failed to decrypt /opt/stacks/foo/.env"), "")
	assert.Contains(t, err.Error(), "request_id: req_xyz")
	assert.Contains(t, err.Error(), "an internal error occurred")
	// Path / underlying error must NOT leak when falling back.
	assert.NotContains(t, err.Error(), "/opt/stacks")
	assert.NotContains(t, err.Error(), "sops:")
}

func TestComposeError_WhitespaceOnlyStderr_FallsBack(t *testing.T) {
	// Whitespace-only stderr should be treated as empty.
	ctx := context.WithValue(context.Background(), chimiddleware.RequestIDKey, "req_abc")
	err := composeError(ctx, errors.New("x"), "  \n\t  \n")
	assert.Contains(t, err.Error(), "request_id: req_abc")
	assert.Contains(t, err.Error(), "an internal error occurred")
}

func TestComposeError_NoRequestID_NoStderr(t *testing.T) {
	err := composeError(context.Background(), errors.New("x"), "")
	assert.Contains(t, err.Error(), "an internal error occurred")
	assert.NotContains(t, err.Error(), "request_id")
}
