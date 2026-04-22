package app

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	domevent "github.com/erfianugrah/composer/internal/domain/event"
	"github.com/erfianugrah/composer/internal/domain/pipeline"
	"github.com/erfianugrah/composer/internal/infra/eventbus"
)

// mockPipelineRepo is an in-memory PipelineRepository.
type mockPipelineRepo struct {
	mu        sync.Mutex
	pipelines map[string]*pipeline.Pipeline
}

func newMockPipelineRepo() *mockPipelineRepo {
	return &mockPipelineRepo{pipelines: make(map[string]*pipeline.Pipeline)}
}

func (r *mockPipelineRepo) Create(_ context.Context, p *pipeline.Pipeline) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.pipelines[p.ID] = p
	return nil
}
func (r *mockPipelineRepo) GetByID(_ context.Context, id string) (*pipeline.Pipeline, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.pipelines[id], nil
}
func (r *mockPipelineRepo) List(_ context.Context) ([]*pipeline.Pipeline, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]*pipeline.Pipeline, 0, len(r.pipelines))
	for _, p := range r.pipelines {
		out = append(out, p)
	}
	return out, nil
}
func (r *mockPipelineRepo) Update(_ context.Context, p *pipeline.Pipeline) error { return nil }
func (r *mockPipelineRepo) Delete(_ context.Context, _ string) error             { return nil }

// mockRunRepo tracks Update calls to verify persist behavior.
type mockRunRepo struct {
	mu      sync.Mutex
	runs    map[string]*pipeline.Run
	updates atomic.Int32 // count of Update calls

	// updateCh is signalled on each Update if non-nil.
	updateCh chan struct{}
}

func newMockRunRepo() *mockRunRepo {
	return &mockRunRepo{runs: make(map[string]*pipeline.Run)}
}

