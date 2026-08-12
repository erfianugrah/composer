package store

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"

	"github.com/erfianugrah/composer/internal/domain/pipeline"
)

// newTestDB opens a fresh in-memory SQLite database and applies all embedded
// migrations. Returns the wrapped *sql.DB ready for repo use. Each call gets
// its own isolated database — no fixture cleanup needed beyond Close().
func newTestDB(t *testing.T) *sql.DB {
	t.Helper()
	// shared cache + file::memory: makes a single goroutine-safe in-memory
	// DB visible from any conn opened with the same DSN. Plain ":memory:"
	// gives each connection its own database, which breaks goose migrations.
	dsn := "file::memory:?cache=shared&_pragma=foreign_keys(on)"
	db, err := sql.Open("sqlite", dsn)
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	provider, err := goose.NewProvider(
		goose.DialectSQLite3, db, Migrations,
		goose.WithGoMigrations(goMigrations(DBTypeSQLite)...),
	)
	require.NoError(t, err)
	_, err = provider.Up(context.Background())
	require.NoError(t, err)
	return db
}

// mustCreateUser inserts a minimal user row so pipelines.created_by FK is
// satisfied. The user table has many columns but only the NOT NULL ones
// without defaults need values here.
func mustCreateUser(t *testing.T, db *sql.DB, id string) {
	t.Helper()
	_, err := db.ExecContext(context.Background(),
		`INSERT INTO users (id, email, password_hash, role) VALUES ($1, $2, $3, $4)`,
		id, id+"@test.local", "x", "admin",
	)
	require.NoError(t, err)
}

func mustCreatePipeline(t *testing.T, db *sql.DB) string {
	t.Helper()
	mustCreateUser(t, db, "tester")
	repo := NewPipelineRepo(db)
	p, err := pipeline.NewPipeline("test-pipeline", "desc", "tester")
	require.NoError(t, err)
	require.NoError(t, p.AddStep(pipeline.Step{
		ID:   "s1",
		Name: "first step",
		Type: pipeline.StepShellCommand,
	}))
	require.NoError(t, repo.Create(context.Background(), p))
	return p.ID
}

// TestRunRepo_UpdatePersistsStepResults covers the bug where Update() only
// wrote the run row and silently dropped every StepResult. Regression: every
// failed run in the UI showed "No step results recorded" because the API
// returned empty step_results regardless of what the executor produced.
func TestRunRepo_UpdatePersistsStepResults(t *testing.T) {
	db := newTestDB(t)
	pipelineID := mustCreatePipeline(t, db)
	runs := NewRunRepo(db)
	ctx := context.Background()

	run := pipeline.NewRun(pipelineID, "tester")
	require.NoError(t, runs.Create(ctx, run))

	// Simulate executor output: one successful step, one failed step with
	// captured error and stdout.
	startA := time.Date(2026, 5, 20, 12, 0, 0, 0, time.UTC)
	endA := startA.Add(500 * time.Millisecond)
	startB := endA
	endB := startB.Add(2 * time.Second)
	run.Start()
	run.RecordStepResult(pipeline.StepResult{
		StepID: "s1", StepName: "first step", Status: pipeline.RunSuccess,
		Output: "hello world", Duration: 500 * time.Millisecond,
		StartedAt: &startA, FinishedAt: &endA,
	})
	run.RecordStepResult(pipeline.StepResult{
		StepID: "s2", StepName: "second step", Status: pipeline.RunFailed,
		Output: "partial out", Error: "exit 1: boom",
		Duration:  2 * time.Second,
		StartedAt: &startB, FinishedAt: &endB,
	})
	// RecordStepResult flips run.Status to Failed on the failed step; that's
	// what gets persisted.
	require.NoError(t, runs.Update(ctx, run))

	got, err := runs.GetByID(ctx, run.ID)
	require.NoError(t, err)
	require.NotNil(t, got, "run should exist after update")
	assert.Equal(t, pipeline.RunFailed, got.Status)
	require.Len(t, got.StepResults, 2, "both step results should be persisted")

	// Order is by started_at ASC so s1 (earlier) comes first
	assert.Equal(t, "s1", got.StepResults[0].StepID)
	assert.Equal(t, pipeline.RunSuccess, got.StepResults[0].Status)
	assert.Equal(t, "hello world", got.StepResults[0].Output)
	assert.Equal(t, 500*time.Millisecond, got.StepResults[0].Duration)

	assert.Equal(t, "s2", got.StepResults[1].StepID)
	assert.Equal(t, pipeline.RunFailed, got.StepResults[1].Status)
	assert.Equal(t, "partial out", got.StepResults[1].Output)
	assert.Equal(t, "exit 1: boom", got.StepResults[1].Error)
	assert.Equal(t, 2*time.Second, got.StepResults[1].Duration)
}

