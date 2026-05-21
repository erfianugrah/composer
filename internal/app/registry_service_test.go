package app_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/erfianugrah/composer/internal/app"
	"github.com/erfianugrah/composer/internal/domain/registry"
)

// memRegistryRepo is an in-memory registry.Repository for unit tests.
type memRegistryRepo struct {
	mu   sync.Mutex
	rows []*registry.Credential
	next int64
}

func (r *memRegistryRepo) Upsert(_ context.Context, c *registry.Credential) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := c.Validate(); err != nil {
		return err
	}
	for i, ex := range r.rows {
		if ex.Registry == c.Registry && ex.StackName == c.StackName {
			c.ID = ex.ID
			r.rows[i] = c
			return nil
		}
	}
	r.next++
	c.ID = r.next
	r.rows = append(r.rows, c)
	return nil
}
func (r *memRegistryRepo) Delete(_ context.Context, id int64) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i, c := range r.rows {
		if c.ID == id {
			r.rows = append(r.rows[:i], r.rows[i+1:]...)
			return nil
		}
	}
	return nil
}
func (r *memRegistryRepo) GetByID(_ context.Context, id int64) (*registry.Credential, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, c := range r.rows {
		if c.ID == id {
			return c, nil
		}
	}
	return nil, nil
}
func (r *memRegistryRepo) List(_ context.Context) ([]*registry.Credential, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]*registry.Credential, len(r.rows))
	copy(out, r.rows)
	return out, nil
}
func (r *memRegistryRepo) ListGlobal(ctx context.Context) ([]*registry.Credential, error) {
	all, _ := r.List(ctx)
	var out []*registry.Credential
	for _, c := range all {
		if c.IsGlobal() {
			out = append(out, c)
		}
	}
	return out, nil
}
func (r *memRegistryRepo) ListForStack(ctx context.Context, stack string) ([]*registry.Credential, error) {
	all, _ := r.List(ctx)
	var out []*registry.Credential
	for _, c := range all {
		if c.StackName == stack {
			out = append(out, c)
		}
	}
	return out, nil
}

func TestBootstrapFromEnv_InlineJSON(t *testing.T) {
	repo := &memRegistryRepo{}
	svc := app.NewRegistryService(repo, nil)

	t.Setenv("COMPOSER_REGISTRY_AUTHS", `[
		{"registry":"ghcr.io","username":"alice","secret":"ghp_pat"},
		{"registry":"docker.io","username":"bob","secret":"hunter2","email":"bob@x"}
	]`)
	t.Setenv("COMPOSER_REGISTRY_AUTHS_FILE", "")

	require.NoError(t, svc.BootstrapFromEnv(context.Background()))
	all, _ := repo.List(context.Background())
	assert.Len(t, all, 2)
}

func TestBootstrapFromEnv_FilePath(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "auths.json")
	data, _ := json.Marshal([]map[string]string{
		{"registry": "ghcr.io", "username": "alice", "secret": "ghp_pat"},
	})
	require.NoError(t, os.WriteFile(path, data, 0600))

	t.Setenv("COMPOSER_REGISTRY_AUTHS", "")
	t.Setenv("COMPOSER_REGISTRY_AUTHS_FILE", path)

	repo := &memRegistryRepo{}
	svc := app.NewRegistryService(repo, nil)
	require.NoError(t, svc.BootstrapFromEnv(context.Background()))
	all, _ := repo.List(context.Background())
	require.Len(t, all, 1)
	assert.Equal(t, "ghcr.io", all[0].Registry)
}

func TestBootstrapFromEnv_IdempotentByDefault(t *testing.T) {
	repo := &memRegistryRepo{}
	// Pre-existing row with different secret — bootstrap must NOT clobber it
	// unless OVERWRITE=true.
	require.NoError(t, repo.Upsert(context.Background(), &registry.Credential{
		Registry: "ghcr.io", Username: "old-user", Secret: "old-secret",
	}))

	t.Setenv("COMPOSER_REGISTRY_AUTHS", `[{"registry":"ghcr.io","username":"new-user","secret":"new-secret"}]`)
	t.Setenv("COMPOSER_REGISTRY_AUTHS_OVERWRITE", "")

	svc := app.NewRegistryService(repo, nil)
	require.NoError(t, svc.BootstrapFromEnv(context.Background()))

	all, _ := repo.List(context.Background())
	require.Len(t, all, 1)
	assert.Equal(t, "old-user", all[0].Username, "existing row preserved without OVERWRITE")
}

func TestBootstrapFromEnv_OverwriteTrue(t *testing.T) {
	repo := &memRegistryRepo{}
	require.NoError(t, repo.Upsert(context.Background(), &registry.Credential{
		Registry: "ghcr.io", Username: "old", Secret: "old",
	}))

	t.Setenv("COMPOSER_REGISTRY_AUTHS", `[{"registry":"ghcr.io","username":"new","secret":"new"}]`)
	t.Setenv("COMPOSER_REGISTRY_AUTHS_OVERWRITE", "true")

	svc := app.NewRegistryService(repo, nil)
	require.NoError(t, svc.BootstrapFromEnv(context.Background()))

	all, _ := repo.List(context.Background())
	require.Len(t, all, 1)
	assert.Equal(t, "new", all[0].Username, "OVERWRITE=true replaces existing row")
	assert.Equal(t, "new", all[0].Secret)
}

func TestBootstrapFromEnv_EmptyNoOp(t *testing.T) {
	t.Setenv("COMPOSER_REGISTRY_AUTHS", "")
	t.Setenv("COMPOSER_REGISTRY_AUTHS_FILE", "")

	repo := &memRegistryRepo{}
	svc := app.NewRegistryService(repo, nil)
	require.NoError(t, svc.BootstrapFromEnv(context.Background()))
	all, _ := repo.List(context.Background())
	assert.Empty(t, all)
}

func TestBootstrapFromEnv_BadJSON(t *testing.T) {
	t.Setenv("COMPOSER_REGISTRY_AUTHS", "not-json{")
	svc := app.NewRegistryService(&memRegistryRepo{}, nil)
	err := svc.BootstrapFromEnv(context.Background())
	require.Error(t, err, "malformed JSON should surface as error so operator sees the typo")
}

func TestBootstrapFromEnv_SkipsInvalidSeed(t *testing.T) {
	// One invalid (no secret), one valid — invalid is logged + skipped, valid still applied.
	t.Setenv("COMPOSER_REGISTRY_AUTHS", `[
		{"registry":"ghcr.io","username":"alice"},
		{"registry":"docker.io","username":"bob","secret":"ok"}
	]`)
	repo := &memRegistryRepo{}
	svc := app.NewRegistryService(repo, nil)
	require.NoError(t, svc.BootstrapFromEnv(context.Background()))
	all, _ := repo.List(context.Background())
	require.Len(t, all, 1)
	assert.Equal(t, "docker.io", all[0].Registry)
}
