package store

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/erfianugrah/composer/internal/domain/registry"
)

func TestRegistryRepo_UpsertAndList(t *testing.T) {
	db := newTestDB(t)
	repo := NewRegistryCredentialRepo(db)
	ctx := context.Background()

	global := &registry.Credential{Registry: "ghcr.io", Username: "alice", Secret: "ghp_global"}
	stackScoped := &registry.Credential{Registry: "ghcr.io", Username: "bob", Secret: "ghp_stack", StackName: "bonkled"}
	other := &registry.Credential{Registry: "docker.io", Username: "carol", Secret: "hunter2"}

	require.NoError(t, repo.Upsert(ctx, global))
	require.NoError(t, repo.Upsert(ctx, stackScoped))
	require.NoError(t, repo.Upsert(ctx, other))
	assert.NotZero(t, global.ID)

	all, err := repo.List(ctx)
	require.NoError(t, err)
	assert.Len(t, all, 3)

	g, err := repo.ListGlobal(ctx)
	require.NoError(t, err)
	assert.Len(t, g, 2) // global + docker.io (also global)

	s, err := repo.ListForStack(ctx, "bonkled")
	require.NoError(t, err)
	require.Len(t, s, 1)
	assert.Equal(t, "ghcr.io", s[0].Registry)
	assert.Equal(t, "bob", s[0].Username)
	assert.Equal(t, "ghp_stack", s[0].Secret, "secret round-trips through AES-GCM encrypt+decrypt")
}

func TestRegistryRepo_UpsertConflict(t *testing.T) {
	db := newTestDB(t)
	repo := NewRegistryCredentialRepo(db)
	ctx := context.Background()

	c := &registry.Credential{Registry: "ghcr.io", Username: "alice", Secret: "v1"}
	require.NoError(t, repo.Upsert(ctx, c))

	// Same (registry, stack_name) tuple — should update, not duplicate
	c2 := &registry.Credential{Registry: "ghcr.io", Username: "alice", Secret: "v2"}
	require.NoError(t, repo.Upsert(ctx, c2))

	all, _ := repo.List(ctx)
	require.Len(t, all, 1, "ON CONFLICT(registry, stack_name) should update in place")
	assert.Equal(t, "v2", all[0].Secret)
}

func TestRegistryRepo_GetByID_NotFound(t *testing.T) {
	db := newTestDB(t)
	repo := NewRegistryCredentialRepo(db)
	c, err := repo.GetByID(context.Background(), 9999)
	require.NoError(t, err)
	assert.Nil(t, c)
}

func TestRegistryRepo_Delete(t *testing.T) {
	db := newTestDB(t)
	repo := NewRegistryCredentialRepo(db)
	ctx := context.Background()

	c := &registry.Credential{Registry: "ghcr.io", Username: "alice", Secret: "v1"}
	require.NoError(t, repo.Upsert(ctx, c))
	require.NotZero(t, c.ID)

	require.NoError(t, repo.Delete(ctx, c.ID))
	got, _ := repo.GetByID(ctx, c.ID)
	assert.Nil(t, got)
}
