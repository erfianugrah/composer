# Deploy Endpoint + Webhook-Triggered Pipelines

## Status

- [x] Section 1: `POST /api/v1/stacks/{name}/deploy` endpoint
- [x] Section 2: Wire webhook triggers to pipeline runs
- [x] Section 3: SOPS support in pipeline compose steps
- [x] Tests

## Problem

When CI (GitHub Actions) builds and pushes a Docker image, Composer needs to pull the new image and redeploy. The current webhook fires on `git push` — before the image is built — so Composer pulls the old image.

**Current workaround**: CI calls three separate endpoints after image push:
```bash
curl -X POST /api/v1/stacks/{name}/sync   # git pull
curl -X POST /api/v1/stacks/{name}/pull   # docker compose pull
curl -X POST /api/v1/stacks/{name}/up     # docker compose up -d
```

This works but is fragile (3 calls, no SOPS decrypt on pull, no atomicity).

## Root Cause

`SyncAndRedeploy` in `git_service.go:163-216` does the full flow (sync → SOPS decrypt → pull → up) but is **only reachable via webhook** (`webhook.go:114`). No authenticated API endpoint exposes it.

## Solution — Three Changes

### 1. `POST /api/v1/stacks/{name}/deploy` endpoint

Expose `SyncAndRedeploy` as an authenticated API endpoint. One call from CI after image build. Handles sync, SOPS decryption, image pull, and deploy atomically.

#### Files to change

**`internal/api/handler/git.go`** — expand struct to include `jobs` dependency:

```go
type GitHandler struct {
    git  *app.GitService
    jobs *app.JobManager  // new — async deploy support
}

func NewGitHandler(git *app.GitService, jobs *app.JobManager) *GitHandler {
    return &GitHandler{git: git, jobs: jobs}
}
```

**`internal/api/handler/git.go`** — add `Deploy` handler:

```go
// Deploy syncs git, pulls images, and redeploys (for CI integration).
func (h *GitHandler) Deploy(ctx context.Context, input *dto.StackNameInput) (*dto.GitDeployOutput, error) {
    if err := authmw.CheckRole(ctx, auth.RoleOperator); err != nil {
        return nil, err
    }
    if input.Async && h.jobs != nil {
        job := h.jobs.Create("deploy", input.Name)
        h.jobs.Start(job.ID)
        go func() {
            opCtx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
            defer cancel()
            action, err := h.git.SyncAndRedeploy(opCtx, input.Name)
            if err != nil {
                h.jobs.Fail(job.ID, err.Error())
                return
            }
            h.jobs.Complete(job.ID, action, "")
        }()
        out := &dto.GitDeployOutput{}
        out.Body.Action = "accepted"
        out.Body.JobID = job.ID
        return out, nil
    }
    // Synchronous (default)
    opCtx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
    defer cancel()
    action, err := h.git.SyncAndRedeploy(opCtx, input.Name)
    if err != nil {
        if errors.Is(err, app.ErrNotFound) {
            return nil, huma.Error404NotFound("stack not found")
        }
        return nil, serverError(err)
    }
    out := &dto.GitDeployOutput{}
    out.Body.Action = action
    return out, nil
}
```

Key differences from original plan:
- Uses `h.git` not `h.gitSvc` (matches existing field name)
- Honors `input.Async` flag (consistent with `?async=true` pattern on other stack ops)
- `jobs.Create()` returns `*Job`, uses `job.ID` for string ID
- Calls `jobs.Start(job.ID)` before goroutine (matches webhook.go pattern)
- `jobs.Complete()` takes 3 args: `(id, output, errOutput)`

**`internal/api/dto/git.go`** — add output DTO:

```go
type GitDeployOutput struct {
    Body struct {
        Action string `json:"action" doc:"Deploy action taken (redeployed, synced_pending_manual, accepted)"`
        JobID  string `json:"job_id,omitempty" doc:"Background job ID (async mode)"`
    }
}
```

Uses inline struct (consistent with existing DTOs like `GitSyncOutput`).

**`internal/api/handler/git.go`** `Register()` — add route:

```go
huma.Register(api, huma.Operation{
    OperationID: "deploySyncStack", Method: http.MethodPost,
    Path:        "/api/v1/stacks/{name}/deploy",
    Summary:     "Sync git, pull images, and redeploy",
    Description: "Full deploy pipeline: git pull → SOPS decrypt → docker compose pull → docker compose up -d. Designed for CI to call after image push. Use ?async=true for background execution.",
    Tags:        []string{"git"},
}, h.Deploy)
```

**`internal/api/server.go`** L147 — update `NewGitHandler` call to pass `Jobs`:

```go
// Was:  handler.NewGitHandler(deps.GitService).Register(api)
// Now:
handler.NewGitHandler(deps.GitService, deps.Jobs).Register(api)
```