func (r *mockRunRepo) Create(_ context.Context, run *pipeline.Run) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.runs[run.ID] = run
	return nil
}
func (r *mockRunRepo) GetByID(_ context.Context, id string) (*pipeline.Run, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	// Return a copy so callers see a snapshot.
	if orig, ok := r.runs[id]; ok {
		cp := *orig
		return &cp, nil
	}
	return nil, nil
}
func (r *mockRunRepo) ListByPipeline(_ context.Context, _ string) ([]*pipeline.Run, error) {
	return nil, nil
}
func (r *mockRunRepo) Update(_ context.Context, run *pipeline.Run) error {
	r.mu.Lock()
	// Store a copy to avoid races with caller mutating after return.
	cp := *run
	r.runs[run.ID] = &cp
	r.updates.Add(1)
	ch := r.updateCh
	r.mu.Unlock()
	if ch != nil {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
	return nil
}

// waitForUpdate blocks until at least one Update call occurs or timeout.
func (r *mockRunRepo) waitForUpdate(t *testing.T, timeout time.Duration) {
	t.Helper()
	select {
	case <-r.updateCh:
	case <-time.After(timeout):
		t.Fatal("timed out waiting for Update call")
	}
}

func TestPipelineService_Run_PersistsOnCompletion(t *testing.T) {
	bus := eventbus.NewMemoryBus(16)
	defer bus.Close()

	pipelineRepo := newMockPipelineRepo()
	runRepo := newMockRunRepo()
	runRepo.updateCh = make(chan struct{}, 8)
	executor := NewPipelineExecutor(nil, nil, bus, nil, nil, "", NewStackLocks())
	svc := NewPipelineService(pipelineRepo, runRepo, executor)
	defer svc.Stop()

	// Create a fast pipeline using the wait step (no external deps).
	p, err := pipeline.NewPipeline("test", "Fast pipe", "user1")
	require.NoError(t, err)
	err = p.AddStep(pipeline.Step{
		ID: "fast", Name: "FastWait", Type: pipeline.StepWait,
		Config: map[string]any{"duration": "10ms"},
	})
	require.NoError(t, err)
	err = pipelineRepo.Create(context.Background(), p)
	require.NoError(t, err)

	// Run it
	run, err := svc.Run(context.Background(), p.ID, "test")
	require.NoError(t, err)

	// Wait for executor goroutine to call Update (persist result).
	runRepo.waitForUpdate(t, 5*time.Second)

	// Executor goroutine should have persisted (context NOT cancelled)
	assert.GreaterOrEqual(t, runRepo.updates.Load(), int32(1), "executor should persist completed run")

	// Check persisted status
	persisted, err := runRepo.GetByID(context.Background(), run.ID)
	require.NoError(t, err)
	require.NotNil(t, persisted)
	assert.Equal(t, pipeline.RunSuccess, persisted.Status)
}

func TestPipelineService_CancelRun_ExecutorSkipsPersist(t *testing.T) {
	bus := eventbus.NewMemoryBus(16)
	defer bus.Close()

	pipelineRepo := newMockPipelineRepo()
	runRepo := newMockRunRepo()
	runRepo.updateCh = make(chan struct{}, 8)
	executor := NewPipelineExecutor(nil, nil, bus, nil, nil, "", NewStackLocks())
	svc := NewPipelineService(pipelineRepo, runRepo, executor)
	defer svc.Stop()

	// Create a slow pipeline (30s wait)
	p, err := pipeline.NewPipeline("test", "Slow pipe", "user1")
	require.NoError(t, err)
	err = p.AddStep(pipeline.Step{
		ID: "wait", Name: "Long wait", Type: pipeline.StepWait,
		Config: map[string]any{"duration": "30s"},
	})
	require.NoError(t, err)
	err = pipelineRepo.Create(context.Background(), p)
	require.NoError(t, err)

	// Run it (starts goroutine with 30s wait)
	run, err := svc.Run(context.Background(), p.ID, "test")
	require.NoError(t, err)

	// Give the goroutine time to start executing the wait step
	time.Sleep(100 * time.Millisecond)

	// Record updates before cancel
	updatesBefore := runRepo.updates.Load()

	// Cancel the run — this cancels the per-run context AND persists cancelled status
	err = svc.CancelRun(context.Background(), run)
	require.NoError(t, err)

	// Wait for goroutine to finish (service context still alive, only per-run cancelled)
	// The executor goroutine checks runCtx.Err() and should skip its own persist.
	// Give it time to drain.
	time.Sleep(200 * time.Millisecond)

	// CancelRun should have called Update exactly once.
	// Executor goroutine should NOT have called Update (per-run context cancelled).
	cancelUpdates := runRepo.updates.Load() - updatesBefore
	assert.Equal(t, int32(1), cancelUpdates, "only CancelRun should persist, not executor goroutine")

	// Verify final status is cancelled
	persisted, err := runRepo.GetByID(context.Background(), run.ID)
	require.NoError(t, err)
	require.NotNil(t, persisted)
	assert.Equal(t, pipeline.RunCancelled, persisted.Status)
}

func TestRunByWebhookTrigger_MatchesStackAndBranch(t *testing.T) {
	bus := eventbus.NewMemoryBus(16)
	defer bus.Close()

	pipelineRepo := newMockPipelineRepo()
	runRepo := newMockRunRepo()
	runRepo.updateCh = make(chan struct{}, 8)
	executor := NewPipelineExecutor(nil, nil, bus, nil, nil, "", NewStackLocks())
	svc := NewPipelineService(pipelineRepo, runRepo, executor)
	defer svc.Stop()

	// Pipeline with webhook trigger for "mystack" on "main"
	p, err := pipeline.NewPipeline("deploy-mystack", "Deploy mystack", "user1")
	require.NoError(t, err)
	p.Triggers = []pipeline.Trigger{{
		Type:   pipeline.TriggerWebhook,
		Config: map[string]any{"stack": "mystack", "branch": "main"},
	}}
	err = p.AddStep(pipeline.Step{
		ID: "fast", Name: "Fast", Type: pipeline.StepWait,
		Config: map[string]any{"duration": "10ms"},
	})
	require.NoError(t, err)
	err = pipelineRepo.Create(context.Background(), p)
	require.NoError(t, err)

	// Matching: stack=mystack, branch=main → should trigger
	svc.RunByWebhookTrigger(context.Background(), "mystack", "main")
	runRepo.waitForUpdate(t, 5*time.Second)

	// Verify a run was created and completed (mock ListByPipeline returns nil, use update count)
	assert.GreaterOrEqual(t, runRepo.updates.Load(), int32(1), "matching webhook should trigger run")
}

func TestRunByWebhookTrigger_SkipsNonMatchingStack(t *testing.T) {
	bus := eventbus.NewMemoryBus(16)
	defer bus.Close()

	pipelineRepo := newMockPipelineRepo()
	runRepo := newMockRunRepo()
	executor := NewPipelineExecutor(nil, nil, bus, nil, nil, "", NewStackLocks())
	svc := NewPipelineService(pipelineRepo, runRepo, executor)
	defer svc.Stop()

	p, err := pipeline.NewPipeline("deploy-other", "Deploy other", "user1")
	require.NoError(t, err)
	p.Triggers = []pipeline.Trigger{{
		Type:   pipeline.TriggerWebhook,
		Config: map[string]any{"stack": "other-stack", "branch": "main"},
	}}
	err = p.AddStep(pipeline.Step{
		ID: "fast", Name: "Fast", Type: pipeline.StepWait,
		Config: map[string]any{"duration": "10ms"},
	})
	require.NoError(t, err)
	err = pipelineRepo.Create(context.Background(), p)
	require.NoError(t, err)

	// Non-matching stack → should NOT trigger
	svc.RunByWebhookTrigger(context.Background(), "mystack", "main")
	time.Sleep(100 * time.Millisecond)

	assert.Equal(t, int32(0), runRepo.updates.Load(), "non-matching stack should not trigger run")
}

func TestRunByWebhookTrigger_SkipsNonMatchingBranch(t *testing.T) {
	bus := eventbus.NewMemoryBus(16)
	defer bus.Close()

	pipelineRepo := newMockPipelineRepo()
	runRepo := newMockRunRepo()
	executor := NewPipelineExecutor(nil, nil, bus, nil, nil, "", NewStackLocks())
	svc := NewPipelineService(pipelineRepo, runRepo, executor)
	defer svc.Stop()

	p, err := pipeline.NewPipeline("deploy-branch", "Deploy on main only", "user1")
	require.NoError(t, err)
	p.Triggers = []pipeline.Trigger{{
		Type:   pipeline.TriggerWebhook,
		Config: map[string]any{"stack": "mystack", "branch": "main"},
	}}
	err = p.AddStep(pipeline.Step{
		ID: "fast", Name: "Fast", Type: pipeline.StepWait,
		Config: map[string]any{"duration": "10ms"},
	})
	require.NoError(t, err)
	err = pipelineRepo.Create(context.Background(), p)
	require.NoError(t, err)

	// Matching stack but wrong branch → should NOT trigger
	svc.RunByWebhookTrigger(context.Background(), "mystack", "develop")
	time.Sleep(100 * time.Millisecond)

	assert.Equal(t, int32(0), runRepo.updates.Load(), "non-matching branch should not trigger run")
}

func TestRunByWebhookTrigger_EmptyBranchMatchesAny(t *testing.T) {
	bus := eventbus.NewMemoryBus(16)
	defer bus.Close()

	pipelineRepo := newMockPipelineRepo()
	runRepo := newMockRunRepo()
	runRepo.updateCh = make(chan struct{}, 8)
	executor := NewPipelineExecutor(nil, nil, bus, nil, nil, "", NewStackLocks())
	svc := NewPipelineService(pipelineRepo, runRepo, executor)
	defer svc.Stop()

	// Pipeline with no branch filter → matches any branch
	p, err := pipeline.NewPipeline("deploy-any", "Deploy any branch", "user1")
	require.NoError(t, err)
	p.Triggers = []pipeline.Trigger{{
		Type:   pipeline.TriggerWebhook,
		Config: map[string]any{"stack": "mystack"},
	}}
	err = p.AddStep(pipeline.Step{
		ID: "fast", Name: "Fast", Type: pipeline.StepWait,
		Config: map[string]any{"duration": "10ms"},
	})
	require.NoError(t, err)
	err = pipelineRepo.Create(context.Background(), p)
	require.NoError(t, err)

	// Any branch should trigger when trigger has no branch filter
	svc.RunByWebhookTrigger(context.Background(), "mystack", "feature/x")
	runRepo.waitForUpdate(t, 5*time.Second)

	assert.GreaterOrEqual(t, runRepo.updates.Load(), int32(1), "empty branch filter should match any branch")
}

// --- Event trigger tests ---

// newEventTriggerFixture wires a PipelineService to a bus, returns both plus
// a helper for asserting trigger dispatch. Shared by the event tests below.
func newEventTriggerFixture(t *testing.T) (*PipelineService, *eventbus.MemoryBus, *mockPipelineRepo, *mockRunRepo) {
	t.Helper()
	bus := eventbus.NewMemoryBus(16)
	t.Cleanup(func() { bus.Close() })

	pipelineRepo := newMockPipelineRepo()
	runRepo := newMockRunRepo()
	runRepo.updateCh = make(chan struct{}, 8)
	executor := NewPipelineExecutor(nil, nil, bus, nil, nil, "", NewStackLocks())
	svc := NewPipelineService(pipelineRepo, runRepo, executor)
	t.Cleanup(svc.Stop)
	svc.SubscribeBus(bus)

	return svc, bus, pipelineRepo, runRepo
}

func TestSubscribeBus_NilBus(t *testing.T) {
	// Passing nil must not panic — supports tests / minimal configs.
	svc := NewPipelineService(newMockPipelineRepo(), newMockRunRepo(), nil)
	defer svc.Stop()

	assert.NotPanics(t, func() { svc.SubscribeBus(nil) })
}

func TestSubscribeBus_EventTrigger_MatchingStack(t *testing.T) {
	// A pipeline with an event trigger for stack.deployed + stack=caddy
	// fires when StackDeployed{Name: "caddy"} is published.
	svc, bus, pipelineRepo, runRepo := newEventTriggerFixture(t)

	p, err := pipeline.NewPipeline("reload-caddy", "Reload after deploy", "user1")
	require.NoError(t, err)
	p.Triggers = []pipeline.Trigger{{
		Type:   pipeline.TriggerEvent,
		Config: map[string]any{"event": "stack.deployed", "stack": "caddy"},
	}}
	require.NoError(t, p.AddStep(pipeline.Step{
		ID: "tick", Name: "Tick", Type: pipeline.StepWait,
		Config: map[string]any{"duration": "10ms"},
	}))
	require.NoError(t, pipelineRepo.Create(context.Background(), p))

	bus.Publish(domevent.StackDeployed{Name: "caddy", Timestamp: time.Now()})
	runRepo.waitForUpdate(t, 5*time.Second)

	assert.GreaterOrEqual(t, runRepo.updates.Load(), int32(1), "matching event should have triggered a run")
	_ = svc
}

func TestSubscribeBus_EventTrigger_StackMismatch(t *testing.T) {
	// Stack filter on the trigger excludes events for other stacks.
	_, bus, pipelineRepo, runRepo := newEventTriggerFixture(t)

	p, err := pipeline.NewPipeline("reload-caddy", "Reload", "user1")
	require.NoError(t, err)
	p.Triggers = []pipeline.Trigger{{
		Type:   pipeline.TriggerEvent,
		Config: map[string]any{"event": "stack.deployed", "stack": "caddy"},
	}}
	require.NoError(t, p.AddStep(pipeline.Step{
		ID: "tick", Name: "Tick", Type: pipeline.StepWait,
		Config: map[string]any{"duration": "10ms"},
	}))
	require.NoError(t, pipelineRepo.Create(context.Background(), p))

	// Event for a different stack — should not trigger
	bus.Publish(domevent.StackDeployed{Name: "atuin", Timestamp: time.Now()})

	// Wait a short while then confirm no run was dispatched
	time.Sleep(100 * time.Millisecond)
	assert.Equal(t, int32(0), runRepo.updates.Load(), "different stack should not trigger pipeline")
}

func TestSubscribeBus_EventTrigger_EventMismatch(t *testing.T) {
	// Event type mismatch: trigger wants stack.deployed, publisher sends stack.stopped.
	_, bus, pipelineRepo, runRepo := newEventTriggerFixture(t)

	p, err := pipeline.NewPipeline("reload-caddy", "Reload", "user1")
	require.NoError(t, err)
	p.Triggers = []pipeline.Trigger{{
		Type:   pipeline.TriggerEvent,
		Config: map[string]any{"event": "stack.deployed", "stack": "caddy"},
	}}
	require.NoError(t, p.AddStep(pipeline.Step{
		ID: "tick", Name: "Tick", Type: pipeline.StepWait,
		Config: map[string]any{"duration": "10ms"},
	}))
	require.NoError(t, pipelineRepo.Create(context.Background(), p))

	bus.Publish(domevent.StackStopped{Name: "caddy", Timestamp: time.Now()})

	time.Sleep(100 * time.Millisecond)
	assert.Equal(t, int32(0), runRepo.updates.Load(), "mismatched event type should not trigger")
}

func TestSubscribeBus_EventTrigger_EmptyStackMatchesAll(t *testing.T) {
	// Trigger with no stack filter matches every stack for a given event type.
	_, bus, pipelineRepo, runRepo := newEventTriggerFixture(t)

	p, err := pipeline.NewPipeline("global-reload", "Reload any stack", "user1")
	require.NoError(t, err)
	p.Triggers = []pipeline.Trigger{{
		Type:   pipeline.TriggerEvent,
		Config: map[string]any{"event": "stack.deployed"}, // no stack filter
	}}
	require.NoError(t, p.AddStep(pipeline.Step{
		ID: "tick", Name: "Tick", Type: pipeline.StepWait,
		Config: map[string]any{"duration": "10ms"},
	}))
	require.NoError(t, pipelineRepo.Create(context.Background(), p))

	// Any stack should trigger when no stack filter is set
	bus.Publish(domevent.StackDeployed{Name: "any-stack-name", Timestamp: time.Now()})
	runRepo.waitForUpdate(t, 5*time.Second)

	assert.GreaterOrEqual(t, runRepo.updates.Load(), int32(1), "empty stack filter should match any stack")
}

func TestSubscribeBus_EventTrigger_IgnoresNonStackEvents(t *testing.T) {
	// Non-stack events (pipeline run lifecycle, container state) must not be
	// used to trigger pipelines — the config key is `event: stack.*` by contract.
	_, bus, pipelineRepo, runRepo := newEventTriggerFixture(t)

	p, err := pipeline.NewPipeline("listens", "Listens", "user1")
	require.NoError(t, err)
	p.Triggers = []pipeline.Trigger{{
		Type:   pipeline.TriggerEvent,
		Config: map[string]any{"event": "pipeline.run.finished"},
	}}
	require.NoError(t, p.AddStep(pipeline.Step{
		ID: "tick", Name: "Tick", Type: pipeline.StepWait,
		Config: map[string]any{"duration": "10ms"},
	}))
	require.NoError(t, pipelineRepo.Create(context.Background(), p))

	// Publishing a pipeline run finished event should NOT dispatch this pipeline
	// (the type switch in SubscribeBus only matches stack events by design).
	bus.Publish(domevent.PipelineRunFinished{
		PipelineID: "pl_other", RunID: "r_1", Status: "success",
		Duration: "1s", Timestamp: time.Now(),
	})

	time.Sleep(100 * time.Millisecond)
	assert.Equal(t, int32(0), runRepo.updates.Load(), "non-stack events should not trigger pipelines")
}

func TestTriggerType_EventValue(t *testing.T) {
	// Guard the string value — the DTO enum and docs all embed `event`.
	assert.Equal(t, pipeline.TriggerType("event"), pipeline.TriggerEvent)
}
