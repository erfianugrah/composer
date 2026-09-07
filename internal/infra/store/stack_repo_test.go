package store

import (
	"context"
	"testing"
	"time"

	"github.com/erfianugrah/composer/internal/domain/host"
	"github.com/erfianugrah/composer/internal/domain/stack"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func now() time.Time { return time.Now().UTC() }

// The "_system" row is a sentinel seeded by 007_system_webhook_stack.sql
// solely to satisfy the webhooks.stack_name FK for the self-upgrade webhook
// (see webhook.go: StackName == "_system"). It must never surface through
// the generic stack API/UI as a phantom stack.

func TestStackRepo_GetByName_HidesSystemSentinel(t *testing.T) {
	db := newTestDB(t)
	repo := NewStackRepo(db)

	got, err := repo.GetByName(context.Background(), "_system")
	require.NoError(t, err)
	assert.Nil(t, got, "the _system sentinel must not be reachable via GetByName")
}

func TestStackRepo_List_ExcludesSystemSentinel(t *testing.T) {
	db := newTestDB(t)
	repo := NewStackRepo(db)

	// Confirm the sentinel row really was seeded by the migration (otherwise
	// this test would pass vacuously).
	var count int
	require.NoError(t, db.QueryRowContext(context.Background(),
		`SELECT COUNT(*) FROM stacks WHERE name = '_system'`).Scan(&count))
	require.Equal(t, 1, count, "007_system_webhook_stack.sql should have seeded the sentinel row")

	stacks, err := repo.List(context.Background())
	require.NoError(t, err)
	for _, s := range stacks {
		assert.NotEqual(t, "_system", s.Name, "the _system sentinel must not appear in stack listings")
	}
}

func TestStackRepoHostIDRoundTrip(t *testing.T) {
	db := newTestDB(t)
	hostRepo := NewHostRepo(db)
	repo := NewStackRepo(db)
	ctx := context.Background()

	// Create a docker host
	h := &host.Host{Name: "remote1", Endpoint: "tcp://docker-remote.example:2376", CreatedAt: now(), UpdatedAt: now()}
	require.NoError(t, hostRepo.Create(ctx, h))

	// Create a stack with HostID set
	st, err := stack.NewStack("s1", "/tmp/s1", stack.SourceLocal)
	require.NoError(t, err)
	st.HostID = &h.ID
	require.NoError(t, repo.Create(ctx, st))

	// GetByName returns HostID
	got, err := repo.GetByName(ctx, "s1")
	require.NoError(t, err)
	require.NotNil(t, got)
	require.NotNil(t, got.HostID)
	assert.Equal(t, h.ID, *got.HostID)

	// List returns HostID
	list, err := repo.List(ctx)
	require.NoError(t, err)
	var found *stack.Stack
	for _, s := range list {
		if s.Name == "s1" {
			found = s
			break
		}
	}
	require.NotNil(t, found)
	require.NotNil(t, found.HostID)
	assert.Equal(t, h.ID, *found.HostID)

	// A stack created without HostID comes back with HostID == nil
	st2, err := stack.NewStack("s2", "/tmp/s2", stack.SourceLocal)
	require.NoError(t, err)
	require.NoError(t, repo.Create(ctx, st2))
	got2, err := repo.GetByName(ctx, "s2")
	require.NoError(t, err)
	require.NotNil(t, got2)
	assert.Nil(t, got2.HostID)
}

func TestStackRepoDependsOnRoundTrip(t *testing.T) {
	db := newTestDB(t)
	repo := NewStackRepo(db)
	ctx := context.Background()

	// A stack created without dependencies reads back as an empty (non-nil)
	// list: the column default is '[]' and the API always emits an array.
	base, err := stack.NewStack("servarr", "/tmp/servarr", stack.SourceLocal)
	require.NoError(t, err)
	require.NoError(t, repo.Create(ctx, base))
	got, err := repo.GetByName(ctx, "servarr")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.NotNil(t, got.DependsOn)
	assert.Empty(t, got.DependsOn)

	// Dependencies survive Create...
	dep, err := stack.NewStack("sonarr", "/tmp/sonarr", stack.SourceLocal)
	require.NoError(t, err)
	dep.DependsOn = []string{"servarr"}
	require.NoError(t, repo.Create(ctx, dep))
	got, err = repo.GetByName(ctx, "sonarr")
	require.NoError(t, err)
	assert.Equal(t, []string{"servarr"}, got.DependsOn)

	// ...and Update, including order.
	got.DependsOn = []string{"servarr", "gluetun"}
	got.UpdatedAt = now()
	require.NoError(t, repo.Update(ctx, got))
	got, err = repo.GetByName(ctx, "sonarr")
	require.NoError(t, err)
	assert.Equal(t, []string{"servarr", "gluetun"}, got.DependsOn)

	// List carries the column too.
	list, err := repo.List(ctx)
	require.NoError(t, err)
	byName := map[string][]string{}
	for _, s := range list {
		byName[s.Name] = s.DependsOn
	}
	assert.Equal(t, []string{"servarr", "gluetun"}, byName["sonarr"])
	assert.Empty(t, byName["servarr"])

	// Clearing writes '[]', not NULL (the column is NOT NULL).
	got.DependsOn = nil
	require.NoError(t, repo.Update(ctx, got))
	var raw string
	require.NoError(t, db.QueryRowContext(ctx, `SELECT depends_on FROM stacks WHERE name = 'sonarr'`).Scan(&raw))
	assert.Equal(t, "[]", raw)
}
