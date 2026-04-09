//go:build integration

package docker_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/erfianugrah/composer/internal/infra/docker"
)

func TestClient_Ping(t *testing.T) {
	c, err := docker.NewClient("")
	require.NoError(t, err)
	defer c.Close()

	err = c.Ping(context.Background())
	require.NoError(t, err)
}

func TestClient_Info(t *testing.T) {
	c, err := docker.NewClient("")
	require.NoError(t, err)
	defer c.Close()

	info, err := c.Info(context.Background())
	require.NoError(t, err)
	assert.NotEmpty(t, info.ServerVersion)
	t.Logf("Docker version: %s, runtime: %s", info.ServerVersion, c.Runtime())
}

func TestClient_ListContainers(t *testing.T) {
	c, err := docker.NewClient("")
	require.NoError(t, err)
	defer c.Close()

	// List all containers (may be empty on a clean machine, but should not error)
	containers, err := c.ListContainers(context.Background(), "")
	require.NoError(t, err)
	t.Logf("Found %d containers", len(containers))
}

func TestCompose_ValidateAndUp(t *testing.T) {
	// Create a temp compose file
	dir := t.TempDir()
	compose := `services:
  test-nginx:
    image: nginx:alpine
    ports:
      - "19876:80"
`
	err := os.WriteFile(filepath.Join(dir, "compose.yaml"), []byte(compose), 0644)
	require.NoError(t, err)

	c, err := docker.NewClient("")
	require.NoError(t, err)
	defer c.Close()

	comp := docker.NewCompose(c.Host(), nil)
	ctx := context.Background()

	// Validate
	result, err := comp.Validate(ctx, dir)
	require.NoError(t, err, "validate stderr: %s", result.Stderr)

	// Up
	result, err = comp.Up(ctx, dir, "")
	require.NoError(t, err, "up stderr: %s", result.Stderr)
	t.Logf("compose up stdout: %s", result.Stdout)

	// Verify container is running
	containers, err := c.ListContainers(ctx, "")
	require.NoError(t, err)

	found := false
	for _, ctr := range containers {
		if ctr.Image == "nginx:alpine" && ctr.IsRunning() {
			found = true
			t.Logf("Found running container: %s (%s)", ctr.Name, ctr.ID)
			break
		}
	}
	assert.True(t, found, "expected to find running nginx:alpine container")

	// Down
	result, err = comp.Down(ctx, dir, "", true)
	require.NoError(t, err, "down stderr: %s", result.Stderr)
	t.Logf("compose down stdout: %s", result.Stdout)
}

func TestCompose_ValidateInvalid(t *testing.T) {
	dir := t.TempDir()
	// Invalid YAML
	err := os.WriteFile(filepath.Join(dir, "compose.yaml"), []byte("this is not valid yaml: ["), 0644)
	require.NoError(t, err)

	c, err := docker.NewClient("")
	require.NoError(t, err)
	defer c.Close()

	comp := docker.NewCompose(c.Host(), nil)

	_, err = comp.Validate(context.Background(), dir)
	assert.Error(t, err, "expected validation to fail for invalid compose")
}
