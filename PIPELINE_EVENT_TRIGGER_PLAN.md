# Event-Triggered Pipelines + `docker_exec` Step Type

## Status

- [x] Section 1: `docker_exec` step type (shipped in v0.8.1)
- [x] Section 2: `event` trigger type + `PipelineService` event bus subscription (shipped in v0.8.1)
- [ ] Section 3 (optional): `http_request` method/body support — deferred, not needed for caddy-compose reload
- [x] Tests (domain `Valid()`, `dockerExecArgv`, nil-client guard, missing-container guard, event trigger matching / mismatch / empty-stack / ignore-non-stack-events)
- [x] Docs updates (`api-reference.md`, DTO enum expanded, PIPELINE_EVENT_TRIGGER_PLAN.md status)

## Deviations from the original plan

- **Output cap on `ExecRun`** (my review flagged this as a blocker). Added
  `ExecMaxOutput = 1 MB` per stream via a `cappedBuffer` writer. Runaway exec
  output is truncated with `result.Truncated = true` and the rest drained to
  prevent the container blocking on write.
- **DTO enum updates** (also flagged as a blocker since Batch 5 locked the
  enums). `PipelineStepDTO.Type` now includes `docker_exec`; `TriggerDTO.Type`
  now includes `event`. `make generate` regenerated `web/src/lib/api/types.ts`
  so frontend `openapi-fetch` users get the new values.
- **Admin gate clarified**: the existing `handler/pipeline.go:280` check on
  `shell_command`/`docker_exec` fires only on **Update** (operator+ base role,
  admin escalation for those step types). **Create** is blanket admin-only at
  `pipeline.go:124`, so the gate is never bypassed. Both paths are covered
  — no new admin check needed.
- **Test file structure**: argv parser tests live in
  `internal/app/pipeline_docker_exec_test.go` (same package, so `dockerExecArgv`
  is reachable). Higher-level nil-client / missing-container guard tests
  live in `pipeline_executor_test.go` under the `app_test` package.

## Problem

Real-world scenario: the `caddy-compose` stack bind-mounts its `Caddyfile` from
`/mnt/user/composer/stacks/caddy/Caddyfile` (git-managed). When a `git push`
lands, the webhook → `SyncAndRedeploy` pipeline pulls the new `Caddyfile` into
place, but:

1. `docker compose up -d` is a **no-op** — bind-mount file changes do not
   trigger container recreation (compose only reacts to compose.yaml / image /
   env var / volume definition changes).
2. Caddy keeps serving the old config in memory. Nothing reloads it.

The project's `Makefile` patches this manually (`make caddy-reload`) by running
three `docker exec wafctl wget POST /api/...` calls after sync. These hit
wafctl's loopback-only admin API, which regenerates `policy-rules.json` and
then calls Caddy's admin API (`POST :2020/load`) to hot-reload the config.

We want this to happen automatically on every successful deploy of the caddy
stack — no manual `make` invocation.

## Root Cause

Composer's pipeline subsystem has the right shape for this (webhook trigger,
step DAG, async execution) but four concrete gaps block the use case:

1. **Race on webhook dispatch.** `internal/api/handler/webhook.go:111-132`
   fires `SyncAndRedeploy` and `RunByWebhookTrigger` as two parallel
   goroutines with no ordering guarantee. A pipeline can start before the
   sync pulls the new `Caddyfile`.

2. **No post-deploy trigger type.** `internal/domain/pipeline/aggregate.go:64-68`
   only defines `manual`, `webhook`, `schedule`. There is no
   `TriggerEvent`/`TriggerStackDeployed` that would fire *after*
   `SyncAndRedeploy` successfully publishes `StackDeployed` on the event bus.

3. **`docker_exec` step type referenced but unimplemented.** The RBAC gate at
   `internal/api/handler/pipeline.go:278-286` references `"docker_exec"` for
   admin-only enforcement. But:
   - `StepType.Valid()` in `aggregate.go:47-54` does not accept it.
   - `executeStep` in `pipeline_executor.go:164-240` has no case.
   - `docs/security.md:128` documents "shell/docker steps" as if shipped.

   Result: it is impossible to create a `docker_exec` step today — the
   `Create` handler rejects it with `invalid step type`.

