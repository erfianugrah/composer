package store

import (
	"context"
	"testing"
	"time"

	"github.com/erfianugrah/composer/internal/domain/host"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHostRepoCRUD(t *testing.T) {
	db := newTestDB(t)
	repo := NewHostRepo(db)
	ctx := context.Background()
	now := time.Now().UTC()

	h := &host.Host{Name: "remote1", Endpoint: "tcp://docker-remote.example:2376", CertDir: "/certs", CreatedAt: now, UpdatedAt: now}
	require.NoError(t, repo.Create(ctx, h))
	assert.NotZero(t, h.ID)

	// GetByID round-trip
	got, err := repo.GetByID(ctx, h.ID)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "remote1", got.Name)
	assert.Equal(t, "tcp://docker-remote.example:2376", got.Endpoint)
	assert.Equal(t, "/certs", got.CertDir)
	assert.False(t, got.CreatedAt.IsZero())
	assert.False(t, got.UpdatedAt.IsZero())

	// GetByName round-trip
	gotByName, err := repo.GetByName(ctx, "remote1")
	require.NoError(t, err)
	require.NotNil(t, gotByName)
	assert.Equal(t, h.ID, gotByName.ID)

	// GetByName missing -> nil, nil
	missing, err := repo.GetByName(ctx, "nope")
	require.NoError(t, err)
	assert.Nil(t, missing)

	// List returns both
	h2 := &host.Host{Name: "edge", Endpoint: "tcp://10.0.0.2:2375", CreatedAt: now, UpdatedAt: now}
	require.NoError(t, repo.Create(ctx, h2))
	list, err := repo.List(ctx)
	require.NoError(t, err)
	assert.Len(t, list, 2)

	// Update changes endpoint
	h.Endpoint = "tcp://docker-remote2.example:2376"
	require.NoError(t, repo.Update(ctx, h))
	updated, err := repo.GetByID(ctx, h.ID)
	require.NoError(t, err)
	assert.Equal(t, "tcp://docker-remote2.example:2376", updated.Endpoint)

	// Delete removes
	require.NoError(t, repo.Delete(ctx, h.ID))
	afterDel, err := repo.GetByID(ctx, h.ID)
	require.NoError(t, err)
	assert.Nil(t, afterDel)
}

func TestHostRepoDuplicateName(t *testing.T) {
	db := newTestDB(t)
	repo := NewHostRepo(db)
	ctx := context.Background()
	now := time.Now().UTC()

	h1 := &host.Host{Name: "dup", Endpoint: "tcp://x:2375", CreatedAt: now, UpdatedAt: now}
	require.NoError(t, repo.Create(ctx, h1))

	h2 := &host.Host{Name: "dup", Endpoint: "tcp://y:2375", CreatedAt: now, UpdatedAt: now}
	err := repo.Create(ctx, h2)
	assert.Error(t, err, "duplicate host name should fail")
}

func TestHostRepoCountStacks(t *testing.T) {
	db := newTestDB(t)
	repo := NewHostRepo(db)
	ctx := context.Background()
	now := time.Now().UTC()

	h := &host.Host{Name: "countme", Endpoint: "tcp://x:2375", CreatedAt: now, UpdatedAt: now}
	require.NoError(t, repo.Create(ctx, h))

	// No stacks referencing this host yet.
	n, err := repo.CountStacks(ctx, h.ID)
	require.NoError(t, err)
	assert.Equal(t, 0, n)

	// Insert a stack referencing the host.
	_, err = db.ExecContext(ctx,
		`INSERT INTO stacks (name, path, source, host_id, created_at, updated_at)
		 VALUES ('teststack', '/tmp/test', 'local', $1, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`,
		h.ID)
	require.NoError(t, err)

	n, err = repo.CountStacks(ctx, h.ID)
	require.NoError(t, err)
	assert.Equal(t, 1, n)

	// Delete the stack; count should drop back.
	_, err = db.ExecContext(ctx, `DELETE FROM stacks WHERE name = 'teststack'`)
	require.NoError(t, err)

	n, err = repo.CountStacks(ctx, h.ID)
	require.NoError(t, err)
	assert.Equal(t, 0, n)
}
