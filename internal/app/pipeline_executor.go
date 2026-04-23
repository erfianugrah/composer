package app

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	domevent "github.com/erfianugrah/composer/internal/domain/event"
	"github.com/erfianugrah/composer/internal/domain/pipeline"
	"github.com/erfianugrah/composer/internal/domain/stack"
	"github.com/erfianugrah/composer/internal/infra/docker"
	"github.com/erfianugrah/composer/internal/infra/sops"
)

// StepExecutor defines how to execute a single pipeline step.
type StepExecutor func(ctx context.Context, step pipeline.Step) (output string, err error)

// PipelineExecutor runs pipeline steps in DAG order with concurrency.
type PipelineExecutor struct {
	compose   *docker.Compose
	docker    *docker.Client            // required for docker_exec steps; may be nil in tests
	bus       domevent.Bus
	stacks    stack.StackRepository     // resolve stack name → path
	gitCfgs   stack.GitConfigRepository // per-stack SOPS age key
	stacksDir string                    // global age key fallback
	locks     *StackLocks               // shared with StackService — prevents concurrent compose ops
}

// NewPipelineExecutor constructs the executor.
//
// dockerClient is optional — pass nil in tests that don't exercise docker_exec
// steps. When a pipeline tries to run a docker_exec step without a wired
// client, the step fails with a clear error instead of panicking.
func NewPipelineExecutor(
	compose *docker.Compose,
	dockerClient *docker.Client,
	bus domevent.Bus,
	stacks stack.StackRepository,
	gitCfgs stack.GitConfigRepository,
	stacksDir string,
	locks *StackLocks,
) *PipelineExecutor {
	return &PipelineExecutor{
		compose:   compose,
		docker:    dockerClient,
		bus:       bus,
		stacks:    stacks,
		gitCfgs:   gitCfgs,
		stacksDir: stacksDir,
		locks:     locks,
	}
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

		// Record results -- track whether any non-continuable failure occurred
		hasHardFailure := false
		for _, result := range results {
			run.RecordStepResult(result)

			if result.Status == pipeline.RunFailed {
				continuable := false
				for _, s := range batch {
					if s.ID == result.StepID && s.ContinueOnError {
						continuable = true
						break
					}
				}
				if !continuable {
					hasHardFailure = true
				}
			}
		}

		// Only resume running if ALL failures were continuable
		if !hasHardFailure && run.Status == pipeline.RunFailed {
			run.Status = pipeline.RunRunning
			run.FinishedAt = nil // reset premature FinishedAt
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
		return e.executeComposeStep(ctx, step, "up")
	case pipeline.StepComposeDown:
		return e.executeComposeStep(ctx, step, "down")
	case pipeline.StepComposePull:
		return e.executeComposeStep(ctx, step, "pull")
	case pipeline.StepComposeRestart:
		return e.executeComposeStep(ctx, step, "restart")

	case pipeline.StepShellCommand:
		command, _ := step.Config["command"].(string)
		if command == "" {
			return "", fmt.Errorf("shell_command: missing command config")
		}
		// WARNING: Executes arbitrary host commands. Pipeline creation requires admin role.
		// Environment is scrubbed to prevent leaking secrets (DB URLs, API tokens, etc.).
		cmd := exec.CommandContext(ctx, "sh", "-c", command)
		cmd.Env = []string{
			"PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin",
			"HOME=/tmp",
			"HISTFILE=/dev/null",
			"TERM=xterm",
		}
		// Cap combined output at docker.ExecMaxOutput (1 MB) per stream so a
		// runaway command can't balloon composer memory. Matches the
		// docker_exec step's bound introduced in v0.8.1.
		stdout := &docker.CappedBuffer{Limit: docker.ExecMaxOutput}
		stderr := &docker.CappedBuffer{Limit: docker.ExecMaxOutput}
		cmd.Stdout = stdout
		cmd.Stderr = stderr
		err := cmd.Run()
		out := stdout.String() + stderr.String()
		if stdout.Truncated || stderr.Truncated {
			out += fmt.Sprintf("\n[output truncated at %d bytes/stream]", docker.ExecMaxOutput)
		}
		return out, err

	case pipeline.StepDockerExec:
		return e.executeDockerExec(ctx, step)

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
		urlStr, _ := step.Config["url"].(string)
		if urlStr == "" {
			return "", fmt.Errorf("http_request: missing url config")
		}
		// Validate URL scheme (only http/https -- block file://, gopher://, etc.)
		if !strings.HasPrefix(urlStr, "http://") && !strings.HasPrefix(urlStr, "https://") {
			return "", fmt.Errorf("http_request: only http:// and https:// URLs are allowed")
		}
		// SSRF protection: block private/link-local IPs unless explicitly allowed
		if err := validateHTTPTarget(urlStr); err != nil {
			return "", fmt.Errorf("http_request: %w", err)
		}
		// Use Go's http client instead of shelling out to curl (no SSRF via exotic protocols)
		client := &http.Client{Timeout: 30 * time.Second}
		req, err := http.NewRequestWithContext(ctx, "GET", urlStr, nil)
		if err != nil {
			return "", fmt.Errorf("http_request: %w", err)
		}
		resp, err := client.Do(req)
		if err != nil {
			return "", fmt.Errorf("http_request: %w", err)
		}
		defer resp.Body.Close()
		return fmt.Sprintf("%d", resp.StatusCode), nil

	case pipeline.StepNotify:
		target, _ := step.Config["target"].(string)
		return fmt.Sprintf("notification sent to %s", target), nil

	default:
		return "", fmt.Errorf("unknown step type %q", step.Type)
	}
}