4. **`http_request` is GET-only.** `pipeline_executor.go:222-223` hardcodes
   `http.NewRequestWithContext(ctx, "GET", urlStr, nil)`. wafctl's deploy
   endpoints require POST (even with empty body). This is only relevant if
   we choose to reach wafctl via its HTTP API instead of `docker exec`.

## Solution — Three Changes

### 1. `docker_exec` step type

Implement the step type the admin gate already anticipates. Uses the existing
Docker SDK client (composer already has the socket mounted and the `docker.Client`
wired in via `NewClient` in `internal/infra/docker/client.go:34`).

#### Files to change

**`internal/infra/docker/terminal.go`** — add non-interactive exec helper next
to the existing `ExecAttach`:

```go
// ExecResult is the outcome of a non-interactive container exec.
type ExecResult struct {
    ExitCode int
    Stdout   string
    Stderr   string
}

// ExecRun runs a command inside a container non-interactively and returns
// captured stdout/stderr plus the exit code. Uses stdcopy to demultiplex
// the Docker API's multiplexed stream.
func (c *Client) ExecRun(ctx context.Context, containerID string, cmd []string) (*ExecResult, error) {
    exec, err := c.cli.ContainerExecCreate(ctx, containerID, container.ExecOptions{
        Cmd:          cmd,
        AttachStdout: true,
        AttachStderr: true,
        Tty:          false,
    })
    if err != nil {
        return nil, fmt.Errorf("creating exec: %w", err)
    }

    attach, err := c.cli.ContainerExecAttach(ctx, exec.ID, container.ExecStartOptions{Tty: false})
    if err != nil {
        return nil, fmt.Errorf("attaching to exec: %w", err)
    }
    defer attach.Close()

    // Demultiplex stdout/stderr from the Docker stream
    var stdoutBuf, stderrBuf bytes.Buffer
    done := make(chan error, 1)
    go func() {
        _, copyErr := stdcopy.StdCopy(&stdoutBuf, &stderrBuf, attach.Reader)
        done <- copyErr
    }()

    select {
    case err := <-done:
        if err != nil {
            return nil, fmt.Errorf("reading exec output: %w", err)
        }
    case <-ctx.Done():
        return nil, ctx.Err()
    }

    inspect, err := c.cli.ContainerExecInspect(ctx, exec.ID)
    if err != nil {
        return nil, fmt.Errorf("inspecting exec: %w", err)
    }

    return &ExecResult{
        ExitCode: inspect.ExitCode,
        Stdout:   stdoutBuf.String(),
        Stderr:   stderrBuf.String(),
    }, nil
}
```

Imports needed: `bytes`, `github.com/docker/docker/pkg/stdcopy` (already in
go.mod transitively — pin explicitly).

**`internal/domain/pipeline/aggregate.go`** — add the step type constant
(`:36-45`) and extend `Valid()` (`:47-54`):

```go
const (
    StepComposeUp      StepType = "compose_up"
    StepComposeDown    StepType = "compose_down"
    StepComposePull    StepType = "compose_pull"
    StepComposeRestart StepType = "compose_restart"
    StepShellCommand   StepType = "shell_command"
    StepDockerExec     StepType = "docker_exec"  // new
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
```

**`internal/app/pipeline_executor.go`** — wire the Docker client into the
executor (struct at `:27-34`, constructor at `:36-52`):

```go
type PipelineExecutor struct {
    compose   *docker.Compose
    docker    *docker.Client        // new — for docker_exec steps
    bus       domevent.Bus
    stacks    stack.StackRepository
    gitCfgs   stack.GitConfigRepository
    stacksDir string
    locks     *StackLocks
}

func NewPipelineExecutor(
    compose *docker.Compose,
    dockerClient *docker.Client,    // new
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
```

Add the case in `executeStep` (`:164-240`) next to `StepShellCommand`:

```go
case pipeline.StepDockerExec:
    return e.executeDockerExec(ctx, step)
```

Add the handler method:

