package pipeline_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/erfianugrah/composer/internal/domain/pipeline"
)

func TestNewPipeline(t *testing.T) {
	p, err := pipeline.NewPipeline("deploy-web", "Deploy the web stack", "user1")
	require.NoError(t, err)
	assert.NotEmpty(t, p.ID)
	assert.Equal(t, "deploy-web", p.Name)
	assert.Equal(t, "Deploy the web stack", p.Description)
	assert.Equal(t, "user1", p.CreatedBy)
	assert.Empty(t, p.Steps)
	assert.Empty(t, p.Triggers)
}

func TestNewPipeline_Validation(t *testing.T) {
	_, err := pipeline.NewPipeline("", "desc", "user1")
	assert.Error(t, err)

	_, err = pipeline.NewPipeline("name", "desc", "")
	assert.Error(t, err)
}

func TestPipeline_AddStep(t *testing.T) {
	p, _ := pipeline.NewPipeline("test", "", "user1")

	err := p.AddStep(pipeline.Step{
		ID: "pull", Name: "Pull images", Type: pipeline.StepComposePull,
		Config: map[string]any{"stack": "web"},
	})
	require.NoError(t, err)
	assert.Len(t, p.Steps, 1)

	err = p.AddStep(pipeline.Step{
		ID: "deploy", Name: "Deploy", Type: pipeline.StepComposeUp,
		DependsOn: []string{"pull"},
	})
	require.NoError(t, err)
	assert.Len(t, p.Steps, 2)
}

func TestPipeline_AddStep_Validation(t *testing.T) {
	p, _ := pipeline.NewPipeline("test", "", "user1")

	// Missing ID
	err := p.AddStep(pipeline.Step{Name: "x", Type: pipeline.StepWait})
	assert.Error(t, err)

	// Missing name
	err = p.AddStep(pipeline.Step{ID: "x", Type: pipeline.StepWait})
	assert.Error(t, err)

	// Invalid type
	err = p.AddStep(pipeline.Step{ID: "x", Name: "x", Type: "invalid"})
	assert.Error(t, err)

	// Unknown dependency
	err = p.AddStep(pipeline.Step{
		ID: "x", Name: "x", Type: pipeline.StepWait,
		DependsOn: []string{"nonexistent"},
	})
	assert.Error(t, err)
}

func TestPipeline_Validate_Empty(t *testing.T) {
	p, _ := pipeline.NewPipeline("test", "", "user1")
	assert.Error(t, p.Validate())
}

func TestPipeline_Validate_DuplicateIDs(t *testing.T) {
	p, _ := pipeline.NewPipeline("test", "", "user1")
	p.Steps = []pipeline.Step{
		{ID: "a", Name: "A", Type: pipeline.StepWait},
		{ID: "a", Name: "B", Type: pipeline.StepWait},
	}
	assert.Error(t, p.Validate())
}

func TestPipeline_Validate_Cycle(t *testing.T) {
	p, _ := pipeline.NewPipeline("test", "", "user1")
	p.Steps = []pipeline.Step{
		{ID: "a", Name: "A", Type: pipeline.StepWait, DependsOn: []string{"b"}},
		{ID: "b", Name: "B", Type: pipeline.StepWait, DependsOn: []string{"a"}},
	}
	assert.Error(t, p.Validate())
}

func TestPipeline_Validate_Valid(t *testing.T) {
	p, _ := pipeline.NewPipeline("test", "", "user1")
	p.AddStep(pipeline.Step{ID: "pull", Name: "Pull", Type: pipeline.StepComposePull})
	p.AddStep(pipeline.Step{ID: "deploy", Name: "Deploy", Type: pipeline.StepComposeUp, DependsOn: []string{"pull"}})
	p.AddStep(pipeline.Step{ID: "health", Name: "Health", Type: pipeline.StepHTTPRequest, DependsOn: []string{"deploy"}})

	assert.NoError(t, p.Validate())
}

