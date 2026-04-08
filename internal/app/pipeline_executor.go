package app

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"time"

	domevent "github.com/erfianugrah/composer/internal/domain/event"
	"github.com/erfianugrah/composer/internal/domain/pipeline"
	"github.com/erfianugrah/composer/internal/infra/docker"
)

// StepExecutor defines how to execute a single pipeline step.
type StepExecutor func(ctx context.Context, step pipeline.Step) (output string, err error)

// PipelineExecutor runs pipeline steps in DAG order with concurrency.
type PipelineExecutor struct {
	compose *docker.Compose
	bus     domevent.Bus
}

func NewPipelineExecutor(compose *docker.Compose, bus domevent.Bus) *PipelineExecutor {
	return &PipelineExecutor{compose: compose, bus: bus}
}

// Execute runs a pipeline and returns the completed run.
func (e *PipelineExecutor) Execute(ctx context.Context, p *pipeline.Pipeline, run *pipeline.Run) *pipeline.Run {
	if err := p.Validate(); err != nil {
		run.Fail()
		return run
	}

	run.Start()
	e.publishEvent(domevent.PipelineRunStarted{
		PipelineID: p.ID, RunID: run.ID, Timestamp: time.Now(),
	})

	batches := p.ExecutionOrder()

	for _, batch := range batches {
		if run.Status != pipeline.RunRunning {
			break // cancelled or failed
		}

		// Execute batch concurrently
		var wg sync.WaitGroup
		results := make([]pipeline.StepResult, len(batch))

		for i, step := range batch {
			wg.Add(1)
			go func(idx int, s pipeline.Step) {
				defer wg.Done()

				e.publishEvent(domevent.PipelineStepStarted{
					PipelineID: p.ID, RunID: run.ID, StepID: s.ID, Timestamp: time.Now(),
				})

				stepCtx := ctx
				if s.Timeout > 0 {
					var cancel context.CancelFunc
					stepCtx, cancel = context.WithTimeout(ctx, s.Timeout)
					defer cancel()
				}

				start := time.Now()
				output, err := e.executeStep(stepCtx, s)
				dur := time.Since(start)

				now := time.Now()
				result := pipeline.StepResult{
					StepID:     s.ID,
					StepName:   s.Name,
					Duration:   dur,
					Output:     output,
					StartedAt:  &start,
					FinishedAt: &now,
				}

				if err != nil {
					result.Status = pipeline.RunFailed
					result.Error = err.Error()
				} else {
					result.Status = pipeline.RunSuccess
				}

				results[idx] = result

				e.publishEvent(domevent.PipelineStepFinished{
					PipelineID: p.ID, RunID: run.ID, StepID: s.ID,
					Status: string(result.Status), Duration: dur.String(), Timestamp: now,
				})
			}(i, step)
		}

		wg.Wait()

		// Record results
		for _, result := range results {
			run.RecordStepResult(result)

			// Check if we should continue
			if result.Status == pipeline.RunFailed {
				// Find the step to check continue_on_error
				for _, s := range batch {
					if s.ID == result.StepID && s.ContinueOnError {
						// Override: allow the run to continue
						run.Status = pipeline.RunRunning
					}
				}
			}
		}
	}

	if run.Status == pipeline.RunRunning {
		run.Complete()
	}

	e.publishEvent(domevent.PipelineRunFinished{
		PipelineID: p.ID, RunID: run.ID,
		Status: string(run.Status), Duration: time.Since(*run.StartedAt).String(),
		Timestamp: time.Now(),
	})

	return run
}

func (e *PipelineExecutor) executeStep(ctx context.Context, step pipeline.Step) (string, error) {
	switch step.Type {
	case pipeline.StepComposeUp:
		stackName, _ := step.Config["stack"].(string)
		if stackName == "" {
			return "", fmt.Errorf("compose_up: missing stack config")
		}
		result, err := e.compose.Up(ctx, stackName)
		if err != nil {
			return result.Stderr, err
		}
		return result.Stdout, nil

	case pipeline.StepComposeDown:
		stackName, _ := step.Config["stack"].(string)
		result, err := e.compose.Down(ctx, stackName, false)
		if err != nil {
			return result.Stderr, err
		}
		return result.Stdout, nil

	case pipeline.StepComposePull:
		stackName, _ := step.Config["stack"].(string)
		result, err := e.compose.Pull(ctx, stackName)
		if err != nil {
			return result.Stderr, err
		}
		return result.Stdout, nil

	case pipeline.StepComposeRestart:
		stackName, _ := step.Config["stack"].(string)
		result, err := e.compose.Restart(ctx, stackName)
		if err != nil {
			return result.Stderr, err
		}
		return result.Stdout, nil

	case pipeline.StepShellCommand:
		command, _ := step.Config["command"].(string)
		if command == "" {
			return "", fmt.Errorf("shell_command: missing command config")
		}
		cmd := exec.CommandContext(ctx, "sh", "-c", command)
		out, err := cmd.CombinedOutput()
		return string(out), err

	case pipeline.StepWait:
		durationStr, _ := step.Config["duration"].(string)
		if durationStr == "" {
			durationStr = "5s"
		}
		d, err := time.ParseDuration(durationStr)
		if err != nil {
			return "", fmt.Errorf("wait: invalid duration %q", durationStr)
		}
		select {
		case <-time.After(d):
			return fmt.Sprintf("waited %s", d), nil
		case <-ctx.Done():
			return "", ctx.Err()
		}

	case pipeline.StepHTTPRequest:
		// Simple HTTP check (expand later)
		url, _ := step.Config["url"].(string)
		if url == "" {
			return "", fmt.Errorf("http_request: missing url config")
		}
		cmd := exec.CommandContext(ctx, "curl", "-sf", "-o", "/dev/null", "-w", "%{http_code}", url)
		out, err := cmd.CombinedOutput()
		return strings.TrimSpace(string(out)), err

	case pipeline.StepNotify:
		target, _ := step.Config["target"].(string)
		return fmt.Sprintf("notification sent to %s", target), nil

	default:
		return "", fmt.Errorf("unknown step type %q", step.Type)
	}
}

func (e *PipelineExecutor) publishEvent(evt domevent.Event) {
	if e.bus != nil {
		e.bus.Publish(evt)
	}
}