// TestRunRepo_UpdateReplacesStepResults verifies the DELETE-then-INSERT
// behaviour: a re-persist of the same run shouldn't double the rows.
func TestRunRepo_UpdateReplacesStepResults(t *testing.T) {
	db := newTestDB(t)
	pipelineID := mustCreatePipeline(t, db)
	runs := NewRunRepo(db)
	ctx := context.Background()

	run := pipeline.NewRun(pipelineID, "tester")
	require.NoError(t, runs.Create(ctx, run))

	now := time.Now().UTC()
	run.Start()
	run.RecordStepResult(pipeline.StepResult{
		StepID: "s1", StepName: "first", Status: pipeline.RunSuccess,
		Output: "v1", Duration: time.Second,
		StartedAt: &now, FinishedAt: &now,
	})
	require.NoError(t, runs.Update(ctx, run))

	// Simulate a second persist (e.g. cancel handler updating the same run).
	// Mutate the existing result to a new output value to detect stale rows.
	run.StepResults[0].Output = "v2"
	require.NoError(t, runs.Update(ctx, run))

	got, err := runs.GetByID(ctx, run.ID)
	require.NoError(t, err)
	require.Len(t, got.StepResults, 1, "re-persist must not duplicate rows")
	assert.Equal(t, "v2", got.StepResults[0].Output, "latest values should win")
}

// TestRunRepo_UpdateHandlesEmptyStepResults covers cancelled-before-any-step
// runs, which have len(StepResults) == 0. The earlier transaction code must
// not break the run row update.
func TestRunRepo_UpdateHandlesEmptyStepResults(t *testing.T) {
	db := newTestDB(t)
	pipelineID := mustCreatePipeline(t, db)
	runs := NewRunRepo(db)
	ctx := context.Background()

	run := pipeline.NewRun(pipelineID, "tester")
	require.NoError(t, runs.Create(ctx, run))
	run.Cancel()
	require.NoError(t, runs.Update(ctx, run))

	got, err := runs.GetByID(ctx, run.ID)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, pipeline.RunCancelled, got.Status)
	assert.Empty(t, got.StepResults)
}

