package app

import (
	"context"
	"crypto/rand"
	"fmt"
	"slices"
	"sync"
	"time"
)

// JobStatus represents the state of a background job.
type JobStatus string

const (
	JobPending   JobStatus = "pending"
	JobRunning   JobStatus = "running"
	JobCompleted JobStatus = "completed"
	JobFailed    JobStatus = "failed"
)

// Job tracks a long-running background operation.
type Job struct {
	ID         string     `json:"id"`
	Type       string     `json:"type"`   // "deploy", "build", "pull", "image_pull", etc.
	Target     string     `json:"target"` // stack name or image ref
	Status     JobStatus  `json:"status"`
	Output     string     `json:"output"` // stdout
	Error      string     `json:"error"`  // stderr or error message
	CreatedAt  time.Time  `json:"created_at"`
	StartedAt  *time.Time `json:"started_at,omitempty"`
	FinishedAt *time.Time `json:"finished_at,omitempty"`
}

// JobManager tracks background jobs in memory.
type JobManager struct {
	mu   sync.RWMutex
	jobs map[string]*Job
}

func NewJobManager() *JobManager {
	return &JobManager{jobs: make(map[string]*Job)}
}

// Create creates a new pending job and returns it.
func (m *JobManager) Create(jobType, target string) *Job {
	var buf [8]byte
	rand.Read(buf[:])
	job := &Job{
		ID:        fmt.Sprintf("job_%x", buf),
		Type:      jobType,
		Target:    target,
		Status:    JobPending,
		CreatedAt: time.Now().UTC(),
	}
	m.mu.Lock()
	m.jobs[job.ID] = job
	m.mu.Unlock()
	return job
}

// Start marks a job as running.
func (m *JobManager) Start(id string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if j, ok := m.jobs[id]; ok {
		j.Status = JobRunning
		now := time.Now().UTC()
		j.StartedAt = &now
	}
}

// Complete marks a job as completed with output.
func (m *JobManager) Complete(id, output, errOutput string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if j, ok := m.jobs[id]; ok {
		j.Status = JobCompleted
		j.Output = output
		j.Error = errOutput
		now := time.Now().UTC()
		j.FinishedAt = &now
	}
}

// Fail marks a job as failed with error.
func (m *JobManager) Fail(id, errMsg string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if j, ok := m.jobs[id]; ok {
		j.Status = JobFailed
		j.Error = errMsg
		now := time.Now().UTC()
		j.FinishedAt = &now
	}
}

// Get returns a job by ID (returns a copy to avoid data races).
func (m *JobManager) Get(id string) *Job {
	m.mu.RLock()
	defer m.mu.RUnlock()
	j, ok := m.jobs[id]
	if !ok {
		return nil
	}
	cp := *j
	return &cp
}

// List returns all jobs, newest first. Caps at 100.
func (m *JobManager) List() []*Job {
	m.mu.RLock()
	defer m.mu.RUnlock()
	jobs := make([]*Job, 0, len(m.jobs))
	for _, j := range m.jobs {
		cp := *j
		jobs = append(jobs, &cp)
	}
	// Sort by created_at descending
	slices.SortFunc(jobs, func(a, b *Job) int {
		return b.CreatedAt.Compare(a.CreatedAt) // descending
	})
	if len(jobs) > 100 {
		jobs = jobs[:100]
	}
	return jobs
}

// Cleanup removes completed/failed jobs older than the given duration.
func (m *JobManager) Cleanup(maxAge time.Duration) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	cutoff := time.Now().UTC().Add(-maxAge)
	removed := 0
	for id, j := range m.jobs {
		if (j.Status == JobCompleted || j.Status == JobFailed) && j.CreatedAt.Before(cutoff) {
			delete(m.jobs, id)
			removed++
		}
	}
	return removed
}

// RunningCount returns the number of currently running jobs.
func (m *JobManager) RunningCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	n := 0
	for _, j := range m.jobs {
		if j.Status == JobRunning {
			n++
		}
	}
	return n
}

// StartCleanup runs a periodic cleanup goroutine. It removes completed/failed
// jobs older than maxAge every interval. Stops when ctx is cancelled.
func (m *JobManager) StartCleanup(ctx context.Context, interval, maxAge time.Duration) {
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				m.Cleanup(maxAge)
			case <-ctx.Done():
				return
			}
		}
	}()
}