```go
// executeDockerExec runs a command inside an existing container and returns
// its stdout/stderr. Intended for post-deploy hooks that need to poke
// sidecar containers' admin APIs (e.g. wafctl reload, caddy reload).
// Admin-only at the API layer (see handler/pipeline.go).
func (e *PipelineExecutor) executeDockerExec(ctx context.Context, step pipeline.Step) (string, error) {
    if e.docker == nil {
        return "", fmt.Errorf("docker_exec: docker client not available")
    }
    containerName, _ := step.Config["container"].(string)
    if containerName == "" {
        return "", fmt.Errorf("docker_exec: missing container config")
    }

    // Accept either `command` (string, parsed via shlex-style split) or
    // `cmd` ([]string, pre-tokenised). Prefer `cmd` for robustness.
    var argv []string
    if raw, ok := step.Config["cmd"].([]any); ok {
        argv = make([]string, 0, len(raw))
        for _, a := range raw {
            if s, ok := a.(string); ok {
                argv = append(argv, s)
            }
        }
    } else if commandStr, _ := step.Config["command"].(string); commandStr != "" {
        // Wrap in `sh -c` so shell operators (&&, ||, |, redirects) work.
        argv = []string{"sh", "-c", commandStr}
    } else {
        return "", fmt.Errorf("docker_exec: missing cmd or command config")
    }

    result, err := e.docker.ExecRun(ctx, containerName, argv)
    if err != nil {
        return "", fmt.Errorf("docker_exec: %w", err)
    }
    if result.ExitCode != 0 {
        return result.Stdout + result.Stderr, fmt.Errorf("docker_exec: %q exited %d: %s",
            containerName, result.ExitCode, strings.TrimSpace(result.Stderr))
    }
    return result.Stdout, nil
}
```

**`cmd/composerd/main.go`** `:244` — pass `dockerClient` to the new
constructor signature:

```go
// Was:
//   pipelineExecutor = app.NewPipelineExecutor(compose, bus, stackRepo, gitConfigRepo, cfg.StacksDir, stackLocks)
// Now:
pipelineExecutor = app.NewPipelineExecutor(compose, dockerClient, bus, stackRepo, gitConfigRepo, cfg.StacksDir, stackLocks)
```

**Test files** — update 12 call sites of `NewPipelineExecutor` to inject
`nil` for the new docker parameter:

- `internal/app/pipeline_executor_test.go:22,49,83,109,138,169`
- `internal/app/pipeline_service_test.go:117,156,209,242,273,305`

```go
// Was:  NewPipelineExecutor(nil, bus, nil, nil, "", NewStackLocks())
// Now:  NewPipelineExecutor(nil, nil, bus, nil, nil, "", NewStackLocks())
```

#### Behavior

- Admin-only: enforced at `handler/pipeline.go:280` (already in place, no
  change needed — it references `docker_exec` by string literal).
- Frontend `PipelinePage.tsx` passes step types as free-form strings, so no
  frontend enum to update. Phase 2 could add a type-aware create form.
- Non-zero exit code = step failure. Output (stdout + stderr) returned as
  the step result, visible in the pipeline run UI.
- Context cancellation propagates: if the run is cancelled, the exec attach
  read aborts.

### 2. `event` trigger type

Subscribe `PipelineService` to the event bus and fire matching pipelines on
domain events. This replaces the webhook race with a clean post-deploy hook.

#### Files to change

**`internal/domain/pipeline/aggregate.go`** `:64-68` — add the trigger type:

```go
const (
    TriggerManual  TriggerType = "manual"
    TriggerWebhook TriggerType = "webhook"
    TriggerCron    TriggerType = "schedule"
    TriggerEvent   TriggerType = "event"    // new
)
```

No new validation logic needed — `Trigger.Type` is already stored as string
and checked by consumers, not an enum.

**`internal/app/pipeline_service.go`** — add `SubscribeBus` and
`runByEventTrigger`, mirroring the existing `Notifier.Subscribe` pattern
(see `internal/infra/notify/notifier.go:40-50`):

