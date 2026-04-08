package app

import (
	"context"
	"fmt"

	"github.com/erfianugrah/composer/internal/domain/pipeline"
)

// PipelineService orchestrates pipeline CRUD and execution.
type PipelineService struct {
	pipelines pipeline.PipelineRepository
	runs      pipeline.RunRepository
	executor  *PipelineExecutor
}

func NewPipelineService(
	pipelines pipeline.PipelineRepository,
	runs pipeline.RunRepository,
	executor *PipelineExecutor,
) *PipelineService {
	return &PipelineService{pipelines: pipelines, runs: runs, executor: executor}
}

func (s *PipelineService) Create(ctx context.Context, name, description, createdBy string, steps []pipeline.Step, triggers []pipeline.Trigger) (*pipeline.Pipeline, error) {
	p, err := pipeline.NewPipeline(name, description, createdBy)
	if err != nil {
		return nil, err
	}

	for _, step := range steps {
		if err := p.AddStep(step); err != nil {
			return nil, fmt.Errorf("adding step %q: %w", step.ID, err)
		}
	}
	p.Triggers = triggers

	if err := p.Validate(); err != nil {
		return nil, err
	}

	if err := s.pipelines.Create(ctx, p); err != nil {
		return nil, fmt.Errorf("persisting pipeline: %w", err)
	}

	return p, nil
}

func (s *PipelineService) Get(ctx context.Context, id string) (*pipeline.Pipeline, error) {
	p, err := s.pipelines.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if p == nil {
		return nil, ErrNotFound
	}
	return p, nil
}

func (s *PipelineService) List(ctx context.Context) ([]*pipeline.Pipeline, error) {
	return s.pipelines.List(ctx)
}

func (s *PipelineService) Update(ctx context.Context, p *pipeline.Pipeline) error {
	if err := p.Validate(); err != nil {
		return err
	}
	return s.pipelines.Update(ctx, p)
}

func (s *PipelineService) Delete(ctx context.Context, id string) error {
	return s.pipelines.Delete(ctx, id)
}

// Run triggers a pipeline execution. Runs asynchronously in a goroutine.
func (s *PipelineService) Run(ctx context.Context, pipelineID, triggeredBy string) (*pipeline.Run, error) {
	p, err := s.pipelines.GetByID(ctx, pipelineID)
	if err != nil {
		return nil, err
	}
	if p == nil {
		return nil, ErrNotFound
	}

	run := pipeline.NewRun(pipelineID, triggeredBy)
	if err := s.runs.Create(ctx, run); err != nil {
		return nil, fmt.Errorf("persisting run: %w", err)
	}

	// Execute asynchronously
	go func() {
		result := s.executor.Execute(context.Background(), p, run)
		s.runs.Update(context.Background(), result)
	}()

	return run, nil
}

func (s *PipelineService) GetRun(ctx context.Context, runID string) (*pipeline.Run, error) {
	run, err := s.runs.GetByID(ctx, runID)
	if err != nil {
		return nil, err
	}
	if run == nil {
		return nil, ErrNotFound
	}
	return run, nil
}

func (s *PipelineService) ListRuns(ctx context.Context, pipelineID string) ([]*pipeline.Run, error) {
	return s.runs.ListByPipeline(ctx, pipelineID)
}
