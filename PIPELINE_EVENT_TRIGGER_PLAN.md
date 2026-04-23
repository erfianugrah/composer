# Event-Triggered Pipelines + `docker_exec` Step Type

> **Mostly shipped.** Sections 1 and 2 landed in v0.8.1. Section 3 is deferred
> indefinitely — no concrete use case today. This doc now serves as a
> retrospective + small backlog of adjacent ideas. For the shipped
> implementation see `internal/domain/pipeline/aggregate.go`,
> `internal/app/pipeline_executor.go`, and `internal/app/pipeline_service.go`.

## Status

- [x] Section 1: `docker_exec` step type (shipped in v0.8.1)
- [x] Section 2: `event` trigger type + `PipelineService.SubscribeBus` (shipped in v0.8.1)
- [ ] Section 3 (optional): `http_request` method/body support — **deferred indefinitely**. The target use case (caddy-compose reload) uses `docker_exec` against wafctl's loopback API, not HTTP. Revisit when a pipeline step needs to call an externally-reachable HTTP endpoint with POST + body.
- [x] Tests: domain `Valid()`, `dockerExecArgv` argv parser (7 cases), nil-client guard, missing-container guard, event trigger matching / stack-mismatch / event-mismatch / empty-stack-filter / ignore-non-stack-events (5 cases)
- [ ] Integration tests (testcontainers): deferred. Unit tests cover pre-dispatch logic; actual Docker exec is a thin wrapper around the SDK. Low marginal value.
- [x] Docs updates: `docs/api-reference.md` gained a Pipelines section with step-type / trigger-type enum documentation + config shapes

## Original Problem (kept for context)

Real-world scenario: the `caddy-compose` stack bind-mounts its `Caddyfile`
from `/mnt/user/composer/stacks/caddy/Caddyfile` (git-managed). When a
`git push` lands, the webhook → `SyncAndRedeploy` pipeline pulls the new
`Caddyfile` into place, but:

1. `docker compose up -d` is a **no-op** — bind-mount file changes do not
   trigger container recreation (compose only reacts to compose.yaml /
   image / env var / volume definition changes).
2. Caddy keeps serving the old config in memory. Nothing reloads it.

The `caddy-compose/Makefile` had `caddy-reload` manually running three
`docker exec wafctl wget POST /api/...` calls after sync. These hit
wafctl's loopback admin API which regenerates `policy-rules.json` and
calls Caddy's admin API to hot-reload. We wanted this to happen
automatically on every successful deploy — no manual `make`.

## Original Root Cause Diagnosis (still accurate)

Four gaps blocked the use case:

1. **Race on webhook dispatch**: `SyncAndRedeploy` and `RunByWebhookTrigger`
   both fired as parallel goroutines with no ordering guarantee. A
   webhook-triggered pipeline could start before the sync finished
   pulling the new file.
2. **No post-deploy trigger type**: `manual`, `webhook`, `schedule` only —
   no way to say "run AFTER sync completes successfully".
3. **`docker_exec` step type referenced but unimplemented**: the RBAC gate
   in the pipeline Update handler mentioned `"docker_exec"` as a string
   literal, but `StepType.Valid()` didn't accept it and the executor had
   no case. Dead security theater.
4. **`http_request` is GET-only** — relevant only if we wanted to reach
   wafctl over HTTP. We went with `docker_exec` instead, so this stays
   deferred.

## What shipped in v0.8.1

### `docker_exec` step type

- `StepDockerExec` constant + `Valid()` switch arm in
  `internal/domain/pipeline/aggregate.go`
- `Client.ExecRun` in `internal/infra/docker/terminal.go` using
  `stdcopy.StdCopy` to demux stdout/stderr. Bounded by
  `ExecMaxOutput = 1 MB` per stream via `CappedBuffer` (promoted to
  exported type in v0.8.2 so `shell_command` could share it).
- `executeDockerExec` case in `PipelineExecutor.executeStep`
- `dockerExecArgv` parser accepts both forms:
  - `cmd: []string` (preferred — quote-safe, no shell required in the
    container)
  - `command: string` (wrapped in `sh -c`, requires a shell in the
    target container)