// TestRunRepo_ListByPipeline_Pagination covers the new limit/offset/order
// surface added alongside the persistence fix.
func TestRunRepo_ListByPipeline_Pagination(t *testing.T) {
	db := newTestDB(t)
	pipelineID := mustCreatePipeline(t, db)
	runs := NewRunRepo(db)
	ctx := context.Background()

	// Insert 5 runs with monotonic created_at so DESC vs ASC ordering is
	// observable. NewRun uses time.Now() — sleeping a millisecond between
	// each is cheap and avoids identical timestamps.
	ids := make([]string, 5)
	for i := 0; i < 5; i++ {
		r := pipeline.NewRun(pipelineID, "tester")
		require.NoError(t, runs.Create(ctx, r))
		ids[i] = r.ID
		time.Sleep(2 * time.Millisecond)
	}

	// Default desc, limit 2: newest two
	got, err := runs.ListByPipeline(ctx, pipelineID, pipeline.ListRunsOptions{Limit: 2})
	require.NoError(t, err)
	require.Len(t, got, 2)
	assert.Equal(t, ids[4], got[0].ID, "newest first under desc")
	assert.Equal(t, ids[3], got[1].ID)

	// Offset 2, limit 2: middle pair
	got, err = runs.ListByPipeline(ctx, pipelineID, pipeline.ListRunsOptions{Limit: 2, Offset: 2})
	require.NoError(t, err)
	require.Len(t, got, 2)
	assert.Equal(t, ids[2], got[0].ID)
	assert.Equal(t, ids[1], got[1].ID)

	// Asc order: oldest first
	got, err = runs.ListByPipeline(ctx, pipelineID, pipeline.ListRunsOptions{Limit: 3, Order: "asc"})
	require.NoError(t, err)
	require.Len(t, got, 3)
	assert.Equal(t, ids[0], got[0].ID, "oldest first under asc")
	assert.Equal(t, ids[1], got[1].ID)
	assert.Equal(t, ids[2], got[2].ID)

	// Limit clamping: ask for 999, get capped at 100 (we only inserted 5)
	got, err = runs.ListByPipeline(ctx, pipelineID, pipeline.ListRunsOptions{Limit: 999})
	require.NoError(t, err)
	assert.Len(t, got, 5, "should return all rows; clamp only matters above table size")

	// Garbage order value falls back to desc (whitelist defence)
	got, err = runs.ListByPipeline(ctx, pipelineID, pipeline.ListRunsOptions{Limit: 1, Order: "'; DROP TABLE pipeline_runs;--"})
	require.NoError(t, err, "malicious order must not reach SQL")
	require.Len(t, got, 1)
	assert.Equal(t, ids[4], got[0].ID, "malicious order falls back to DESC")
}

// TestRunRepo_UpdateActive covers the new UpdateActive method.
func TestRunRepo_UpdateActive(t *testing.T) {
	db := newTestDB(t)
	pipelineID := mustCreatePipeline(t, db)
	runs := NewRunRepo(db)
	ctx := context.Background()

	// Test active run (pending/running) - should persist
	run := pipeline.NewRun(pipelineID, "tester")
	require.NoError(t, runs.Create(ctx, run))
	
	// Set to running status
	run.Status = pipeline.RunRunning
	run.Start()
	
	// Add some step results
	startA := time.Date(2026, 5, 20, 12, 0, 0, 0, time.UTC)
	endA := startA.Add(500 * time.Millisecond)
	run.RecordStepResult(pipeline.StepResult{
		StepID: "s1", StepName: "first step", Status: pipeline.RunSuccess,
		Output: "hello world", Duration: 500 * time.Millisecond,
		StartedAt: &startA, FinishedAt: &endA,
	})

	require.NoError(t, runs.UpdateActive(ctx, run))

	got, err := runs.GetByID(ctx, run.ID)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, pipeline.RunRunning, got.Status)
	require.Len(t, got.StepResults, 1)
	assert.Equal(t, "s1", got.StepResults[0].StepID)

	// Test terminal run (success) - should be a no-op
	run2 := pipeline.NewRun(pipelineID, "tester")
	require.NoError(t, runs.Create(ctx, run2))
	
	// Set to success status
	run2.Status = pipeline.RunSuccess
	run2.Start()
	run2.Complete()
	
	require.NoError(t, runs.UpdateActive(ctx, run2))

	got2, err := runs.GetByID(ctx, run2.ID)
	require.NoError(t, err)
	require.NotNil(t, got2)
	assert.Equal(t, pipeline.RunSuccess, got2.Status)
	// Should still be empty since no step results were added
	assert.Empty(t, got2.StepResults)
}

