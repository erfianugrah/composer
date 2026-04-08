package pipeline

import (
	"crypto/rand"
	"fmt"
	"time"
)

// RunStatus tracks the execution state of a pipeline run.
type RunStatus string

const (
	RunPending   RunStatus = "pending"
	RunRunning   RunStatus = "running"
	RunSuccess   RunStatus = "success"
	RunFailed    RunStatus = "failed"
	RunCancelled RunStatus = "cancelled"
)

// Run tracks the execution of a pipeline.
type Run struct {
	ID          string
	PipelineID  string
	Status      RunStatus
	TriggeredBy string
	StartedAt   *time.Time
	FinishedAt  *time.Time
	StepResults []StepResult
	CreatedAt   time.Time
}

// StepResult records the outcome of executing a single step.
type StepResult struct {
	StepID     string
	StepName   string
	Status     RunStatus
	Output     string
	Error      string
	Duration   time.Duration
	StartedAt  *time.Time
	FinishedAt *time.Time
}

// NewRun creates a new pipeline run in pending state.
func NewRun(pipelineID, triggeredBy string) *Run {
	now := time.Now().UTC()
	var buf [4]byte
	rand.Read(buf[:])
	return &Run{
		ID:          fmt.Sprintf("run_%s_%x", now.Format("20060102150405"), buf),
		PipelineID:  pipelineID,
		Status:      RunPending,
		TriggeredBy: triggeredBy,
		StepResults: []StepResult{},
		CreatedAt:   now,
	}
}

// Start marks the run as started. No-op if not in pending state.
func (r *Run) Start() {
	if r.Status != RunPending {
		return
	}
	now := time.Now().UTC()
	r.Status = RunRunning
	r.StartedAt = &now
}

// RecordStepResult adds a step result and updates run status.
func (r *Run) RecordStepResult(result StepResult) {
	r.StepResults = append(r.StepResults, result)

	if result.Status == RunFailed {
		// Check if the step has continue_on_error (not tracked here -- handled by executor)
		// Default: first failure fails the run
		r.Status = RunFailed
		now := time.Now().UTC()
		r.FinishedAt = &now
	}
}

// Complete marks the run as successfully finished. No-op if already in a terminal state.
func (r *Run) Complete() {
	if r.Status != RunRunning {
		return
	}
	r.Status = RunSuccess
	now := time.Now().UTC()
	r.FinishedAt = &now
}

// Cancel marks the run as cancelled. No-op if already in a terminal state.
func (r *Run) Cancel() {
	if r.Status != RunPending && r.Status != RunRunning {
		return
	}
	r.Status = RunCancelled
	now := time.Now().UTC()
	r.FinishedAt = &now
}

// Fail marks the run as failed. No-op if already in a terminal state.
func (r *Run) Fail() {
	if r.Status != RunPending && r.Status != RunRunning {
		return
	}
	r.Status = RunFailed
	now := time.Now().UTC()
	r.FinishedAt = &now
}
