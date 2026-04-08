package app

import (
	"context"
	"fmt"
	"sync"

	"go.uber.org/zap"

	"github.com/erfianugrah/composer/internal/domain/pipeline"
)

// PipelineService orchestrates pipeline CRUD and execution.
type PipelineService struct {
	pipelines pipeline.PipelineRepository
	runs      pipeline.RunRepository
	executor  *PipelineExecutor
	wg        sync.WaitGroup
	cancel    context.CancelFunc
	runCtx    context.Context
	logger    *zap.Logger
}

func NewPipelineService(
	pipelines pipeline.PipelineRepository,
	runs pipeline.RunRepository,
	executor *PipelineExecutor,
) *PipelineService {
	ctx, cancel := context.WithCancel(context.Background())
	return &PipelineService{
		pipelines: pipelines,
		runs:      runs,
		executor:  executor,
		cancel:    cancel,
		runCtx:    ctx,
	}
}

// Stop cancels all in-flight pipeline runs and waits for them to finish.
func (s *PipelineService) Stop() {
	s.cancel()
	s.wg.Wait()
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

	// Execute asynchronously with cancellable context and WaitGroup tracking
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		result := s.executor.Execute(s.runCtx, p, run)
		if err := s.runs.Update(context.Background(), result); err != nil && s.logger != nil {
			s.logger.Warn("failed to update pipeline run", zap.String("run_id", run.ID), zap.Error(err))
		}
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

// UpdateRun persists a run's current state (e.g. after cancellation).
func (s *PipelineService) UpdateRun(ctx context.Context, run *pipeline.Run) error {
	return s.runs.Update(ctx, run)
}
