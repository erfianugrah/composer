package pipeline

import (
	"errors"
	"fmt"
	"time"
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
	StepHTTPRequest    StepType = "http_request"
	StepWait           StepType = "wait"
	StepNotify         StepType = "notify"
)

func (t StepType) Valid() bool {
	switch t {
	case StepComposeUp, StepComposeDown, StepComposePull, StepComposeRestart,
		StepShellCommand, StepHTTPRequest, StepWait, StepNotify:
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
		ID:          fmt.Sprintf("pl_%x", now.UnixNano()),
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

	// Check for cycles using DFS
	visited := make(map[string]int) // 0=unvisited, 1=visiting, 2=done
	var hasCycle func(id string) bool
	depMap := make(map[string][]string)
	for _, s := range p.Steps {
		depMap[s.ID] = s.DependsOn
	}

	hasCycle = func(id string) bool {
		if visited[id] == 1 {
			return true // cycle
		}
		if visited[id] == 2 {
			return false
		}
		visited[id] = 1
		for _, dep := range depMap[id] {
			if hasCycle(dep) {
				return true
			}
		}
		visited[id] = 2
		return false
	}

	for _, s := range p.Steps {
		if hasCycle(s.ID) {
			return fmt.Errorf("cycle detected involving step %q", s.ID)
		}
	}

	return nil
}

// ExecutionOrder returns steps in topological order (respecting dependencies).
// Steps with no deps come first. Steps with same depth can run concurrently.
func (p *Pipeline) ExecutionOrder() [][]Step {
	depMap := make(map[string][]string)
	stepMap := make(map[string]Step)
	inDegree := make(map[string]int)

	for _, s := range p.Steps {
		stepMap[s.ID] = s
		depMap[s.ID] = s.DependsOn
		inDegree[s.ID] = len(s.DependsOn)
	}

	var result [][]Step

	for len(stepMap) > 0 {
		// Find all steps with zero in-degree (ready to run)
		var batch []Step
		for id, deg := range inDegree {
			if deg == 0 {
				if s, ok := stepMap[id]; ok {
					batch = append(batch, s)
				}
			}
		}

		if len(batch) == 0 {
			break // shouldn't happen if Validate() passes
		}

		result = append(result, batch)

		// Remove batch from graph
		for _, s := range batch {
			delete(stepMap, s.ID)
			delete(inDegree, s.ID)
			// Reduce in-degree of dependents
			for id, deps := range depMap {
				for _, dep := range deps {
					if dep == s.ID {
						inDegree[id]--
					}
				}
			}
		}
	}

	return result
}
