package store

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
