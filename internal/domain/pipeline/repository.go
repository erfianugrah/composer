package pipeline

import "context"

// PipelineRepository persists pipeline configurations.
type PipelineRepository interface {
	Create(ctx context.Context, p *Pipeline) error
	GetByID(ctx context.Context, id string) (*Pipeline, error)
	List(ctx context.Context) ([]*Pipeline, error)
	Update(ctx context.Context, p *Pipeline) error
	Delete(ctx context.Context, id string) error
}

// ListRunsOptions controls pagination and ordering for run history queries.
// Zero value is "first 50, newest first" — matches pre-pagination behaviour.
type ListRunsOptions struct {
	// Limit caps the number of rows returned. Clamped to [1, 100] by the
	// repository; 0 means use the repository default (50).
	Limit int
	// Offset skips this many rows. Negative values are treated as 0.
	Offset int
	// Order is the sort direction over created_at. Only "asc" and "desc" are
	// honoured; any other value (including empty) falls back to "desc".
	Order string
}

// RunRepository persists pipeline run records.
type RunRepository interface {
	Create(ctx context.Context, run *Run) error
	GetByID(ctx context.Context, id string) (*Run, error)
	ListByPipeline(ctx context.Context, pipelineID string, opts ListRunsOptions) ([]*Run, error)
	Update(ctx context.Context, run *Run) error
}