- `NewPipelineExecutor` gained a `*docker.Client` parameter. All 12 test
  call sites updated to pass `nil` (tests don't exercise `docker_exec`).

Admin-only enforcement split across two handlers:

- `Create` (`handler/pipeline.go` — line offsets shift over time, search
  for the `authmw.CheckRole(ctx, auth.RoleAdmin)` call at the top of
  `Create`): blanket admin-only regardless of step types
- `Update`: operator+ base role with admin escalation if any step is
  `shell_command` or `docker_exec`. The string-literal check that used
  to be dead code is now load-bearing.

### `event` trigger type

- `TriggerEvent` constant in the domain
- `PipelineService.SubscribeBus(bus)` wires the service into the event
  bus once at startup. Dispatch happens in the bus-callback goroutine;
  `PipelineService.Run` spawns its own goroutine so the callback doesn't
  block.
- `runByEventTrigger` filters pipelines by exact-match `event` +
  optional `stack` config field. Empty `stack` matches any stack.
- Handles all six stack-scoped events: `stack.created`, `stack.deployed`,
  `stack.stopped`, `stack.updated`, `stack.deleted`, `stack.error`.
  Non-stack events (pipeline run lifecycle, container state) deliberately
  do not trigger pipelines.
- Uses `context.Background()` — the publisher's ctx may be short-lived;
  our pipeline run lifetime is independent.

### DTO enum expansion

`PipelineStepDTO.Type` gained `docker_exec`; `TriggerDTO.Type` gained
`event`. `web/src/lib/api/types.ts` regenerated. Without this change,
Huma's `enum:` validator would reject any POST using the new values.

## Example: `reload-caddy` pipeline

Use the `cmd` slice form — quote-safe, no shell required in the target
container, and recommended over the `command` string form:

```json
{
  "name": "reload-caddy",
  "description": "Regenerate WAF + CSP + security headers and reload Caddy after a Caddyfile push.",
  "triggers": [
    {
      "type": "event",
      "config": {"event": "stack.deployed", "stack": "caddy"}
    }
  ],
  "steps": [
    {
      "id": "deploy-waf",
      "name": "Deploy WAF policy",
      "type": "docker_exec",
      "config": {
        "container": "wafctl",
        "cmd": ["wget", "-qO-", "-T", "120", "--post-data=", "http://localhost:8080/api/deploy"]
      },
      "timeout": "2m"
    },
    {
      "id": "deploy-csp",
      "name": "Deploy CSP",
      "type": "docker_exec",
      "depends_on": ["deploy-waf"],
      "config": {
        "container": "wafctl",
        "cmd": ["wget", "-qO-", "-T", "120", "--post-data=", "http://localhost:8080/api/csp/deploy"]
      },
      "timeout": "2m"
    },
    {
      "id": "deploy-sec-headers",
      "name": "Deploy Security Headers",
      "type": "docker_exec",
      "depends_on": ["deploy-csp"],
      "config": {
        "container": "wafctl",
        "cmd": ["wget", "-qO-", "-T", "120", "--post-data=", "http://localhost:8080/api/security-headers/deploy"]
      },
      "timeout": "2m"
    }
  ]
}
```

Flow on `git push`:

1. Gitea fires push webhook → `POST /api/v1/hooks/{id}`
2. Webhook handler validates HMAC, spawns `SyncAndRedeploy` goroutine
3. `SyncAndRedeploy`: git pull → SOPS decrypt → `docker compose pull` →
   `docker compose up -d` (no-op for caddy stack; harmless) → re-encrypt
   SOPS → `bus.Publish(StackDeployed{Name: "caddy"})`
4. `PipelineService.SubscribeBus` callback matches `stack.deployed` +
   stack `caddy` → calls `Run(ctx, p.ID, "event:stack.deployed:caddy")`
5. DAG executes three `docker_exec` steps sequentially (each depends on
   the previous): wafctl regenerates `policy-rules.json` +
   `custom-waf-settings.conf` and calls Caddy admin API to hot-reload
6. Caddy serves the new `Caddyfile`

Net effect: same as `make caddy-reload`, automatic on every push.

## Deferred backlog (moved out of original plan)

These were listed as "Future Work" in the pre-ship plan. Mostly
speculative — keep here so the ideas don't vanish, promote to their own
plan docs when someone commits to shipping them.

- **`http_request` POST/method/body support** (original Section 3). No
  blocker today. Ship when a pipeline needs to call an externally-reachable
  HTTP endpoint with method/body/headers control.
- **`docker_exec` service-label resolution**. Today `container` is a
  literal container name. Compose mangles names (`caddy-wafctl-1`), so
  operators must know the exact name. Accept `service: "wafctl"` config
  and resolve via the compose project label on running containers,
  surviving rename/restart.
- **Post-sync-only trigger**. The existing webhook trigger fires on every
  delivery. A variant that only fires when `GitService.Sync` actually
  pulled new commits (uses the `changed` bool already returned) would
  skip redundant redeploys.
- **`ContainerHealthChanged` subscription**. Add the event type to
  `SubscribeBus`'s type switch so a pipeline can react to a container
  flapping (e.g. "if caddy goes unhealthy for 30s, try restarting").
- **Structured step outputs (JSON)**. Today each step's output is a
  plain string. If downstream steps could reference upstream `result.foo`
  JSONPath-style, pipelines would compose better. Requires changing
  `StepResult.Output` shape — breaking change.
- **Pipeline-to-pipeline chaining via `PipelineRunFinished`**. The event
  is already on the bus; adding `event: "pipeline.run.finished"` to the
  SubscribeBus type switch would enable pipeline A to trigger pipeline B
  on success. Cycle risk — needs loop detection.

## Migration path (historical — for reference)

1. ✅ Shipped `docker_exec` step in v0.8.1 (unblocks the RBAC gate that
   was dead code since inception)
2. ✅ Shipped `event` trigger in v0.8.1 (post-deploy pipelines for any
   stack)
3. Caddy-compose maintainer creates the `reload-caddy` pipeline via
   `POST /api/v1/pipelines` with the JSON above, or via the UI once it
   supports custom steps/triggers (still operator-only API for now)
4. Update `caddy-compose/Makefile` to remove `caddy-reload` target OR
   keep it as a manual "reload without pushing" tool

## Backward compatibility

- Existing pipelines (manual/webhook/schedule triggers, non-docker_exec
  steps) unchanged
- `NewPipelineExecutor` signature change is internal — all 12 test call
  sites updated in the same commit
- Frontend (`PipelinePage.tsx`) passes types as free-form strings;
  validation is server-side via the DTO enum. Frontend UI still only
  offers `compose_up` + `shell_command` in the create form — power users
  must POST JSON with `docker_exec` or `event` types until the UI
  catches up.
- Event triggers coexist with webhook triggers on the same pipeline;
  users wanting both fire twice per push on purpose.
