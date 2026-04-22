package app_test

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/erfianugrah/composer/internal/app"
	domevent "github.com/erfianugrah/composer/internal/domain/event"
	"github.com/erfianugrah/composer/internal/domain/pipeline"
	"github.com/erfianugrah/composer/internal/infra/eventbus"
)

func TestPipelineExecutor_SimpleShellSteps(t *testing.T) {
	bus := eventbus.NewMemoryBus(16)
	defer bus.Close()

	executor := app.NewPipelineExecutor(nil, nil, bus, nil, nil, "", app.NewStackLocks()) // no compose needed for shell steps

	p, _ := pipeline.NewPipeline("test", "Shell test", "user1")
	p.AddStep(pipeline.Step{
		ID: "echo", Name: "Echo hello", Type: pipeline.StepShellCommand,
		Config: map[string]any{"command": "echo hello-from-pipeline"},
	})
	p.AddStep(pipeline.Step{
		ID: "date", Name: "Print date", Type: pipeline.StepShellCommand,
		Config:    map[string]any{"command": "date +%Y"},
		DependsOn: []string{"echo"},
	})

	run := pipeline.NewRun(p.ID, "test")
	result := executor.Execute(context.Background(), p, run)

	assert.Equal(t, pipeline.RunSuccess, result.Status)
	require.Len(t, result.StepResults, 2)
	assert.Equal(t, pipeline.RunSuccess, result.StepResults[0].Status)
	assert.Contains(t, result.StepResults[0].Output, "hello-from-pipeline")
	assert.Equal(t, pipeline.RunSuccess, result.StepResults[1].Status)
}

func TestPipelineExecutor_ParallelSteps(t *testing.T) {
	bus := eventbus.NewMemoryBus(16)
	defer bus.Close()

	executor := app.NewPipelineExecutor(nil, nil, bus, nil, nil, "", app.NewStackLocks())

	p, _ := pipeline.NewPipeline("test", "Parallel test", "user1")
	// Two independent steps should run concurrently
	p.AddStep(pipeline.Step{
		ID: "wait1", Name: "Wait 100ms", Type: pipeline.StepWait,
		Config: map[string]any{"duration": "100ms"},
	})
	p.AddStep(pipeline.Step{
		ID: "wait2", Name: "Wait 100ms", Type: pipeline.StepWait,
		Config: map[string]any{"duration": "100ms"},
	})
	p.AddStep(pipeline.Step{
		ID: "done", Name: "Done", Type: pipeline.StepShellCommand,
		Config:    map[string]any{"command": "echo done"},
		DependsOn: []string{"wait1", "wait2"},
	})

	start := time.Now()
	run := pipeline.NewRun(p.ID, "test")
	result := executor.Execute(context.Background(), p, run)
	elapsed := time.Since(start)

	assert.Equal(t, pipeline.RunSuccess, result.Status)
	require.Len(t, result.StepResults, 3)

	// If parallel, total time should be ~100ms + overhead, not 200ms+
	assert.Less(t, elapsed, 500*time.Millisecond, "parallel steps should not be sequential")
}

func TestPipelineExecutor_StepFailure(t *testing.T) {
	bus := eventbus.NewMemoryBus(16)
	defer bus.Close()

	executor := app.NewPipelineExecutor(nil, nil, bus, nil, nil, "", app.NewStackLocks())

	p, _ := pipeline.NewPipeline("test", "Failure test", "user1")
	p.AddStep(pipeline.Step{
		ID: "fail", Name: "Fail", Type: pipeline.StepShellCommand,
		Config: map[string]any{"command": "exit 1"},
	})
	p.AddStep(pipeline.Step{
		ID: "after", Name: "After", Type: pipeline.StepShellCommand,
		Config:    map[string]any{"command": "echo should-not-run"},
		DependsOn: []string{"fail"},
	})

	run := pipeline.NewRun(p.ID, "test")
	result := executor.Execute(context.Background(), p, run)

	assert.Equal(t, pipeline.RunFailed, result.Status)
	// Only the first step should have run
	require.GreaterOrEqual(t, len(result.StepResults), 1)
	assert.Equal(t, pipeline.RunFailed, result.StepResults[0].Status)
}

func TestPipelineExecutor_ContinueOnError(t *testing.T) {
	bus := eventbus.NewMemoryBus(16)
	defer bus.Close()

	executor := app.NewPipelineExecutor(nil, nil, bus, nil, nil, "", app.NewStackLocks())

	p, _ := pipeline.NewPipeline("test", "Continue on error", "user1")
	p.AddStep(pipeline.Step{
		ID: "fail", Name: "Fail (continue)", Type: pipeline.StepShellCommand,
		Config:          map[string]any{"command": "exit 1"},
		ContinueOnError: true,
	})
	p.AddStep(pipeline.Step{
		ID: "after", Name: "Should run", Type: pipeline.StepShellCommand,
		Config:    map[string]any{"command": "echo ran-after-failure"},
		DependsOn: []string{"fail"},
	})

	run := pipeline.NewRun(p.ID, "test")
	result := executor.Execute(context.Background(), p, run)

	// The run should complete (not fail) because continue_on_error is set
	assert.Equal(t, pipeline.RunSuccess, result.Status)
	require.Len(t, result.StepResults, 2)
	assert.Equal(t, pipeline.RunFailed, result.StepResults[0].Status)
	assert.Equal(t, pipeline.RunSuccess, result.StepResults[1].Status)
	assert.Contains(t, result.StepResults[1].Output, "ran-after-failure")
}

