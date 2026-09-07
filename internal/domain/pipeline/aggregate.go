package pipeline

import (
	"crypto/rand"
	"errors"
	"fmt"
	"time"

	"github.com/erfianugrah/composer/internal/domain/dag"
)

// Pipeline is the aggregate root for CI-esque deployment workflows.
type Pipeline struct {
	ID          string
	Name        string
	Description string
	Steps       []Step
	Triggers    []Trigger
	CreatedBy   string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// Step defines a single unit of work in a pipeline.
type Step struct {
	ID              string
	Name            string
	Type            StepType
	Config          map[string]any
	Timeout         time.Duration
	ContinueOnError bool
	DependsOn       []string // step IDs
}

// StepType defines what kind of operation a step performs.
type StepType string

const (
	StepComposeUp      StepType = "compose_up"
	StepComposeDown    StepType = "compose_down"
	StepComposePull    StepType = "compose_pull"
	StepComposeRestart StepType = "compose_restart"
	StepShellCommand   StepType = "shell_command"
	StepDockerExec     StepType = "docker_exec"
	StepHTTPRequest    StepType = "http_request"
	StepWait           StepType = "wait"
	StepNotify         StepType = "notify"
)

func (t StepType) Valid() bool {
	switch t {
	case StepComposeUp, StepComposeDown, StepComposePull, StepComposeRestart,
		StepShellCommand, StepDockerExec, StepHTTPRequest, StepWait, StepNotify:
		return true
	}
	return false
}

// Trigger defines what starts a pipeline.
type Trigger struct {
	Type   TriggerType
	Config map[string]any
}

type TriggerType string

const (
	TriggerManual  TriggerType = "manual"
	TriggerWebhook TriggerType = "webhook"
	TriggerCron    TriggerType = "schedule"
	// TriggerEvent fires after a domain event is published on the event bus.
	// Unlike TriggerWebhook (which fires immediately on webhook receipt, in
	// parallel with SyncAndRedeploy), event triggers fire after the
	// publishing operation completes — making them the right choice for
	// post-deploy hooks like "reload caddy after the stack is up".
	//
	// Config shape:
	//   {"event": "stack.deployed", "stack": "caddy"}
	//
	// `event` is matched exactly against domain event types (stack.deployed,
	// stack.stopped, stack.error, …). `stack` is an optional filter — omit
	// to match all stacks for that event type.
	TriggerEvent TriggerType = "event"
)

// NewPipeline creates a new pipeline.
func NewPipeline(name, description, createdBy string) (*Pipeline, error) {
	if name == "" {
		return nil, errors.New("pipeline name is required")
	}
	if createdBy == "" {
		return nil, errors.New("createdBy is required")
	}

	now := time.Now().UTC()
	return &Pipeline{
		ID:          generatePipelineID(now),
		Name:        name,
		Description: description,
		Steps:       []Step{},
		Triggers:    []Trigger{},
		CreatedBy:   createdBy,
		CreatedAt:   now,
		UpdatedAt:   now,
	}, nil
}

// AddStep appends a step to the pipeline.
func (p *Pipeline) AddStep(step Step) error {
	if step.ID == "" {
		return errors.New("step ID is required")
	}
	if step.Name == "" {
		return errors.New("step name is required")
	}
	if !step.Type.Valid() {
		return fmt.Errorf("invalid step type %q", step.Type)
	}

	// Verify depends_on references exist
	stepIDs := make(map[string]bool)
	for _, s := range p.Steps {
		stepIDs[s.ID] = true
	}
	for _, dep := range step.DependsOn {
		if !stepIDs[dep] {
			return fmt.Errorf("step %q depends on unknown step %q", step.ID, dep)
		}
	}

	p.Steps = append(p.Steps, step)
	p.UpdatedAt = time.Now().UTC()
	return nil
}

// Validate checks the pipeline for errors (cycles, missing deps, etc.).
func (p *Pipeline) Validate() error {
	if len(p.Steps) == 0 {
		return errors.New("pipeline has no steps")
	}

	stepIDs := make(map[string]bool)
	for _, s := range p.Steps {
		if stepIDs[s.ID] {
			return fmt.Errorf("duplicate step ID %q", s.ID)
		}
		stepIDs[s.ID] = true
	}

	// Cycle check via the shared DAG orderer (also drives stack batch deploys).
	ids, depMap := p.stepGraph()
	if _, err := dag.Waves(ids, func(id string) []string { return depMap[id] }); err != nil {
		var cycle *dag.CycleError
		if errors.As(err, &cycle) && len(cycle.Nodes) > 0 {
			return fmt.Errorf("cycle detected involving step %q", cycle.Nodes[0])
		}
		return err
	}

	return nil
}

// stepGraph projects the steps to the id list + dependency map dag.Waves wants.
func (p *Pipeline) stepGraph() ([]string, map[string][]string) {
	ids := make([]string, 0, len(p.Steps))
	depMap := make(map[string][]string, len(p.Steps))
	for _, s := range p.Steps {
		ids = append(ids, s.ID)
		depMap[s.ID] = s.DependsOn
	}
	return ids, depMap
}

// ExecutionOrder returns steps in topological order (respecting dependencies).
// Steps with no deps come first. Steps with same depth can run concurrently.
func (p *Pipeline) ExecutionOrder() [][]Step {
	stepMap := make(map[string]Step, len(p.Steps))
	for _, s := range p.Steps {
		stepMap[s.ID] = s
	}
	ids, depMap := p.stepGraph()
	// A cycle cannot reach here once Validate() has passed; if it does, the
	// waves computed before the cycle are returned (the previous behaviour).
	waves, _ := dag.Waves(ids, func(id string) []string { return depMap[id] })

	result := make([][]Step, 0, len(waves))
	for _, wave := range waves {
		batch := make([]Step, 0, len(wave))
		for _, id := range wave {
			batch = append(batch, stepMap[id])
		}
		result = append(result, batch)
	}
	return result
}

// generatePipelineID creates a unique pipeline ID with random component.
func generatePipelineID(now time.Time) string {
	var buf [4]byte
	rand.Read(buf[:])
	return fmt.Sprintf("pl_%x_%x", now.UnixNano(), buf)
}
