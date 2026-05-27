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

func TestClient_ListImages(t *testing.T) {
	c, err := docker.NewClient("")
	require.NoError(t, err)
	defer c.Close()

	imgs, err := c.ListImages(context.Background())
	require.NoError(t, err)
	t.Logf("Found %d images", len(imgs))
}

func TestClient_PullAndRemoveImage(t *testing.T) {
	c, err := docker.NewClient("")
	require.NoError(t, err)
	defer c.Close()

	ctx := context.Background()
	const ref = "busybox:uclibc"

	// Pull a small image
	err = c.PullImage(ctx, ref)
	require.NoError(t, err)

	// Verify it appears in list
	imgs, err := c.ListImages(ctx)
	require.NoError(t, err)
	var imgID string
	for _, img := range imgs {
		for _, tag := range img.Tags {
			if tag == ref {
				imgID = img.ID
				break
			}
		}
	}
	require.NotEmpty(t, imgID, "expected to find pulled image %s", ref)

	// Remove it (Force=true should succeed even without stopped containers)
	err = c.RemoveImage(ctx, imgID)
	require.NoError(t, err)

	// Verify it's gone
	imgs, err = c.ListImages(ctx)
	require.NoError(t, err)
	for _, img := range imgs {
		for _, tag := range img.Tags {
			assert.NotEqual(t, ref, tag, "image should have been removed")
		}
	}
}

func TestClient_PruneImages(t *testing.T) {
	c, err := docker.NewClient("")
	require.NoError(t, err)
	defer c.Close()

	ctx := context.Background()

	// Prune dangling only (default) — should not error
	reclaimed, err := c.PruneImages(ctx, false)
	require.NoError(t, err)
	t.Logf("Dangling prune reclaimed: %d bytes", reclaimed)

	// Prune all unused — should not error
	reclaimed, err = c.PruneImages(ctx, true)
	require.NoError(t, err)
	t.Logf("All-unused prune reclaimed: %d bytes", reclaimed)
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

// TestCompose_Config_NoInterpolate is the regression test for the /diff
// secret-leak fix: Compose.Config must call `docker compose config` with
// --no-interpolate so that ${VAR} references stay verbatim in the output
// instead of being expanded against the on-disk .env file.
//
// The /diff endpoint is viewer-role and renders this stdout back to the
// client. If the flag regresses, any plaintext .env value (or transiently
// SOPS-decrypted secret) becomes readable by every viewer-token holder on
// every diff request.
func TestCompose_Config_NoInterpolate(t *testing.T) {
	dir := t.TempDir()
	compose := `services:
  app:
    image: alpine:3
    environment:
      DB_PASSWORD: ${DB_PASSWORD}
      API_TOKEN: ${API_TOKEN}
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "compose.yaml"), []byte(compose), 0644))

	const dbSecret = "super-secret-leaked-value-123"
	const apiSecret = "ghp_pretend_this_is_a_real_pat"
	env := "DB_PASSWORD=" + dbSecret + "\nAPI_TOKEN=" + apiSecret + "\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".env"), []byte(env), 0644))

	c, err := docker.NewClient("")
	require.NoError(t, err)
	defer c.Close()

	comp := docker.NewCompose(c.Host(), nil)

	result, err := comp.Config(context.Background(), dir)
	require.NoError(t, err, "config stderr: %s", result.Stderr)

	// Reference syntax must survive -- this is the actual normalized output
	// the /diff endpoint serializes back to clients.
	assert.Contains(t, result.Stdout, "${DB_PASSWORD}",
		"${DB_PASSWORD} must be preserved verbatim, not interpolated")
	assert.Contains(t, result.Stdout, "${API_TOKEN}",
		"${API_TOKEN} must be preserved verbatim, not interpolated")

	// And the secret VALUES must never appear in the output.
	assert.NotContains(t, result.Stdout, dbSecret,
		"DB_PASSWORD value leaked into /diff output -- --no-interpolate regressed")
	assert.NotContains(t, result.Stdout, apiSecret,
		"API_TOKEN value leaked into /diff output -- --no-interpolate regressed")
}
