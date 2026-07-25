package store

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUpgradeRepo_Upsert_New(t *testing.T) {
	db := newTestDB(t)
	repo := NewUpgradeRepo(db)
	ctx := context.Background()

	row := &UpgradeRow{
		ID:             1,
		Status:         "pending",
		FromVersion:    "0.15.0",
		TargetImage:    "ghcr.io/erfianugrah/composer:v0.16.0",
		DeploymentType: "compose",
	}
	err := repo.Upsert(ctx, row)
	require.NoError(t, err)

	// Read it back.
	got, err := repo.Get(ctx)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "pending", got.Status)
	assert.Equal(t, "0.15.0", got.FromVersion)
	assert.Equal(t, "ghcr.io/erfianugrah/composer:v0.16.0", got.TargetImage)
	assert.Equal(t, "compose", got.DeploymentType)
}

func TestUpgradeRepo_Upsert_Conflict(t *testing.T) {
	db := newTestDB(t)
	repo := NewUpgradeRepo(db)
	ctx := context.Background()

	// First upsert.
	err := repo.Upsert(ctx, &UpgradeRow{
		ID: 1, Status: "pending", FromVersion: "0.15.0", TargetImage: "test:latest", DeploymentType: "compose",
	})
	require.NoError(t, err)

	// Second upsert should fail because status is still pending (in-flight).
	err = repo.Upsert(ctx, &UpgradeRow{
		ID: 1, Status: "pending", FromVersion: "0.15.0", TargetImage: "test:v2", DeploymentType: "compose",
	})
	assert.ErrorIs(t, err, ErrUpgradeInFlight)
}

func TestUpgradeRepo_Upsert_RetryAfterCompleted(t *testing.T) {
	db := newTestDB(t)
	repo := NewUpgradeRepo(db)
	ctx := context.Background()

	// First upsert.
	err := repo.Upsert(ctx, &UpgradeRow{
		ID: 1, Status: "pending", FromVersion: "0.15.0", TargetImage: "test:latest", DeploymentType: "compose",
	})
	require.NoError(t, err)

	// Mark as completed.
	err = repo.UpdateStatus(ctx, "pending", "completed", "helper123", "")
	require.NoError(t, err)

	// New upsert should succeed after completed.
	err = repo.Upsert(ctx, &UpgradeRow{
		ID: 1, Status: "pending", FromVersion: "0.15.0", TargetImage: "test:v2", DeploymentType: "compose",
	})
	assert.NoError(t, err)
}

func TestUpgradeRepo_Upsert_RetryAfterFailed(t *testing.T) {
	db := newTestDB(t)
	repo := NewUpgradeRepo(db)
	ctx := context.Background()

	err := repo.Upsert(ctx, &UpgradeRow{
		ID: 1, Status: "pending", FromVersion: "0.15.0", TargetImage: "test:latest", DeploymentType: "compose",
	})
	require.NoError(t, err)

	// Mark as failed.
	err = repo.UpdateStatus(ctx, "pending", "failed", "", "something broke")
	require.NoError(t, err)

	// New upsert should succeed after failed.
	err = repo.Upsert(ctx, &UpgradeRow{
		ID: 1, Status: "pending", FromVersion: "0.15.0", TargetImage: "test:v2", DeploymentType: "compose",
	})
	assert.NoError(t, err)
}

func TestUpgradeRepo_Get_Empty(t *testing.T) {
	db := newTestDB(t)
	repo := NewUpgradeRepo(db)
	ctx := context.Background()

	got, err := repo.Get(ctx)
	assert.NoError(t, err)
	assert.Nil(t, got)
}

func TestUpgradeRepo_UpdateStatus(t *testing.T) {
	db := newTestDB(t)
	repo := NewUpgradeRepo(db)
	ctx := context.Background()

	err := repo.Upsert(ctx, &UpgradeRow{
		ID: 1, Status: "pending", FromVersion: "0.15.0", TargetImage: "test:latest", DeploymentType: "compose",
	})
	require.NoError(t, err)

	// Transition: pending -> helper_running.
	err = repo.UpdateStatus(ctx, "pending", "helper_running", "helper123", "")
	require.NoError(t, err)

	got, err := repo.Get(ctx)
	require.NoError(t, err)
	assert.Equal(t, "helper_running", got.Status)
	assert.Equal(t, "helper123", got.HelperID)

	// Transition: helper_running -> completed.
	err = repo.UpdateStatus(ctx, "helper_running", "completed", "helper123", "")
	require.NoError(t, err)

	got, err = repo.Get(ctx)
	require.NoError(t, err)
	assert.Equal(t, "completed", got.Status)
}

func TestUpgradeRepo_UpdateStatus_StaleCurrentStatus(t *testing.T) {
	db := newTestDB(t)
	repo := NewUpgradeRepo(db)
	ctx := context.Background()

	err := repo.Upsert(ctx, &UpgradeRow{
		ID: 1, Status: "pending", FromVersion: "0.15.0", TargetImage: "test:latest", DeploymentType: "compose",
	})
	require.NoError(t, err)

	// Try to transition from "helper_running" but actual status is "pending".
	err = repo.UpdateStatus(ctx, "helper_running", "completed", "helper123", "")
	assert.ErrorIs(t, err, ErrNotUpdated)
}

func TestReconcileStaleRuns(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()

	// Create a user first (required by pipelines.created_by FK).
	_, err := db.ExecContext(ctx,
		`INSERT INTO users (id, email, password_hash, role, created_at, updated_at)
		 VALUES ('admin', 'admin@test.local', '$2a$ignored', 'admin', datetime('now'), datetime('now'))`)
	require.NoError(t, err)

	// Insert a run that's still "running" (simulating unclean shutdown).
	_, err = db.ExecContext(ctx,
		`INSERT INTO pipelines (id, name, description, config, created_by, created_at, updated_at)
		 VALUES ('test-pl', 'Test', '', '{}', 'admin', datetime('now'), datetime('now'))`)
	require.NoError(t, err)

	_, err = db.ExecContext(ctx,
		`INSERT INTO pipeline_runs (id, pipeline_id, status, triggered_by, created_at)
		 VALUES ('run-1', 'test-pl', 'running', 'admin', datetime('now'))`)
	require.NoError(t, err)

	// Also insert a completed run that should be left alone.
	_, err = db.ExecContext(ctx,
		`INSERT INTO pipeline_runs (id, pipeline_id, status, triggered_by, created_at, finished_at)
		 VALUES ('run-2', 'test-pl', 'success', 'admin', datetime('now'), datetime('now'))`)
	require.NoError(t, err)

	err = ReconcileStaleRuns(ctx, db)
	require.NoError(t, err)

	// Run-1 should be cancelled.
	var status string
	err = db.QueryRowContext(ctx, `SELECT status FROM pipeline_runs WHERE id='run-1'`).Scan(&status)
	require.NoError(t, err)
	assert.Equal(t, "cancelled", status)

	// Run-2 should still be success.
	err = db.QueryRowContext(ctx, `SELECT status FROM pipeline_runs WHERE id='run-2'`).Scan(&status)
	require.NoError(t, err)
	assert.Equal(t, "success", status)
}