```go
// SubscribeBus registers the pipeline service as an event bus subscriber so
// pipelines with `event` triggers fire in response to domain events.
// Call once at startup after wiring the bus.
func (s *PipelineService) SubscribeBus(bus domevent.Bus) {
    if bus == nil {
        return
    }
    bus.Subscribe(func(evt domevent.Event) bool {
        // Only dispatch on events that carry a stack name. Expand as
        // additional stack-scoped events are added.
        switch e := evt.(type) {
        case domevent.StackDeployed:
            s.runByEventTrigger(evt.EventType(), e.Name)
        case domevent.StackStopped:
            s.runByEventTrigger(evt.EventType(), e.Name)
        case domevent.StackError:
            s.runByEventTrigger(evt.EventType(), e.Name)
        }
        return true
    })
}

// runByEventTrigger finds pipelines with `event` triggers matching the given
// event type and stack name, and runs them async. Run in the bus callback's
// goroutine context — callers must not block; we use context.Background()
// so the dispatch itself does not inherit a cancelled ctx from the publisher.
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
            if triggerStack != "" && triggerStack != stackName {
                continue
            }
            if s.logger != nil {
                s.logger.Info("event triggered pipeline",
                    zap.String("pipeline", p.Name),
                    zap.String("event", eventType),
                    zap.String("stack", stackName))
            }
            if _, err := s.Run(ctx, p.ID, fmt.Sprintf("event:%s:%s", eventType, stackName)); err != nil {
                if s.logger != nil {
                    s.logger.Error("failed to run event-triggered pipeline",
                        zap.String("pipeline", p.Name),
                        zap.Error(err))
                }
            }
        }
    }
}
```

Key design decisions:

- `SubscribeBus` is a separate method (not in constructor) so tests can
  instantiate the service without a bus.
- `runByEventTrigger` uses `context.Background()` — the bus publisher's ctx
  may be short-lived (`SyncAndRedeploy` closes its ctx after Publish), and
  our pipeline runs shouldn't inherit that.
- Filters by `event` type and optional `stack` config, mirroring the
  webhook trigger's `stack + branch` pattern.
- No de-duplication: if a pipeline has both a webhook trigger and an event
  trigger matching the same stack, both fire. Users should pick one.

**`cmd/composerd/main.go`** `:244-247` — subscribe the service after
construction:

```go
if compose != nil {
    pipelineExecutor = app.NewPipelineExecutor(compose, dockerClient, bus, stackRepo, gitConfigRepo, cfg.StacksDir, stackLocks)
    pipelineSvc = app.NewPipelineService(pipelineRepo, runRepo, pipelineExecutor)
    pipelineSvc.SetLogger(logger)
    pipelineSvc.SubscribeBus(bus)   // new
}
```

#### Behavior

- Event triggers fire **after** the publishing service completes its work
  (e.g. `SyncAndRedeploy` only publishes `StackDeployed` after compose up
  returns success). No race with the sync path.
- Stack filter is optional: omit `stack` in config to match all stacks for a
  given event type.
- Dispatch happens in the bus subscriber goroutine — non-blocking; each
  pipeline `Run` further spawns its own goroutine.
- Works for any stack-scoped event. Supported event types on day one:
  `stack.deployed`, `stack.stopped`, `stack.error`. `stack.created`,
  `stack.updated`, `stack.deleted` can be added later (mechanical — just
  extend the type switch).
- Does **not** replace webhook triggers. Webhook triggers fire immediately
  on receipt (pre-sync); event triggers fire on successful outcomes
  (post-sync/post-deploy). Both coexist.

### 3. `http_request` method/body (optional)

Not required for the caddy-compose reload scenario (we use `docker_exec`
instead — wafctl is not on any network composer can reach). Including here
for completeness; skip if scope needs to shrink.

**`internal/app/pipeline_executor.go`** `:208-232` — expand the StepHTTPRequest
case:

