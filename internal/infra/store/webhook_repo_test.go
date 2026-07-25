package store

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestWebhookRepo_Create_SystemStack_FKSatisfied is the regression test for
// the bug found live on the servarr composer instance: webhooks.stack_name
// REFERENCES stacks(name), but nothing seeded a "_system" row, so creating
// the self-upgrade webhook (POST /api/v1/webhooks {"stack_name":"_system",...})
// violated the FK and 500'd. 007_system_webhook_stack.sql fixes it by
// seeding the sentinel row.
func TestWebhookRepo_Create_SystemStack_FKSatisfied(t *testing.T) {
	db := newTestDB(t)
	mustCreateUser(t, db, "tester")
	repo := NewWebhookRepo(db)

	w := &Webhook{
		ID:           "wh_test_system",
		StackName:    "_system",
		Provider:     "generic",
		Secret:       "test-secret",
		AutoRedeploy: false,
		CreatedBy:    "tester",
	}

	err := repo.Create(context.Background(), w)
	require.NoError(t, err, "creating a _system webhook must not fail the stacks(name) FK")

	got, err := repo.GetByID(context.Background(), "wh_test_system")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "_system", got.StackName)
}
