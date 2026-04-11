package app

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestJobManager_CreateAndGet(t *testing.T) {
	m := NewJobManager()

	job := m.Create("deploy", "my-stack")
	require.NotEmpty(t, job.ID)
	assert.True(t, strings.HasPrefix(job.ID, "job_"))
	assert.Equal(t, "deploy", job.Type)
	assert.Equal(t, "my-stack", job.Target)
	assert.Equal(t, JobPending, job.Status)
	assert.Nil(t, job.StartedAt)
	assert.Nil(t, job.FinishedAt)

	got := m.Get(job.ID)
	require.NotNil(t, got)
	assert.Equal(t, job.ID, got.ID)
}

func TestJobManager_GetMissing(t *testing.T) {
	m := NewJobManager()
	assert.Nil(t, m.Get("nonexistent"))
}

func TestJobManager_Lifecycle_Complete(t *testing.T) {
	m := NewJobManager()
	job := m.Create("build_deploy", "web-app")

	m.Start(job.ID)
	got := m.Get(job.ID)
	assert.Equal(t, JobRunning, got.Status)
	assert.NotNil(t, got.StartedAt)
	assert.Nil(t, got.FinishedAt)

	m.Complete(job.ID, "containers started", "some warnings")
	got = m.Get(job.ID)
	assert.Equal(t, JobCompleted, got.Status)
	assert.Equal(t, "containers started", got.Output)
	assert.Equal(t, "some warnings", got.Error)
	assert.NotNil(t, got.FinishedAt)
}

func TestJobManager_Lifecycle_Fail(t *testing.T) {
	m := NewJobManager()
	job := m.Create("deploy", "broken-stack")

	m.Start(job.ID)
	m.Fail(job.ID, "compose validation failed")

	got := m.Get(job.ID)
	assert.Equal(t, JobFailed, got.Status)
	assert.Equal(t, "compose validation failed", got.Error)
	assert.NotNil(t, got.FinishedAt)
}

func TestJobManager_List_OrderedNewestFirst(t *testing.T) {
	m := NewJobManager()

	j1 := m.Create("deploy", "stack-a")
	time.Sleep(time.Millisecond) // ensure different timestamps
	j2 := m.Create("stop", "stack-b")
	time.Sleep(time.Millisecond)
	j3 := m.Create("pull", "stack-c")

	jobs := m.List()
	require.Len(t, jobs, 3)
	assert.Equal(t, j3.ID, jobs[0].ID)
	assert.Equal(t, j2.ID, jobs[1].ID)
	assert.Equal(t, j1.ID, jobs[2].ID)
}

func TestJobManager_List_CapsAt100(t *testing.T) {
	m := NewJobManager()
	for i := 0; i < 120; i++ {
		m.Create("deploy", "stack")
	}
	jobs := m.List()
	assert.Len(t, jobs, 100)
}

func TestJobManager_Cleanup(t *testing.T) {
	m := NewJobManager()

	// Create a completed job with a timestamp in the past
	old := m.Create("deploy", "old-stack")
	m.Start(old.ID)
	m.Complete(old.ID, "done", "")
	// Manually set CreatedAt to 2 hours ago
	m.mu.Lock()
	m.jobs[old.ID].CreatedAt = time.Now().UTC().Add(-2 * time.Hour)
	m.mu.Unlock()

	// Create a running job (should not be cleaned up)
	running := m.Create("deploy", "active-stack")
	m.Start(running.ID)

	// Create a recent completed job (should not be cleaned up)
	recent := m.Create("stop", "recent-stack")
	m.Start(recent.ID)
	m.Complete(recent.ID, "done", "")

	removed := m.Cleanup(1 * time.Hour)
	assert.Equal(t, 1, removed)

	assert.Nil(t, m.Get(old.ID), "old completed job should be removed")
	assert.NotNil(t, m.Get(running.ID), "running job should remain")
	assert.NotNil(t, m.Get(recent.ID), "recent completed job should remain")
}

func TestJobManager_Cleanup_FailedJobs(t *testing.T) {
	m := NewJobManager()

	failed := m.Create("deploy", "fail-stack")
	m.Start(failed.ID)
	m.Fail(failed.ID, "error")
	m.mu.Lock()
	m.jobs[failed.ID].CreatedAt = time.Now().UTC().Add(-2 * time.Hour)
	m.mu.Unlock()

	removed := m.Cleanup(1 * time.Hour)
	assert.Equal(t, 1, removed)
	assert.Nil(t, m.Get(failed.ID))
}

func TestJobManager_RunningCount(t *testing.T) {
	m := NewJobManager()
	assert.Equal(t, 0, m.RunningCount())

	j1 := m.Create("deploy", "a")
	m.Start(j1.ID)
	assert.Equal(t, 1, m.RunningCount())

	j2 := m.Create("stop", "b")
	m.Start(j2.ID)
	assert.Equal(t, 2, m.RunningCount())

	m.Complete(j1.ID, "", "")
	assert.Equal(t, 1, m.RunningCount())

	m.Fail(j2.ID, "err")
	assert.Equal(t, 0, m.RunningCount())
}

func TestJobManager_StartCleanup_StopsOnCancel(t *testing.T) {
	m := NewJobManager()
	ctx, cancel := context.WithCancel(context.Background())

	// Short interval for testing
	m.StartCleanup(ctx, 10*time.Millisecond, 50*time.Millisecond)

	// Create and complete a job
	j := m.Create("deploy", "stack")
	m.Start(j.ID)
	m.Complete(j.ID, "ok", "")

	// Backdate it
	m.mu.Lock()
	m.jobs[j.ID].CreatedAt = time.Now().UTC().Add(-1 * time.Hour)
	m.mu.Unlock()

	// Wait for cleanup to run
	time.Sleep(50 * time.Millisecond)
	assert.Nil(t, m.Get(j.ID), "job should be cleaned up")

	// Cancel and verify goroutine stops (no panics/leaks)
	cancel()
	time.Sleep(20 * time.Millisecond)
}

func TestJobManager_StartNonexistent(t *testing.T) {
	m := NewJobManager()
	// Should not panic
	m.Start("nonexistent")
	m.Complete("nonexistent", "", "")
	m.Fail("nonexistent", "")
}

func TestJobManager_UniqueIDs(t *testing.T) {
	m := NewJobManager()
	ids := make(map[string]bool)
	for i := 0; i < 100; i++ {
		j := m.Create("deploy", "stack")
		assert.False(t, ids[j.ID], "duplicate job ID: %s", j.ID)
		ids[j.ID] = true
	}
}

func TestJobManager_GetReturnsCopy(t *testing.T) {
	m := NewJobManager()
	job := m.Create("deploy", "stack-a")
	m.Start(job.ID)

	// Get a copy and mutate it
	got := m.Get(job.ID)
	require.NotNil(t, got)
	got.Status = JobFailed
	got.Output = "tampered"

	// Original should be unchanged
	original := m.Get(job.ID)
	assert.Equal(t, JobRunning, original.Status, "original should still be running")
	assert.Empty(t, original.Output, "original output should be empty")
}

func TestJobManager_ListReturnsCopies(t *testing.T) {
	m := NewJobManager()
	job := m.Create("deploy", "stack-a")
	m.Start(job.ID)

	jobs := m.List()
	require.Len(t, jobs, 1)

	// Mutate listed job
	jobs[0].Status = JobFailed
	jobs[0].Error = "tampered"

	// Original should be unchanged
	original := m.Get(job.ID)
	assert.Equal(t, JobRunning, original.Status)
	assert.Empty(t, original.Error)
}