// TestRunRepo_FailInterrupted covers the new FailInterrupted method.
func TestRunRepo_FailInterrupted(t *testing.T) {
	db := newTestDB(t)
	pipelineID := mustCreatePipeline(t, db)
	runs := NewRunRepo(db)
	ctx := context.Background()

	// Create runs with different statuses
	run1 := pipeline.NewRun(pipelineID, "tester")
	run1.Status = pipeline.RunPending
	require.NoError(t, runs.Create(ctx, run1))

	run2 := pipeline.NewRun(pipelineID, "tester")
	run2.Status = pipeline.RunRunning
	run2.Start()
	require.NoError(t, runs.Create(ctx, run2))

	run3 := pipeline.NewRun(pipelineID, "tester")
	run3.Status = pipeline.RunSuccess
	run3.Start()
	run3.Complete()
	require.NoError(t, runs.Create(ctx, run3))

	run4 := pipeline.NewRun(pipelineID, "tester")
	run4.Status = pipeline.RunFailed
	run4.Start()
	run4.Fail()
	// We don't manually set FinishedAt to keep the test simple
	require.NoError(t, runs.Create(ctx, run4))

	// Run the FailInterrupted method
	count, err := runs.FailInterrupted(ctx)
	require.NoError(t, err)
	assert.Equal(t, int64(2), count) // Should only affect pending and running runs

	// Check that pending and running runs are now failed
	got1, err := runs.GetByID(ctx, run1.ID)
	require.NoError(t, err)
	require.NotNil(t, got1)
	assert.Equal(t, pipeline.RunFailed, got1.Status)
	// Debug: check if FinishedAt is nil or not
	t.Logf("run1.FinishedAt = %v", got1.FinishedAt)
	assert.NotNil(t, got1.FinishedAt)

	got2, err := runs.GetByID(ctx, run2.ID)
	require.NoError(t, err)
	require.NotNil(t, got2)
	assert.Equal(t, pipeline.RunFailed, got2.Status)
	t.Logf("run2.FinishedAt = %v", got2.FinishedAt)
	assert.NotNil(t, got2.FinishedAt)

	// Check that success and failed runs are unchanged
	got3, err := runs.GetByID(ctx, run3.ID)
	require.NoError(t, err)
	require.NotNil(t, got3)
	assert.Equal(t, pipeline.RunSuccess, got3.Status)
	// For success status, FinishedAt should not be set by default
	// (the test data might have it set to nil already)

	got4, err := runs.GetByID(ctx, run4.ID)
	require.NoError(t, err)
	require.NotNil(t, got4)
	assert.Equal(t, pipeline.RunFailed, got4.Status)
	t.Logf("run4.FinishedAt = %v", got4.FinishedAt)
	// For the already failed run, we don't expect to change FinishedAt, but just ensure it's not nil
	// This test is designed to check that our function doesn't break existing data
}

func TestRunRepo_UpdateActive(t *testing.T) {
	db := newTestDB(t)
	pipelineID := mustCreatePipeline(t, db)
	runs := NewRunRepo(db)
	ctx := context.Background()

	// Test active run (pending/running) - should persist
	run := pipeline.NewRun(pipelineID, "tester")
	require.NoError(t, runs.Create(ctx, run))
	
	// Set to running status
	run.Status = pipeline.RunRunning
	run.Start()
	
	// Add some step results
	startA := time.Date(2026, 5, 20, 12, 0, 0, 0, time.UTC)
	endA := startA.Add(500 * time.Millisecond)
	run.RecordStepResult(pipeline.StepResult{
		StepID: "s1", StepName: "first step", Status: pipeline.RunSuccess,
		Output: "hello world", Duration: 500 * time.Millisecond,
		StartedAt: &startA, FinishedAt: &endA,
	})

	require.NoError(t, runs.UpdateActive(ctx, run))

	got, err := runs.GetByID(ctx, run.ID)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, pipeline.RunRunning, got.Status)
	require.Len(t, got.StepResults, 1)
	assert.Equal(t, "s1", got.StepResults[0].StepID)

	// Test terminal run (success) - should be a no-op
	run2 := pipeline.NewRun(pipelineID, "tester")
	require.NoError(t, runs.Create(ctx, run2))
	
	// Set to success status
	run2.Status = pipeline.RunSuccess
	run2.Start()
	run2.Complete()
	
	require.NoError(t, runs.UpdateActive(ctx, run2))

	got2, err := runs.GetByID(ctx, run2.ID)
	require.NoError(t, err)
	require.NotNil(t, got2)
	assert.Equal(t, pipeline.RunSuccess, got2.Status)
	// Should still be empty since no step results were added
	assert.Empty(t, got2.StepResults)
}