func TestPipeline_ExecutionOrder_Linear(t *testing.T) {
	p, _ := pipeline.NewPipeline("test", "", "user1")
	p.AddStep(pipeline.Step{ID: "a", Name: "A", Type: pipeline.StepWait})
	p.AddStep(pipeline.Step{ID: "b", Name: "B", Type: pipeline.StepWait, DependsOn: []string{"a"}})
	p.AddStep(pipeline.Step{ID: "c", Name: "C", Type: pipeline.StepWait, DependsOn: []string{"b"}})

	order := p.ExecutionOrder()
	require.Len(t, order, 3)
	assert.Len(t, order[0], 1) // a
	assert.Len(t, order[1], 1) // b
	assert.Len(t, order[2], 1) // c
	assert.Equal(t, "a", order[0][0].ID)
	assert.Equal(t, "b", order[1][0].ID)
	assert.Equal(t, "c", order[2][0].ID)
}

func TestPipeline_ExecutionOrder_Parallel(t *testing.T) {
	p, _ := pipeline.NewPipeline("test", "", "user1")
	p.AddStep(pipeline.Step{ID: "pull-web", Name: "Pull Web", Type: pipeline.StepComposePull})
	p.AddStep(pipeline.Step{ID: "pull-api", Name: "Pull API", Type: pipeline.StepComposePull})
	p.AddStep(pipeline.Step{ID: "deploy", Name: "Deploy", Type: pipeline.StepComposeUp,
		DependsOn: []string{"pull-web", "pull-api"}})

	order := p.ExecutionOrder()
	require.Len(t, order, 2)
	assert.Len(t, order[0], 2) // pull-web + pull-api run concurrently
	assert.Len(t, order[1], 1) // deploy waits for both
}

func TestPipeline_ExecutionOrder_Diamond(t *testing.T) {
	// Diamond pattern:
	//     A
	//    / \
	//   B   C
	//    \ /
	//     D
	p, _ := pipeline.NewPipeline("test", "", "user1")
	p.AddStep(pipeline.Step{ID: "a", Name: "A", Type: pipeline.StepWait})
	p.AddStep(pipeline.Step{ID: "b", Name: "B", Type: pipeline.StepWait, DependsOn: []string{"a"}})
	p.AddStep(pipeline.Step{ID: "c", Name: "C", Type: pipeline.StepWait, DependsOn: []string{"a"}})
	p.AddStep(pipeline.Step{ID: "d", Name: "D", Type: pipeline.StepWait, DependsOn: []string{"b", "c"}})

	order := p.ExecutionOrder()
	require.Len(t, order, 3)
	assert.Len(t, order[0], 1) // a
	assert.Len(t, order[1], 2) // b + c (parallel)
	assert.Len(t, order[2], 1) // d
}

func TestStepType_Valid(t *testing.T) {
	assert.True(t, pipeline.StepComposeUp.Valid())
	assert.True(t, pipeline.StepShellCommand.Valid())
	assert.True(t, pipeline.StepHTTPRequest.Valid())
	assert.True(t, pipeline.StepNotify.Valid())
	assert.False(t, pipeline.StepType("invalid").Valid())
	assert.False(t, pipeline.StepType("").Valid())
}

func TestRun_Lifecycle(t *testing.T) {
	run := pipeline.NewRun("pl_123", "user1")
	assert.Equal(t, pipeline.RunPending, run.Status)
	assert.Nil(t, run.StartedAt)
	assert.Nil(t, run.FinishedAt)

	run.Start()
	assert.Equal(t, pipeline.RunRunning, run.Status)
	assert.NotNil(t, run.StartedAt)

	run.RecordStepResult(pipeline.StepResult{
		StepID: "pull", StepName: "Pull", Status: pipeline.RunSuccess,
		Duration: 2 * time.Second,
	})
	assert.Len(t, run.StepResults, 1)
	assert.Equal(t, pipeline.RunRunning, run.Status) // still running

	run.Complete()
	assert.Equal(t, pipeline.RunSuccess, run.Status)
	assert.NotNil(t, run.FinishedAt)
}

func TestRun_Failure(t *testing.T) {
	run := pipeline.NewRun("pl_123", "user1")
	run.Start()

	run.RecordStepResult(pipeline.StepResult{
		StepID: "deploy", StepName: "Deploy", Status: pipeline.RunFailed,
		Error: "container crashed",
	})

	assert.Equal(t, pipeline.RunFailed, run.Status)
	assert.NotNil(t, run.FinishedAt)
}

func TestRun_Cancel(t *testing.T) {
	run := pipeline.NewRun("pl_123", "user1")
	run.Start()
	run.Cancel()

	assert.Equal(t, pipeline.RunCancelled, run.Status)
	assert.NotNil(t, run.FinishedAt)
}
