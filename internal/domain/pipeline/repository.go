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

// RunRepository persists pipeline run records.
type RunRepository interface {
	Create(ctx context.Context, run *Run) error
	GetByID(ctx context.Context, id string) (*Run, error)
	ListByPipeline(ctx context.Context, pipelineID string) ([]*Run, error)
	Update(ctx context.Context, run *Run) error
}
