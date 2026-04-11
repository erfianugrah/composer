# Deploy Endpoint + Webhook-Triggered Pipelines

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

`SyncAndRedeploy` in `git_service.go:163-216` does the full flow (sync → SOPS decrypt → pull → up) but is **only reachable via webhook**. No authenticated API endpoint exposes it.

## Solution — Two Changes

### 1. `POST /api/v1/stacks/{name}/deploy` endpoint

Expose `SyncAndRedeploy` as an authenticated API endpoint. One call from CI after image build. Handles sync, SOPS decryption, image pull, and deploy atomically.

#### Files to change

**`internal/api/handler/git.go`** — add handler:

```go
// Deploy syncs git, pulls images, and redeploys (for CI integration).
func (h *GitHandler) Deploy(ctx context.Context, input *dto.StackNameInput) (*dto.GitDeployOutput, error) {
    if err := authmw.CheckRole(ctx, auth.RoleOperator); err != nil {
        return nil, err
    }
    // Always async — sync+pull+up is long-running
    if h.jobs != nil {
        jobID := h.jobs.Create("deploy", input.Name)
        go func() {
            opCtx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
            defer cancel()
            action, err := h.gitSvc.SyncAndRedeploy(opCtx, input.Name)
            if err != nil {
                h.jobs.Fail(jobID, err.Error())
                return
            }
            h.jobs.Complete(jobID, action)
        }()
        out := &dto.GitDeployOutput{}
        out.Body.Action = "accepted"
        out.Body.JobID = jobID
        return out, nil
    }
    // Fallback: synchronous
    opCtx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
    defer cancel()
    action, err := h.gitSvc.SyncAndRedeploy(opCtx, input.Name)
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

**`internal/api/dto/git.go`** — add output DTO:

```go
type GitDeployOutputBody struct {
    Action string `json:"action" doc:"Deploy action taken (redeployed, synced_pending_manual, error)"`
    JobID  string `json:"job_id,omitempty" doc:"Background job ID (async mode)"`
}

type GitDeployOutput struct {
    Body GitDeployOutputBody
}
```

**`internal/api/handler/git.go`** `Register()` — add route:

```go
huma.Register(api, huma.Operation{
    OperationID: "deploySyncStack",
    Method:      http.MethodPost,
    Path:        "/api/v1/stacks/{name}/deploy",
    Summary:     "Sync git, pull images, and redeploy",
    Description: "Full deploy pipeline: git pull → SOPS decrypt → docker compose pull → docker compose up -d. Designed for CI to call after image push.",
    Tags:        []string{"git"},
}, h.Deploy)
```

**`internal/api/server.go`** — no change needed, git handler already registered.

#### Behavior

- Requires `Operator` role (same as other stack ops)
- Works only for git-backed stacks (returns 422 for local stacks)
- Supports `?async=true` for background execution via `JobManager`
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

---

### 2. Wire webhook triggers to pipeline runs

The `TriggerWebhook` type exists in `aggregate.go:66` but nothing dispatches to it. Connect inbound webhooks to matching pipelines.

#### Design

When a webhook fires for stack X:
1. Current behavior (SyncAndRedeploy) continues as-is for backward compat
2. **Additionally**, find all pipelines with a `webhook` trigger matching the stack name
3. Run those pipelines asynchronously

This enables DAG workflows like: pull image → run migrations → deploy → health check → notify.

#### Files to change

**`internal/api/handler/webhook.go`** `Receive()` — after SyncAndRedeploy goroutine, add pipeline dispatch:

```go
// After L126 (existing SyncAndRedeploy goroutine):

// Dispatch any pipelines triggered by this webhook
go func() {
    if h.pipelineSvc != nil {
        h.pipelineSvc.RunByWebhookTrigger(context.Background(), stackName, payload.Branch)
    }
}()
```

**`internal/api/handler/webhook.go`** — add `pipelineSvc` dependency:

```go
type WebhookHandler struct {
    gitSvc      *app.GitService
    webhookRepo webhook.Repository
    jobs        *app.JobManager
    pipelineSvc *app.PipelineService  // new
}
```

**`internal/app/pipeline_service.go`** — add method:

```go
// RunByWebhookTrigger finds pipelines with webhook triggers matching the
// stack name and branch, then runs them asynchronously.
func (s *PipelineService) RunByWebhookTrigger(ctx context.Context, stackName, branch string) {
    pipelines, err := s.repo.List(ctx)
    if err != nil {
        s.log.Error("listing pipelines for webhook trigger", zap.Error(err))
        return
    }
    for _, p := range pipelines {
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
            s.log.Info("webhook triggered pipeline",
                zap.String("pipeline", p.Name),
                zap.String("stack", stackName))
            go s.Run(ctx, p.ID, "webhook:"+stackName)
        }
    }
}
```

**`internal/api/server.go`** — pass `pipelineSvc` to `WebhookHandler`:

```go
// L196 area — webhook receiver registration
webhookHandler := handler.NewWebhookHandler(gitSvc, webhookRepo, jobMgr, pipelineSvc)
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

#### Files to change

**`internal/app/pipeline_executor.go`** — wrap compose step execution with SOPS:

```go
// Before existing compose step cases (~L143):
func (e *PipelineExecutor) executeComposeStep(ctx context.Context, step pipeline.Step, op string) (string, error) {
    stackName := step.Config["stack"]
    st, err := e.stacks.GetByName(ctx, stackName)
    if err != nil {
        return "", fmt.Errorf("stack %q not found: %w", stackName, err)
    }

    // SOPS decrypt if available
    composePath := ""
    if sops.IsAvailable() {
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

    switch op {
    case "up":
        _, err = e.compose.Up(ctx, st.Path, composePath)
    case "down":
        _, err = e.compose.Down(ctx, st.Path, composePath, false)
    case "pull":
        _, err = e.compose.Pull(ctx, st.Path, composePath)
    case "restart":
        _, err = e.compose.Restart(ctx, st.Path, composePath)
    }
    if err != nil {
        return "", err
    }
    return fmt.Sprintf("compose %s completed for %s", op, stackName), nil
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

**`internal/app/pipeline_executor.go`** — add dependencies:

```go
type PipelineExecutor struct {
    compose   *docker.Compose
    stacks    stack.Repository   // new — resolve stack name → path
    gitCfgs   stack.GitConfigRepository  // new — per-stack SOPS age key
    stacksDir string             // new — global age key fallback
    log       *zap.Logger
}
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

## Testing

- Unit test: `Deploy` handler calls `SyncAndRedeploy` with correct context/timeout
- Unit test: `RunByWebhookTrigger` matches stack name + branch filter
- Unit test: `executeComposeStep` decrypts/re-encrypts SOPS correctly
- Integration test: CI pushes image → calls `/deploy` → stack running new image
- Integration test: webhook fires → pipeline runs → stack deployed
- E2E: full GH Actions workflow → Composer deploy → container healthy
