package handler

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestServerError_ReturnsGenericMessage(t *testing.T) {
	// Should never leak internal error details
	err := serverError(errors.New("pq: connection refused to 10.0.0.5:5432"))
	assert.NotNil(t, err)
	assert.Contains(t, err.Error(), "internal error")
	assert.NotContains(t, err.Error(), "10.0.0.5")
	assert.NotContains(t, err.Error(), "connection refused")
	assert.NotContains(t, err.Error(), "pq:")
}

func TestServerError_NilError(t *testing.T) {
	err := serverError(nil)
	assert.NotNil(t, err)
	assert.Contains(t, err.Error(), "internal error")
}

func TestServerError_DockerSocketError(t *testing.T) {
	err := serverError(errors.New("dial unix /var/run/docker.sock: permission denied"))
	assert.NotContains(t, err.Error(), "docker.sock")
	assert.NotContains(t, err.Error(), "permission denied")
}

func TestServerError_FilesystemPathError(t *testing.T) {
	err := serverError(errors.New("open /opt/stacks/mystack/compose.yaml: no such file"))
	assert.NotContains(t, err.Error(), "/opt/stacks")
	assert.NotContains(t, err.Error(), "compose.yaml")
}