// executeComposeStep resolves the stack path, handles SOPS decrypt/re-encrypt,
// and runs the compose operation. Fixes the prior bug where stack name was
// passed directly as filesystem path.
func (e *PipelineExecutor) executeComposeStep(ctx context.Context, step pipeline.Step, op string) (string, error) {
	stackName, _ := step.Config["stack"].(string)
	if stackName == "" {
		return "", fmt.Errorf("compose_%s: missing stack config", op)
	}

	e.locks.Lock(stackName)
	defer e.locks.Unlock(stackName)

	// Resolve stack name → filesystem path
	var stackPath, composePath string
	if e.stacks != nil {
		st, err := e.stacks.GetByName(ctx, stackName)
		if err != nil {
			return "", fmt.Errorf("stack %q not found: %w", stackName, err)
		}
		stackPath = st.Path

		// SOPS decrypt if available
		if sops.IsAvailable() && e.gitCfgs != nil {
			cfg, _ := e.gitCfgs.GetByStackName(ctx, stackName)
			if cfg != nil {
				composePath = filepath.Join(st.Path, cfg.ComposePath)
				var perStackAgeKey string
				if cfg.Credentials != nil {
					perStackAgeKey = cfg.Credentials.AgeKey
				}
				ageKey := sops.ResolveAgeKey(perStackAgeKey, e.stacksDir)
				sops.DecryptEnvFile(st.Path, ageKey)
				sops.DecryptComposeSecrets(composePath, ageKey)
				defer func() {
					sops.ReEncryptEnvFile(st.Path)
					sops.ReEncryptComposeSecrets(composePath)
				}()
			}
		}
	} else {
		// Fallback: use stack name as path (legacy/test behavior)
		stackPath = stackName
	}

	var result *docker.ComposeResult
	var err error
	switch op {
	case "up":
		result, err = e.compose.Up(ctx, stackPath, composePath)
	case "down":
		result, err = e.compose.Down(ctx, stackPath, composePath, false)
	case "pull":
		result, err = e.compose.Pull(ctx, stackPath, composePath)
	case "restart":
		result, err = e.compose.Restart(ctx, stackPath, composePath)
	default:
		return "", fmt.Errorf("unknown compose op %q", op)
	}
	return composeOutput(result, err)
}