```go
case pipeline.StepHTTPRequest:
    urlStr, _ := step.Config["url"].(string)
    if urlStr == "" {
        return "", fmt.Errorf("http_request: missing url config")
    }
    if !strings.HasPrefix(urlStr, "http://") && !strings.HasPrefix(urlStr, "https://") {
        return "", fmt.Errorf("http_request: only http:// and https:// URLs are allowed")
    }
    if err := validateHTTPTarget(urlStr); err != nil {
        return "", fmt.Errorf("http_request: %w", err)
    }

    method, _ := step.Config["method"].(string)
    if method == "" {
        method = "GET"
    }
    method = strings.ToUpper(method)
    switch method {
    case "GET", "POST", "PUT", "PATCH", "DELETE":
    default:
        return "", fmt.Errorf("http_request: unsupported method %q", method)
    }

    var body io.Reader
    if raw, _ := step.Config["body"].(string); raw != "" {
        body = strings.NewReader(raw)
    }

    client := &http.Client{Timeout: 30 * time.Second}
    req, err := http.NewRequestWithContext(ctx, method, urlStr, body)
    if err != nil {
        return "", fmt.Errorf("http_request: %w", err)
    }
    if headers, ok := step.Config["headers"].(map[string]any); ok {
        for k, v := range headers {
            if s, ok := v.(string); ok {
                req.Header.Set(k, s)
            }
        }
    }

    resp, err := client.Do(req)
    if err != nil {
        return "", fmt.Errorf("http_request: %w", err)
    }
    defer resp.Body.Close()

    respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
    if resp.StatusCode >= 400 {
        return string(respBody), fmt.Errorf("http_request: %s returned %d", method, resp.StatusCode)
    }
    return fmt.Sprintf("%d %s", resp.StatusCode, string(respBody)), nil
}
```

## Example: caddy-compose Reload Pipeline

After the three changes, the caddy reload workflow becomes a declarative
pipeline stored in composer's DB:

```json
{
  "name": "reload-caddy",
  "description": "Regenerate WAF + CSP + security headers and reload Caddy after a Caddyfile push.",
  "triggers": [
    {
      "type": "event",
      "config": {
        "event": "stack.deployed",
        "stack": "caddy"
      }
    }
  ],
  "steps": [
    {
      "id": "deploy-waf",
      "name": "Deploy WAF policy",
      "type": "docker_exec",
      "config": {
        "container": "wafctl",
        "command": "wget -qO- -T 120 --post-data='' http://localhost:8080/api/deploy"
      }
    },
    {
      "id": "deploy-csp",
      "name": "Deploy CSP",
      "type": "docker_exec",
      "depends_on": ["deploy-waf"],
      "config": {
        "container": "wafctl",
        "command": "wget -qO- -T 120 --post-data='' http://localhost:8080/api/csp/deploy"
      }
    },
    {
      "id": "deploy-sec-headers",
      "name": "Deploy Security Headers",
      "type": "docker_exec",
      "depends_on": ["deploy-csp"],
      "config": {
        "container": "wafctl",
        "command": "wget -qO- -T 120 --post-data='' http://localhost:8080/api/security-headers/deploy"
      }
    }
  ]
}
```

Flow on `git push`:

1. Gitea fires push webhook → `POST /api/v1/hooks/{id}`
2. Webhook handler validates HMAC, spawns `SyncAndRedeploy` goroutine
3. `SyncAndRedeploy`: git pull (updates Caddyfile on disk) → SOPS decrypt →
   `docker compose pull` → `docker compose up -d` (no-op for caddy stack,
   but harmless) → re-encrypt SOPS → `bus.Publish(StackDeployed{Name: "caddy"})`
4. `PipelineService.SubscribeBus` callback matches `stack.deployed` + stack
   `caddy` → spawns `reload-caddy` pipeline
5. DAG executes three `docker_exec` steps sequentially (each depends on the
   previous): wafctl regenerates `policy-rules.json` + `custom-waf-settings.conf`
   and calls Caddy admin API to hot-reload
6. Caddy is now serving the new `Caddyfile`

Net effect: same as `make caddy-reload`, but triggered automatically by the
push event with no human in the loop.

## Migration Path

1. Ship `docker_exec` step (Section 1) — unblocks the RBAC gate at
   `handler/pipeline.go:280` that has been dead code since inception.
2. Ship `event` trigger (Section 2) — enables post-deploy pipelines for all
   stacks, not just caddy-compose.
3. Caddy-compose maintainer creates the `reload-caddy` pipeline via API
   (`POST /api/v1/pipelines` with the JSON above) or via the UI once it
   supports custom steps/triggers.
4. Update `caddy-compose/Makefile` to remove `caddy-reload` target or keep
   it as an ad-hoc manual reload (uses SSH + `docker exec` directly;
   unchanged).
5. (Optional) Ship `http_request` POST (Section 3) when a use case arises
   where a pipeline needs to call an exposed HTTP endpoint.