#### Behavior

- Requires `Operator` role (same as other stack ops)
- Works only for git-backed stacks (`SyncAndRedeploy` returns error for local stacks)
- Supports `?async=true` for background execution via `JobManager` (same pattern as `/up`, `/pull`, `/build`)
- Default: synchronous — blocks until deploy completes or 10min timeout
- SOPS decrypt/re-encrypt handled by `SyncAndRedeploy`
- Image pull is non-fatal (warns, continues with cached)
- Publishes `StackDeployed` domain event on success

#### CI Usage

```yaml
# GitHub Actions — after docker build+push
- name: Deploy to Composer
  run: |
    curl -sf -X POST "$COMPOSER_URL/api/v1/stacks/docs-ssh/deploy" \
      -H "Authorization: Bearer ${{ secrets.COMPOSER_API_KEY }}" \
      -H "X-Requested-With: XMLHttpRequest"
```

For async (fire-and-forget, poll job status later):
```yaml
- name: Deploy to Composer (async)
  run: |
    curl -sf -X POST "$COMPOSER_URL/api/v1/stacks/docs-ssh/deploy?async=true" \
      -H "Authorization: Bearer ${{ secrets.COMPOSER_API_KEY }}" \
      -H "X-Requested-With: XMLHttpRequest"
```

---

### 2. Wire webhook triggers to pipeline runs

The `TriggerWebhook` type exists in `domain/pipeline/aggregate.go:66` but nothing dispatches to it. Connect inbound webhooks to matching pipelines.

#### Design

When a webhook fires for stack X:
1. Current behavior (SyncAndRedeploy) continues as-is for backward compat
2. **Additionally**, find all pipelines with a `webhook` trigger matching the stack name
3. Run those pipelines asynchronously

This enables DAG workflows like: pull image → run migrations → deploy → health check → notify.

#### Files to change

**`internal/api/handler/webhook.go`** — add `pipelineSvc` dependency:

```go
type WebhookHandler struct {
    gitSvc      *app.GitService
    webhookRepo *store.WebhookRepo      // actual type (not webhook.Repository)
    jobs        *app.JobManager
    pipelineSvc *app.PipelineService    // new
}

func NewWebhookHandler(gitSvc *app.GitService, webhookRepo *store.WebhookRepo, jobs *app.JobManager, pipelineSvc *app.PipelineService) *WebhookHandler {
    return &WebhookHandler{gitSvc: gitSvc, webhookRepo: webhookRepo, jobs: jobs, pipelineSvc: pipelineSvc}
}
```

**`internal/api/handler/webhook.go`** `Receive()` — after L126 (end of SyncAndRedeploy goroutine), add pipeline dispatch:

```go
// Dispatch any pipelines triggered by this webhook (L127, after SyncAndRedeploy goroutine)
if h.pipelineSvc != nil {
    go h.pipelineSvc.RunByWebhookTrigger(context.Background(), stackName, payload.Branch)
}
```

**`internal/app/pipeline_service.go`** — add `RunByWebhookTrigger` method:

```go
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
```

Key differences from original plan:
- Uses `s.pipelines` not `s.repo` (actual field name)
- Uses `s.logger` not `s.log` (actual field name)
- Nil-checks `s.logger` (field is set lazily, may be nil)
- Handles `Run()` return value `(*pipeline.Run, error)` — logs error instead of ignoring
- No goroutine per pipeline — `Run()` already spawns goroutine internally

**`internal/app/pipeline_service.go`** — add `SetLogger` method (logger not passed in constructor):

```go
func (s *PipelineService) SetLogger(l *zap.Logger) { s.logger = l }
```

**`internal/api/server.go`** L197 — update `NewWebhookHandler` to pass `pipelineSvc`:

```go
// Was:  handler.NewWebhookHandler(deps.GitService, deps.WebhookRepo, deps.Jobs)
// Now:
handler.NewWebhookHandler(deps.GitService, deps.WebhookRepo, deps.Jobs, deps.PipelineService)
```

**`cmd/composerd/main.go`** ~L244 — set logger on pipeline service:

```go
if pipelineSvc != nil {
    pipelineSvc.SetLogger(logger)
}
```

#### Pipeline webhook trigger config

```json
{
  "name": "deploy-docs-ssh",
  "triggers": [
    {
      "type": "webhook",
      "config": {
        "stack": "docs-ssh",
        "branch": "main"
      }
    }
  ],
  "steps": [
    {"id": "pull", "type": "compose_pull", "config": {"stack": "docs-ssh"}},
    {"id": "deploy", "type": "compose_up", "config": {"stack": "docs-ssh"}, "depends_on": ["pull"]},
    {"id": "notify", "type": "http_request", "config": {"url": "https://ntfy.sh/deploys", "method": "POST"}, "depends_on": ["deploy"]}
  ]
}
```