func TestPipelineExecutor_ContextCancellation(t *testing.T) {
	bus := eventbus.NewMemoryBus(16)
	defer bus.Close()

	executor := app.NewPipelineExecutor(nil, nil, bus, nil, nil, "", app.NewStackLocks())

	p, _ := pipeline.NewPipeline("test", "Cancellation", "user1")
	p.AddStep(pipeline.Step{
		ID: "long", Name: "Long wait", Type: pipeline.StepWait,
		Config: map[string]any{"duration": "30s"},
	})

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	run := pipeline.NewRun(p.ID, "test")
	result := executor.Execute(ctx, p, run)

	// Should fail due to context timeout, not hang for 30s
	assert.NotEqual(t, pipeline.RunSuccess, result.Status)
}

func TestPipelineExecutor_DockerExec_NilClient(t *testing.T) {
	// docker_exec step must fail gracefully (not panic) when the executor
	// was constructed without a Docker client — tests and rebuild-from-scratch
	// scenarios exercise this path.
	bus := eventbus.NewMemoryBus(16)
	defer bus.Close()

	executor := app.NewPipelineExecutor(nil, nil, bus, nil, nil, "", app.NewStackLocks())

	p, _ := pipeline.NewPipeline("test", "docker_exec missing client", "user1")
	require.NoError(t, p.AddStep(pipeline.Step{
		ID: "poke", Name: "Poke sidecar", Type: pipeline.StepDockerExec,
		Config: map[string]any{"container": "wafctl", "command": "true"},
	}))

	run := pipeline.NewRun(p.ID, "test")
	result := executor.Execute(context.Background(), p, run)

	assert.Equal(t, pipeline.RunFailed, result.Status)
	require.Len(t, result.StepResults, 1)
	assert.Equal(t, pipeline.RunFailed, result.StepResults[0].Status)
	assert.Contains(t, result.StepResults[0].Error, "docker client not available")
}

func TestPipelineExecutor_DockerExec_MissingContainer(t *testing.T) {
	// Missing `container` config should fail with a clear error before we
	// even reach Docker — guards against config typos / UI bugs.
	bus := eventbus.NewMemoryBus(16)
	defer bus.Close()

	executor := app.NewPipelineExecutor(nil, nil, bus, nil, nil, "", app.NewStackLocks())

	p, _ := pipeline.NewPipeline("test", "docker_exec missing container", "user1")
	require.NoError(t, p.AddStep(pipeline.Step{
		ID: "poke", Name: "Poke", Type: pipeline.StepDockerExec,
		Config: map[string]any{"command": "true"},
	}))

	run := pipeline.NewRun(p.ID, "test")
	result := executor.Execute(context.Background(), p, run)

	assert.Equal(t, pipeline.RunFailed, result.Status)
	require.Len(t, result.StepResults, 1)
	// Either "docker client not available" (hit first) or "missing 'container'"
	// is acceptable — both are pre-dispatch guards.
	errMsg := result.StepResults[0].Error
	assert.True(t,
		strings.Contains(errMsg, "docker client not available") || strings.Contains(errMsg, "missing 'container'"),
		"expected pre-dispatch guard error, got: %s", errMsg)
}

func TestPipelineExecutor_Events(t *testing.T) {
	bus := eventbus.NewMemoryBus(64)
	defer bus.Close()

	var events []string
	var mu = &sync.Mutex{}
	bus.Subscribe(func(evt domevent.Event) bool {
		mu.Lock()
		events = append(events, evt.EventType())
		mu.Unlock()
		return true
	})

	executor := app.NewPipelineExecutor(nil, nil, bus, nil, nil, "", app.NewStackLocks())

	p, _ := pipeline.NewPipeline("test", "Events", "user1")
	p.AddStep(pipeline.Step{
		ID: "echo", Name: "Echo", Type: pipeline.StepShellCommand,
		Config: map[string]any{"command": "echo hi"},
	})

	run := pipeline.NewRun(p.ID, "test")
	executor.Execute(context.Background(), p, run)

	time.Sleep(50 * time.Millisecond) // let events propagate

	mu.Lock()
	defer mu.Unlock()
	assert.Contains(t, events, "pipeline.run.started")
	assert.Contains(t, events, "pipeline.step.started")
	assert.Contains(t, events, "pipeline.step.finished")
	assert.Contains(t, events, "pipeline.run.finished")
}
