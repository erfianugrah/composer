package pipeline

import "time"

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
	return &Run{
		ID:          "run_" + now.Format("20060102150405"),
		PipelineID:  pipelineID,
		Status:      RunPending,
		TriggeredBy: triggeredBy,
		StepResults: []StepResult{},
		CreatedAt:   now,
	}
}

// Start marks the run as started.
func (r *Run) Start() {
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

// Complete marks the run as successfully finished.
func (r *Run) Complete() {
	r.Status = RunSuccess
	now := time.Now().UTC()
	r.FinishedAt = &now
}

// Cancel marks the run as cancelled.
func (r *Run) Cancel() {
	r.Status = RunCancelled
	now := time.Now().UTC()
	r.FinishedAt = &now
}

// Fail marks the run as failed.
func (r *Run) Fail() {
	r.Status = RunFailed
	now := time.Now().UTC()
	r.FinishedAt = &now
}
