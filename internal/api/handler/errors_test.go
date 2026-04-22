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