---

### 3. SOPS support in pipeline compose steps

Pipeline compose steps (`compose_up`, `compose_pull`, etc.) bypass SOPS decrypt/re-encrypt. Stacks with encrypted `.env` will fail when deployed via pipeline.

**Existing bug**: Compose steps also pass stack name directly as path arg to `compose.Up(ctx, stackName, "")` instead of resolving to the actual filesystem path. This fix addresses both issues.

#### Files to change

**`internal/app/pipeline_executor.go`** — expand struct with stack/SOPS deps:

```go
type PipelineExecutor struct {
    compose   *docker.Compose
    bus       domevent.Bus
    stacks    stack.StackRepository       // new — resolve stack name → path
    gitCfgs   stack.GitConfigRepository   // new — per-stack SOPS age key
    stacksDir string                      // new — global age key fallback
}

func NewPipelineExecutor(
    compose *docker.Compose,
    bus domevent.Bus,
    stacks stack.StackRepository,
    gitCfgs stack.GitConfigRepository,
    stacksDir string,
) *PipelineExecutor {
    return &PipelineExecutor{
        compose:   compose,
        bus:       bus,
        stacks:    stacks,
        gitCfgs:   gitCfgs,
        stacksDir: stacksDir,
    }
}
```

**`internal/app/pipeline_executor.go`** — add `executeComposeStep` method:

```go
// executeComposeStep resolves the stack path, handles SOPS decrypt/re-encrypt,
// and runs the compose operation. Fixes the existing bug where stack name was
// passed directly as filesystem path.
func (e *PipelineExecutor) executeComposeStep(ctx context.Context, step pipeline.Step, op string) (string, error) {
    stackName, _ := step.Config["stack"].(string)
    if stackName == "" {
        return "", fmt.Errorf("compose_%s: missing stack config", op)
    }

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
        // Fallback: use stack name as path (legacy behavior)
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
```

Then update the `executeStep` switch to use `executeComposeStep`:

```go
case pipeline.StepComposeUp:
    return e.executeComposeStep(ctx, step, "up")
case pipeline.StepComposeDown:
    return e.executeComposeStep(ctx, step, "down")
case pipeline.StepComposePull:
    return e.executeComposeStep(ctx, step, "pull")
case pipeline.StepComposeRestart:
    return e.executeComposeStep(ctx, step, "restart")
```

**`cmd/composerd/main.go`** L243 — update `NewPipelineExecutor` call:

```go
// Was:  pipelineExecutor = app.NewPipelineExecutor(compose, bus)
// Now:
pipelineExecutor = app.NewPipelineExecutor(compose, bus, stackRepo, gitConfigRepo, cfg.StacksDir)
```

**Test files** — update all `NewPipelineExecutor` calls:

All existing test calls use `app.NewPipelineExecutor(nil, bus)` for non-compose tests. These need updating to pass `nil` for the new params:

```go
// Was:  executor := app.NewPipelineExecutor(nil, bus)
// Now:
executor := app.NewPipelineExecutor(nil, bus, nil, nil, "")
```

---

## Migration Path

1. Implement the `/deploy` endpoint first (smallest change, immediate value)
2. Update CI workflows (docs-ssh, caddy-compose) to use single `/deploy` call
3. Implement webhook → pipeline dispatch
4. Add SOPS to pipeline executor
5. Optionally migrate complex deploy workflows from webhook to pipeline

## Backward Compatibility

- Push webhooks continue to work as-is (SyncAndRedeploy unchanged)
- `/deploy` is additive — new endpoint, no existing behavior changes
- Pipeline webhook triggers are opt-in — only fires if a pipeline has a matching trigger
- SOPS in pipeline executor is transparent — only activates if `.env` is encrypted
- Pipeline executor falls back to stack-name-as-path when `stacks` repo is nil (tests)

## Testing

- Unit test: `Deploy` handler calls `SyncAndRedeploy` with correct context/timeout
- Unit test: `Deploy` handler async mode creates job + calls Start/Complete/Fail correctly
- Unit test: `Deploy` handler returns 404 for non-existent stack
- Unit test: `RunByWebhookTrigger` matches stack name + branch filter
- Unit test: `RunByWebhookTrigger` skips non-matching pipelines
- Unit test: `RunByWebhookTrigger` logs error when `Run()` fails
- Unit test: `executeComposeStep` resolves stack name → path via repo
- Unit test: `executeComposeStep` decrypts/re-encrypts SOPS correctly
- Unit test: `executeComposeStep` falls back to name-as-path when stacks=nil
- Integration test: CI pushes image → calls `/deploy` → stack running new image
- Integration test: webhook fires → pipeline runs → stack deployed
- E2E: full GH Actions workflow → Composer deploy → container healthy