// executeDockerExec runs a command inside an existing container and returns
// its stdout (and stderr folded in on non-zero exit). Intended for post-deploy
// hooks that poke sidecar containers' admin APIs — wafctl reload, caddy
// admin API load, etc.
//
// Admin-only at the API layer: the pipeline Create handler is admin-only
// blanket, and the Update handler enforces admin for steps of this type
// (see internal/api/handler/pipeline.go).
//
// Config shape (one of):
//
//	{"container": "wafctl", "cmd": ["wget", "-qO-", "http://localhost/deploy"]}
//	{"container": "wafctl", "command": "wget -qO- http://localhost/deploy"}
//
// `cmd` is preferred — it's already tokenised so quoted arguments survive.
// `command` is wrapped in `sh -c` for shell-operator convenience; requires
// the container to actually have a shell.
func (e *PipelineExecutor) executeDockerExec(ctx context.Context, step pipeline.Step) (string, error) {
	if e.docker == nil {
		return "", fmt.Errorf("docker_exec: docker client not available")
	}

	containerName, _ := step.Config["container"].(string)
	if containerName == "" {
		return "", fmt.Errorf("docker_exec: missing 'container' config")
	}

	argv, err := dockerExecArgv(step.Config)
	if err != nil {
		return "", err
	}

	result, err := e.docker.ExecRun(ctx, containerName, argv)
	if err != nil {
		return "", fmt.Errorf("docker_exec: %w", err)
	}

	output := result.Stdout
	if result.ExitCode != 0 {
		// Combine for visibility; stderr often has the useful diagnostic.
		combined := strings.TrimRight(output+result.Stderr, "\n")
		trunc := ""
		if result.Truncated {
			trunc = " (output truncated at " + fmt.Sprintf("%d", docker.ExecMaxOutput) + " bytes/stream)"
		}
		return combined, fmt.Errorf("docker_exec: %q exited %d%s", containerName, result.ExitCode, trunc)
	}
	return output, nil
}

// dockerExecArgv extracts the argv for a docker_exec step. Prefers `cmd`
// (slice form, quote-safe); falls back to `command` (string wrapped in
// `sh -c`). Returns an error if neither is present/valid.
func dockerExecArgv(config map[string]any) ([]string, error) {
	if raw, ok := config["cmd"].([]any); ok && len(raw) > 0 {
		argv := make([]string, 0, len(raw))
		for _, a := range raw {
			s, ok := a.(string)
			if !ok {
				return nil, fmt.Errorf("docker_exec: 'cmd' entries must be strings")
			}
			argv = append(argv, s)
		}
		return argv, nil
	}
	// Accept []string directly too (repos that round-trip via Go structs)
	if argv, ok := config["cmd"].([]string); ok && len(argv) > 0 {
		return argv, nil
	}
	if commandStr, _ := config["command"].(string); commandStr != "" {
		return []string{"sh", "-c", commandStr}, nil
	}
	return nil, fmt.Errorf("docker_exec: need either 'cmd' []string or 'command' string")
}

// composeOutput safely extracts output from a compose result, guarding against nil.
func composeOutput(result *docker.ComposeResult, err error) (string, error) {
	if err != nil {
		if result != nil {
			return result.Stderr, err
		}
		return "", err
	}
	if result != nil {
		return result.Stdout, nil
	}
	return "", nil
}

// validateHTTPTarget blocks requests to private/link-local/loopback IPs
// to prevent SSRF attacks (e.g., cloud metadata at 169.254.169.254).
// Set COMPOSER_PIPELINE_ALLOW_PRIVATE_IPS=true to disable this check.
func validateHTTPTarget(rawURL string) error {
	if os.Getenv("COMPOSER_PIPELINE_ALLOW_PRIVATE_IPS") == "true" {
		return nil
	}

	u, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}

	host := u.Hostname()
	ips, err := net.LookupHost(host)
	if err != nil {
		return fmt.Errorf("DNS lookup failed for %s: %w", host, err)
	}

	for _, ipStr := range ips {
		ip := net.ParseIP(ipStr)
		if ip == nil {
			continue
		}
		if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
			return fmt.Errorf("requests to private/internal IP %s (%s) are blocked; set COMPOSER_PIPELINE_ALLOW_PRIVATE_IPS=true to override", host, ipStr)
		}
	}
	return nil
}

func (e *PipelineExecutor) publishEvent(evt domevent.Event) {
	if e.bus != nil {
		e.bus.Publish(evt)
	}
}
