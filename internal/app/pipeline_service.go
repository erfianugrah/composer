package app

import (
	"context"
	"fmt"
	"sync"

	"go.uber.org/zap"

	domevent "github.com/erfianugrah/composer/internal/domain/event"
	"github.com/erfianugrah/composer/internal/domain/pipeline"
)

// SetLogger sets the logger for async operations (webhook triggers, etc.).
func (s *PipelineService) SetLogger(l *zap.Logger) { s.logger = l }

// PipelineService orchestrates pipeline CRUD and execution.
type PipelineService struct {
	pipelines  pipeline.PipelineRepository
	runs       pipeline.RunRepository
	executor   *PipelineExecutor
	wg         sync.WaitGroup
	cancel     context.CancelFunc
	runCtx     context.Context
	logger     *zap.Logger
	runCancels sync.Map // map[runID]context.CancelFunc -- per-run cancellation
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
// Returns a snapshot of the run (not the live pointer used by the executor)
// so callers cannot race with the executor goroutine.
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

	// Snapshot before handing the live pointer to the executor goroutine.
	// Callers get a copy — the executor owns the original exclusively.
	snapshot := *run

	// Execute asynchronously with per-run cancellable context
	runCtx, runCancel := context.WithCancel(s.runCtx)
	s.runCancels.Store(run.ID, runCancel)

	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		defer s.runCancels.Delete(run.ID)
		defer runCancel()

		result := s.executor.Execute(runCtx, p, run)
		// Only persist if the run wasn't cancelled externally.
		// CancelRun handles persistence for cancelled runs to avoid last-write-wins race.
		if runCtx.Err() == nil {
			if err := s.runs.Update(context.Background(), result); err != nil && s.logger != nil {
				s.logger.Warn("failed to update pipeline run", zap.String("run_id", run.ID), zap.Error(err))
			}
		}
	}()

	return &snapshot, nil
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

// CancelRun cancels a running pipeline's context and persists the cancelled status.
func (s *PipelineService) CancelRun(ctx context.Context, run *pipeline.Run) error {
	// Cancel the goroutine's context if it's still running
	if cancelFn, ok := s.runCancels.Load(run.ID); ok {
		cancelFn.(context.CancelFunc)()
	}

	run.Cancel()
	return s.runs.Update(ctx, run)
}

// SubscribeBus registers the pipeline service as an event bus subscriber so
// pipelines with `event` triggers fire in response to domain events.
// Call once at startup after wiring the bus.
//
// This is additive — pipelines with `webhook`, `manual`, or `schedule` triggers
// are unaffected. Event triggers fire AFTER the publishing operation completes,
// making them the right choice for post-deploy hooks. Webhook triggers fire
// immediately on receipt (pre-sync) — use them for pre-deploy workflows.
//
// A pipeline with both webhook and event triggers will fire twice per push.
// Users who want either pre-sync or post-deploy pick one.
func (s *PipelineService) SubscribeBus(bus domevent.Bus) {
	if bus == nil {
		return
	}
	bus.Subscribe(func(evt domevent.Event) bool {
		// Only dispatch on events that carry a stack name. Extend the type
		// switch as additional stack-scoped events need to trigger pipelines.
		var stackName string
		switch e := evt.(type) {
		case domevent.StackCreated:
			stackName = e.Name
		case domevent.StackDeployed:
			stackName = e.Name
		case domevent.StackStopped:
			stackName = e.Name
		case domevent.StackUpdated:
			stackName = e.Name
		case domevent.StackDeleted:
			stackName = e.Name
		case domevent.StackError:
			stackName = e.Name
		default:
			return true // keep subscription, ignore non-stack events
		}
		s.runByEventTrigger(evt.EventType(), stackName)
		return true
	})
}

// runByEventTrigger finds pipelines with `event` triggers matching the given
// event type and stack name, then spawns runs.
//
// Dispatch happens in the bus callback's goroutine; Run() is internally async
// via its own goroutine so this function does not block the publisher.
//
// Uses context.Background() because the bus publisher's ctx may be short-lived
// (e.g. SyncAndRedeploy's timeout ctx closes after Publish). Our pipeline
// run lifetime is independent of the publisher.
func (s *PipelineService) runByEventTrigger(eventType, stackName string) {
	ctx := context.Background()
	all, err := s.pipelines.List(ctx)
	if err != nil {
		if s.logger != nil {
			s.logger.Error("listing pipelines for event trigger", zap.Error(err))
		}
		return
	}
	for _, p := range all {
		for _, t := range p.Triggers {
			if t.Type != pipeline.TriggerEvent {
				continue
			}
			triggerEvent, _ := t.Config["event"].(string)
			triggerStack, _ := t.Config["stack"].(string)
			if triggerEvent != eventType {
				continue
			}
			// Empty stack filter matches any stack
			if triggerStack != "" && triggerStack != stackName {
				continue
			}
			if s.logger != nil {
				s.logger.Info("event triggered pipeline",
					zap.String("pipeline", p.Name),
					zap.String("event", eventType),
					zap.String("stack", stackName))
			}
			triggeredBy := fmt.Sprintf("event:%s:%s", eventType, stackName)
			if _, err := s.Run(ctx, p.ID, triggeredBy); err != nil {
				if s.logger != nil {
					s.logger.Error("failed to run event-triggered pipeline",
						zap.String("pipeline", p.Name),
						zap.Error(err))
				}
			}
		}
	}
}

// RunByWebhookTrigger finds pipelines with webhook triggers matching the
// stack name and branch, then runs them asynchronously.
func (s *PipelineService) RunByWebhookTrigger(ctx context.Context, stackName, branch string) {
	all, err := s.pipelines.List(ctx)
	if err != nil {
		if s.logger != nil {
			s.logger.Error("listing pipelines for webhook trigger", zap.Error(err))
		}
		return
	}
	for _, p := range all {
		for _, t := range p.Triggers {
			if t.Type != pipeline.TriggerWebhook {
				continue
			}
			triggerStack, _ := t.Config["stack"].(string)
			triggerBranch, _ := t.Config["branch"].(string)
			if triggerStack != stackName {
				continue
			}
			if triggerBranch != "" && triggerBranch != branch {
				continue
			}
			if s.logger != nil {
				s.logger.Info("webhook triggered pipeline",
					zap.String("pipeline", p.Name),
					zap.String("stack", stackName))
			}
			if _, err := s.Run(ctx, p.ID, "webhook:"+stackName); err != nil {
				if s.logger != nil {
					s.logger.Error("failed to run webhook-triggered pipeline",
						zap.String("pipeline", p.Name),
						zap.Error(err))
				}
			}
		}
	}
}