## Backward Compatibility

- Existing pipelines with `manual`, `webhook`, `schedule` triggers unchanged.
- Existing step types unchanged. `http_request` default behavior (GET) is
  preserved — method defaults to GET if not set.
- `NewPipelineExecutor` signature change is internal (called only from
  `cmd/composerd/main.go` and tests). No API breakage.
- `PipelineService.SubscribeBus` is additive. Services that don't call it
  (e.g. tests) behave exactly as before — no event triggers fire.
- Frontend (`PipelinePage.tsx`) passes step types as free-form strings;
  backend validation catches invalid types. No UI regression.
- Event triggers can coexist with webhook triggers on the same pipeline.
  Users who want both (e.g. fire on webhook AND on successful deploy)
  simply list both triggers.

## Testing

### Unit

- `pipeline/aggregate_test.go` — `StepDockerExec.Valid()` returns true,
  `TriggerEvent.Type` is persisted/loaded.
- `pipeline_executor_test.go`:
  - `executeDockerExec` returns stdout on exit 0
  - `executeDockerExec` returns error + output on non-zero exit
  - `executeDockerExec` respects context cancellation
  - `executeDockerExec` handles missing `container` / `command` config
  - `executeDockerExec` supports both `command` (string) and `cmd` ([]string) forms
  - `executeDockerExec` returns error when `docker` client is nil
- `pipeline_service_test.go`:
  - `runByEventTrigger` dispatches on matching event+stack
  - `runByEventTrigger` skips mismatched stack
  - `runByEventTrigger` skips mismatched event type
  - `runByEventTrigger` with empty stack filter matches all stacks
  - `SubscribeBus` with nil bus is a no-op

### Integration (`-tags=integration`)

- `pipeline_docker_exec_integration_test.go` — testcontainers spawns a
  busybox container; pipeline runs `docker_exec` against it; asserts
  stdout captured.
- `pipeline_event_trigger_integration_test.go` — publish `StackDeployed`
  on the bus; assert matching pipeline run created in the repo within
  a timeout; assert non-matching pipeline not run.

### Manual / E2E

- Create `reload-caddy` pipeline via `POST /api/v1/pipelines`.
- Configure Gitea webhook pointing at composer.
- Push a Caddyfile change to the caddy-compose repo.
- Verify: Gitea webhook delivery succeeds → `SyncAndRedeploy` job
  completes → `StackDeployed` fires → `reload-caddy` pipeline run
  appears in `/api/v1/pipelines/{id}/runs` → all three steps succeed
  → `curl https://example.test/` reflects the new config.

## Out of Scope

- Frontend UI for creating `docker_exec` steps and `event` triggers. The
  existing `PipelinePage.tsx` create form only offers `compose_up` and
  `shell_command`. Power users can `POST /api/v1/pipelines` with raw JSON
  until a richer form lands. Tracked separately.
- File-watcher trigger (listed as Phase 5 future work in
  `docs/design.md:1730`). Event-driven covers the GitOps case cleanly;
  file-watcher would be additive for non-GitOps edits.
- Generic "post-sync" trigger on non-git-backed stacks. `StackDeployed` is
  published by all deploy paths (`StackService.Deploy` as well as
  `GitService.SyncAndRedeploy`), so the event trigger already covers both.
  Verify `StackService.Deploy` publishes the event (currently does per
  `internal/app/stack_service.go`).
- Pipeline-to-pipeline chaining (pipeline A fires pipeline B on success).
  `PipelineRunFinished` is already on the bus; adding a pipeline-event
  trigger type is mechanical once Section 2 is in.
- Retries / backoff on step failure. Steps fail fast today; adding retry
  policy is orthogonal.

## Future Work

- Subscribe `PipelineService` to `ContainerHealthChanged` so a pipeline can
  react to a container flapping.
- First-class "post-sync" trigger in the webhook flow (fires only if git
  actually pulled new commits, versus on every webhook delivery). Uses the
  `changed` bool already returned by `GitService.Sync`.
- Structured step outputs (JSON) so downstream steps can reference upstream
  results. Currently each step returns a plain string.
- `docker_exec` "service" resolution — accept `service` config instead of
  `container` and resolve via compose project label, so pipelines survive
  container renames.
