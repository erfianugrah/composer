# Composer - Architecture Document

A lightweight, self-hosted Docker Compose management platform.
Think Dockge's simplicity meets Portainer's power, built from scratch with
Go + Astro + Shadcn/ui.

---

## 1. Competitive Analysis

### Dockge (louislam/dockge)

- **Stack**: Node.js + TypeScript + Vue 3 + Socket.IO + SQLite
- **Stars**: 22.8k | **Binary size**: ~200MB Docker image
- **Strengths**: File-based compose storage, reactive UI, simple UX, real-time output
- **Weaknesses**:
  - No REST API (Socket.IO only) -- impossible to script/automate
  - No CI/CD or pipeline workflows
  - No image build support
  - No RBAC or API key auth
  - No container resource metrics
  - In-memory state for some operations
  - 30+ npm production dependencies, node-pty native dep
  - No OpenAPI spec, no generated clients

### Portainer (portainer/portainer)

- **Stack**: Go backend + TypeScript/React frontend
- **Stars**: 37.1k | **Binary size**: ~300MB Docker image
- **Strengths**: Full Docker/K8s/Swarm management, RBAC, REST API, edge agents
- **Weaknesses**:
  - Massively bloated for compose-only use cases
  - Slow stack deployments with opaque spinner/errors
  - Heavy resource usage (~150MB+ RAM idle)
  - Complex UI for simple tasks
  - Freemium model locks features behind Business Edition
  - 5,684 commits of legacy code

### Composer (this project) -- target differentiators

- **Single Go binary** (~15MB) + embedded Astro frontend
- **REST API first** with auto-generated OpenAPI 3.1 spec
- **Hybrid transport**: REST + SSE + WebSocket (terminal only)
- **CI-esque pipelines** for deployment workflows
- **File-based compose storage** (like Dockge, never kidnaps your files)
- **RBAC + API keys** for multi-user and automation
- **< 50MB RAM** idle target
- **Generated TypeScript client** from OpenAPI for type-safe frontend

---

## 2. Philosophy & Principles

### Domain-Driven Design (DDD)

- Clear bounded contexts with aggregate roots
- Domain events for cross-context communication
- Repository pattern for persistence abstraction
- Value objects for immutable domain concepts

### Test-Driven Development (TDD)

- Domain model tests written BEFORE implementation
- Integration tests for infrastructure (Docker SDK, DB)
- API handler tests using huma's `humatest` package
- E2E tests for critical user flows

### Architectural Rules

1. Domain layer has ZERO external dependencies (no Docker SDK, no DB drivers)
2. Application services orchestrate domain objects and infrastructure
3. Infrastructure implements domain repository interfaces
4. API layer is a thin translation between HTTP and application services
5. Frontend is independently deployable, communicates only via API

---

## 3. Tech Stack

| Layer | Technology | Status | Notes |
|-------|-----------|--------|-------|
| **Language** | Go 1.26+ | Done | Docker SDK native, single binary, goroutines |
| **HTTP Framework** | Huma v2.37+ + Chi v5 | Done | Auto OpenAPI 3.1, built-in SSE, RFC9457 errors |
| **Auth** | Custom (session + API key + RBAC) | Done | Session cookie + API key + role middleware |
| **Database** | PostgreSQL (`jackc/pgx/v5`) | Done | pgxpool for queries, goose for migrations |
| **Cache** | Valkey (`valkey-io/valkey-go`) | Phase 4 | Currently in-process event bus. Valkey for multi-instance |
| **Migrations** | goose v3.27+ | Done | Embedded SQL via `embed.FS`, Provider API |
| **Logging** | zap | Done | JSON (prod) + console (dev) encoders |
| **Docker** | `docker/docker` v28+ | Done | Engine SDK + Podman auto-detect |
| **Compose** | CLI wrapper (`docker compose`) | Done | Shell out to V2. Programmatic library optional later |
| **Git** | `go-git/go-git/v5` | Phase 2 | Pure Go git. Directory stubbed, not yet integrated |
| **IDs** | `oklog/ulid` | Planned | Currently using crypto/rand hex. ULID later |
| **Frontend** | Astro 6.1 + React 19 | Done | Static output, React islands via `client:load` |
| **UI Components** | Shadcn/ui + Tailwind CSS 4 | Done | Lovelace theme. button, card, badge, input built |
| **Terminal** | xterm.js + WebSocket | Backend done | WS handler complete. Frontend xterm.js component pending |
| **Code Editor** | CodeMirror 6 | Planned | YAML syntax highlighting for compose editing |
| **Streaming** | SSE (huma/sse) | Done | Events stream + container logs. Stats pending |
| **WebSocket** | `coder/websocket` v1.8+ | Done | Terminal sessions only |
| **TS Client** | openapi-typescript + openapi-fetch | Planned | Generated typed client from OpenAPI spec |

---

## 4. Architecture Overview

```
                    +------------------------------------------+
                    |            Astro Frontend                 |
                    |  +-------------+  +--------------------+ |
                    |  | SSR Pages   |  | React Islands      | |
                    |  | (Astro)     |  | - StackEditor      | |
                    |  | - Dashboard |  | - Terminal (xterm)  | |
                    |  | - Login     |  | - LogViewer         | |
                    |  | - Pipelines |  | - PipelineRunner    | |
                    |  +-------------+  | - ContainerStats    | |
                    |                   +--------------------+ |
                    +-----|-----------|-----------|------------+
                          |           |           |
                        REST        SSE     WebSocket
                    (CRUD ops)   (streams)  (terminal)
                          |           |           |
                    +-----|-----------|-----------|------------+
                    |          composerd (Go binary)           |
                    |                                          |
                    |  +------------------------------------+  |
                    |  | API Layer (Huma v2 + Chi)           |  |
                    |  | - REST handlers  -> OpenAPI 3.1    |  |
                    |  | - SSE endpoints  (logs/events/     |  |
                    |  |                   pipeline/stats)   |  |
                    |  | - WS endpoint   (terminal only)    |  |
                    |  | - Auth middleware (session/apikey)  |  |
                    |  +------------------------------------+  |
                    |                                          |
                    |  +------------------------------------+  |
                    |  | Application Services                |  |
                    |  | - StackService                     |  |
                    |  | - ContainerService                 |  |
                    |  | - PipelineService                  |  |
                    |  | - TerminalService                  |  |
                    |  | - AuthService                      |  |
                    |  | - EventBus (in-process pub/sub)    |  |
                    |  +------------------------------------+  |
                    |                                          |
                    |  +------------------------------------+  |
                    |  | Domain Layer (zero dependencies)    |  |
                    |  | - stack/    (aggregate + entities)  |  |
                    |  | - container/ (entity + value objs)  |  |
                    |  | - pipeline/ (aggregate + entities)  |  |
                    |  | - auth/     (user, session, apikey) |  |
                    |  +------------------------------------+  |
                    |                                          |
                    |  +------------------------------------+  |
                    |  | Infrastructure                      |  |
                    |  | - docker/  (moby/client + compose)  |  |
                    |  | - store/   (Postgres via pgx)       |  |
                    |  | - cache/   (Valkey)                 |  |
                    |  | - git/     (go-git v5)              |  |
                    |  | - fs/      (compose file I/O)       |  |
                    |  +------------------------------------+  |
                    +-----|------------------------------------+
                          |
                    +-----|------------------------------------+
                    |  Docker Engine (unix socket / TCP)       |
                    |  + docker compose CLI (v2)               |
                    |  Git remotes (GitHub/GitLab/Gitea/etc.)  |
                    +------------------------------------------+
```

---

## 5. DDD Domain Model

### 5.1 Bounded Context: Stack Management (Core Domain)

The primary reason this product exists. Git is a first-class citizen --
every stack can be backed by a git repository, making the repo the single
source of truth for compose configuration.

```
Stack (Aggregate Root)
  |-- name: string (unique identifier, directory name)
  |-- path: string (filesystem path to compose dir)
  |-- source: StackSource (local | git)
  |-- status: StackStatus (running|stopped|partial|unknown)
  |-- composeContent: string (raw compose.yaml)
  |-- services: []ServiceDefinition (parsed from compose)
  |-- gitConfig: *GitSource (nil for local stacks)
  |-- webhookID: *string (nil if no webhook registered)
  |-- createdAt: time.Time
  |-- updatedAt: time.Time
  |
  |-- Methods:
  |   Deploy()       -> StackDeployed event
  |   Stop()         -> StackStopped event
  |   Restart()      -> StackRestarted event
  |   Pull()         -> StackPulled event
  |   UpdateCompose(content) -> StackUpdated event
  |   Delete()       -> StackDeleted event
  |   Validate()     -> error (compose syntax + schema)
  |   Sync()         -> StackSynced event (git pull + detect changes)
  |   Rollback(commitSHA) -> StackRolledBack event
  |   GitLog()       -> []GitCommit (history of compose changes)
  |
  |-- Value Objects:
      StackSource enum { Local, Git }
      ServiceDefinition { name, image, ports, volumes, environment, healthcheck }
      StackStatus enum { Running, Stopped, Partial, Unknown, Error, Syncing }
      ComposeValidationResult { valid, errors[], warnings[] }

      GitSource (Value Object -- git configuration for a stack)
        |-- repoURL: string (https:// or git@...)
        |-- branch: string (default: "main")
        |-- composePath: string (path within repo, default: "compose.yaml")
        |-- autoSync: bool (auto-deploy on git changes)
        |-- authMethod: GitAuthMethod (none|token|sshKey|basicAuth)
        |-- credentials: *GitCredentials (encrypted at rest)
        |-- lastSyncAt: time.Time
        |-- lastCommitSHA: string
        |-- syncStatus: GitSyncStatus

      GitCredentials (Value Object -- stored encrypted in DB)
        |-- token: string (GitHub PAT, GitLab token, etc.)
        |-- sshKeyID: string (reference to stored SSH key)
        |-- username: string (for basic auth)
        |-- password: string (for basic auth)

      GitAuthMethod enum { None, Token, SSHKey, BasicAuth }
      GitSyncStatus enum { Synced, Behind, Diverged, Error, Syncing }

      GitCommit (Value Object -- from git log)
        |-- sha: string
        |-- shortSHA: string
        |-- message: string
        |-- author: string
        |-- date: time.Time
        |-- composeDiff: string (diff of compose.yaml only)
```

### 5.2 Bounded Context: Container Runtime (Core Domain)

```
Container (Entity -- not an aggregate, owned by Stack context via Docker)
  |-- id: string (Docker container ID, short)
  |-- name: string
  |-- stackName: string (parent stack)
  |-- serviceName: string (compose service)
  |-- image: string (image:tag)
  |-- status: ContainerStatus
  |-- health: HealthStatus
  |-- ports: []PortBinding
  |-- createdAt: time.Time
  |-- startedAt: time.Time
  |
  |-- Value Objects:
      ContainerStatus enum { Created, Running, Paused, Restarting, Removing, Exited, Dead }
      HealthStatus enum { Healthy, Unhealthy, Starting, None }
      PortBinding { hostIP, hostPort, containerPort, protocol }
      ResourceStats { cpuPercent, memUsage, memLimit, netIO, blockIO, pids }
      LogEntry { timestamp, stream(stdout|stderr), message }
```

### 5.3 Bounded Context: Pipeline (Supporting Domain)

CI-esque workflows for automated operations.

```
Pipeline (Aggregate Root)
  |-- id: string (ULID)
  |-- name: string
  |-- description: string
  |-- steps: []Step (ordered)
  |-- triggers: []Trigger
  |-- createdAt: time.Time
  |-- updatedAt: time.Time
  |
  |-- Methods:
  |   AddStep(step)
  |   RemoveStep(stepID)
  |   ReorderSteps(order)
  |   Run(params) -> PipelineRun
  |
  Step (Entity)
  |-- id: string
  |-- name: string
  |-- type: StepType
  |-- config: StepConfig (varies by type)
  |-- timeout: duration
  |-- continueOnError: bool
  |-- dependsOn: []stepID
  |
  |-- Value Objects:
      StepType enum { ComposeUp, ComposeDown, ComposePull, ComposeRestart,
                      ShellCommand, DockerExec, HTTPRequest, Wait, Notify }
      StepConfig (union type per StepType):
        ComposeUpConfig { stackName, services[], build, forceRecreate }
        ShellCommandConfig { command, workDir, env, shell }
        DockerExecConfig { containerID, command[], tty }
        HTTPRequestConfig { method, url, headers, body, expectStatus }
        WaitConfig { duration }
        NotifyConfig { type(webhook|email), target, template }

  Trigger (Value Object)
  |-- type: TriggerType
  |-- config: TriggerConfig
  |
      TriggerType enum { Manual, Webhook, Schedule, FileWatch }
      ScheduleConfig { cron: string }
      WebhookConfig { secret: string, path: string }
      FileWatchConfig { paths: []string, debounce: duration }

  PipelineRun (Entity -- tracks execution)
  |-- id: string (ULID)
  |-- pipelineID: string
  |-- status: RunStatus
  |-- triggeredBy: string (user or trigger ID)
  |-- startedAt: time.Time
  |-- finishedAt: time.Time
  |-- stepResults: []StepResult
  |
      RunStatus enum { Pending, Running, Success, Failed, Cancelled }
      StepResult { stepID, status, output, duration, error }
```

### 5.4 Bounded Context: Identity & Access (Generic Domain)

Custom auth, modeled after gloryhole session system + gatekeeper RBAC.

```
User (Aggregate Root)
  |-- id: string (ULID)
  |-- email: string (unique, lowercase)
  |-- passwordHash: string (bcrypt, cost 12)
  |-- role: Role
  |-- createdAt: time.Time
  |-- updatedAt: time.Time
  |-- lastLoginAt: time.Time
  |
  |-- Methods:
  |   VerifyPassword(plaintext) -> bool (constant-time)
  |   ChangePassword(old, new) -> error
  |   UpdateRole(newRole) -> error
  |
      Role enum { Admin, Operator, Viewer }
        Admin    -- full access: users, settings, stacks, pipelines, terminal
        Operator -- stacks CRUD, pipelines CRUD, terminal, containers
        Viewer   -- read-only: view stacks, containers, logs, pipeline runs

  Session (Entity)
  |-- id: string (32 bytes, crypto/rand, base64url)
  |-- userID: string
  |-- role: Role (denormalized for fast middleware checks)
  |-- createdAt: time.Time
  |-- expiresAt: time.Time
  |
  |-- Cookie: "composer_session", HttpOnly, SameSite=Lax, Secure, Path=/

  APIKey (Entity)
  |-- id: string (prefix "ck_", 16 random bytes hex)
  |-- name: string (human-readable label)
  |-- hashedKey: string (SHA-256 of full key, stored; full key shown once)
  |-- role: Role
  |-- createdBy: userID
  |-- lastUsedAt: time.Time
  |-- expiresAt: time.Time (optional, nil = never)
  |-- createdAt: time.Time
```

### 5.5 Bounded Context: Git & Webhooks (Supporting Domain)

Manages git operations and inbound webhook processing. This is what makes
Composer a GitOps tool, not just another Docker UI.

```
GitSyncService (Application Service -- orchestrates git + stack)
  |-- Clone(repoURL, branch, creds) -> cloned path
  |-- Pull(stackPath) -> (changed bool, newSHA string, diff string)
  |-- Checkout(stackPath, commitSHA) -> error
  |-- Log(stackPath, limit) -> []GitCommit
  |-- Diff(stackPath) -> string (uncommitted changes)
  |-- CommitAndPush(stackPath, message, author) -> commitSHA
  |     ^ used when user edits compose in UI -> commits back to repo

WebhookReceiver (Application Service -- processes inbound webhooks)
  |-- RegisterWebhook(stackName) -> WebhookEndpoint
  |-- ValidateSignature(provider, secret, headers, body) -> bool
  |-- Process(webhookID, payload) -> triggers git sync + optional redeploy

  WebhookEndpoint (Value Object)
    |-- id: string (ULID, used in URL path)
    |-- stackName: string
    |-- provider: WebhookProvider
    |-- secret: string (HMAC secret for signature validation)
    |-- url: string (full URL: /api/v1/webhooks/{id})
    |-- events: []string (filter: push, tag, release, etc.)
    |-- branchFilter: string (only trigger on this branch)
    |-- autoRedeploy: bool (sync + redeploy, or just sync)
    |-- createdAt: time.Time

  WebhookProvider enum { GitHub, GitLab, Gitea, Bitbucket, Generic }

  WebhookDelivery (Entity -- tracks each inbound webhook call)
    |-- id: string
    |-- webhookID: string
    |-- provider: WebhookProvider
    |-- event: string (e.g. "push", "ping")
    |-- payload: string (raw JSON, truncated)
    |-- branch: string (extracted from payload)
    |-- commitSHA: string (extracted from payload)
    |-- status: DeliveryStatus
    |-- action: string (what Composer did: "synced", "redeployed", "skipped")
    |-- error: string
    |-- processedAt: time.Time
    |-- createdAt: time.Time

  DeliveryStatus enum { Received, Processing, Success, Failed, Skipped }
```

**GitOps Flow:**

```
  GitHub/GitLab/Gitea push
         |
         v
  POST /api/v1/webhooks/{id}
         |
         v
  +--Validate signature (HMAC-SHA256)--+
  |                                     |
  | invalid -> 401 + log delivery       |
  +--valid------------------------------+
         |
         v
  +--Check branch filter----------------+
  |                                      |
  | wrong branch -> skip + log delivery  |
  +--matches-----------------------------+
         |
         v
  +--git pull (in stack directory)-------+
  |                                      |
  | no compose changes -> log "synced,   |
  |   no changes" + done                 |
  +--compose.yaml changed---------------+
         |
         v
  +--Validate new compose.yaml----------+
  |                                      |
  | invalid -> log error, don't deploy   |
  +--valid-------------------------------+
         |
         v
  +--autoRedeploy enabled?--------------+
  |                                      |
  | no  -> log "synced, pending manual   |
  |         redeploy" + SSE event        |
  | yes -> docker compose up -d          |
  |         -> SSE: StackSynced +        |
  |            StackDeployed events       |
  +--------------------------------------+
```

**Edit-in-UI -> Git Commit Flow:**

```
  User edits compose.yaml in the web editor
         |
         v
  PUT /api/v1/stacks/{name}
  (body: { compose: "...", commitMessage: "update nginx config" })
         |
         v
  +--Write compose.yaml to disk---------+
         |
         v
  +--Stack source == Git?---------------+
  |                                      |
  | no (local) -> done, just saved       |
  | yes -> git add + git commit + push   |
  |        (using stored credentials)    |
  +--------------------------------------+
         |
         v
  SSE: StackUpdated event
```

### 5.6 Domain Events

Cross-context communication via an in-process event bus.

```
StackCreated    { name, timestamp }
StackUpdated    { name, timestamp }
StackDeployed   { name, services[], timestamp }
StackStopped    { name, timestamp }
StackRestarted  { name, timestamp }
StackPulled     { name, images[], timestamp }
StackDeleted    { name, timestamp }
StackError      { name, error, timestamp }

ContainerStateChanged  { containerID, stackName, oldStatus, newStatus, timestamp }
ContainerHealthChanged { containerID, stackName, oldHealth, newHealth, timestamp }

PipelineRunStarted   { pipelineID, runID, timestamp }
PipelineStepStarted  { pipelineID, runID, stepID, timestamp }
PipelineStepFinished { pipelineID, runID, stepID, status, duration, timestamp }
PipelineRunFinished  { pipelineID, runID, status, duration, timestamp }

UserCreated  { userID, email, role, timestamp }
UserLoggedIn { userID, email, timestamp }
```

---

## 6. Transport Layer Design

### 6.1 REST API (Huma v2 -- auto-generates OpenAPI 3.1)

All CRUD operations. Every endpoint is a typed Go struct that Huma converts
to OpenAPI schema automatically.

#### Auth Endpoints (no auth required)

```
POST   /api/v1/auth/bootstrap      -- create first admin user (only when 0 users)
POST   /api/v1/auth/login           -- email + password -> session cookie
POST   /api/v1/auth/logout          -- destroy session
GET    /api/v1/auth/session         -- validate current session, return user info
```

#### User Management (admin only) -- NOT YET IMPLEMENTED

```
GET    /api/v1/users                -- list users
POST   /api/v1/users                -- create user
GET    /api/v1/users/{id}           -- get user
PUT    /api/v1/users/{id}           -- update user (role, email)
DELETE /api/v1/users/{id}           -- delete user
PUT    /api/v1/users/{id}/password  -- change password (admin or self)
```

#### API Keys (operator+) -- NOT YET IMPLEMENTED

```
GET    /api/v1/keys                 -- list API keys (redacted)
POST   /api/v1/keys                 -- create key (returns full key ONCE)
DELETE /api/v1/keys/{id}            -- revoke key
```

#### Stack Management (operator+) -- IMPLEMENTED

```
GET    /api/v1/stacks               -- list all stacks with status          [done]
POST   /api/v1/stacks               -- create new stack                     [done]
GET    /api/v1/stacks/{name}        -- get stack detail + containers        [done]
PUT    /api/v1/stacks/{name}        -- update compose content               [done]
DELETE /api/v1/stacks/{name}        -- delete stack (optionally volumes)    [done]

POST   /api/v1/stacks/{name}/up       -- deploy (docker compose up -d)     [done]
POST   /api/v1/stacks/{name}/down     -- stop (docker compose down)        [done]
POST   /api/v1/stacks/{name}/restart  -- restart all services              [done]
POST   /api/v1/stacks/{name}/pull     -- pull latest images                [done]
POST   /api/v1/stacks/{name}/validate -- validate compose syntax           [not yet]
GET    /api/v1/stacks/{name}/diff     -- pending changes vs running        [not yet]
```

#### Containers (viewer+) -- NOT YET IMPLEMENTED

```
GET    /api/v1/containers              -- list all containers across stacks
GET    /api/v1/containers/{id}         -- get container detail
POST   /api/v1/containers/{id}/start   -- start container (operator+)
POST   /api/v1/containers/{id}/stop    -- stop container (operator+)
POST   /api/v1/containers/{id}/restart -- restart container (operator+)
```

#### Pipelines (operator+) -- Phase 3

```
GET    /api/v1/pipelines               -- list pipelines
POST   /api/v1/pipelines               -- create pipeline
GET    /api/v1/pipelines/{id}          -- get pipeline detail
PUT    /api/v1/pipelines/{id}          -- update pipeline
DELETE /api/v1/pipelines/{id}          -- delete pipeline

POST   /api/v1/pipelines/{id}/run      -- trigger a pipeline run
POST   /api/v1/pipelines/{id}/cancel   -- cancel running pipeline
GET    /api/v1/pipelines/{id}/runs     -- list runs for pipeline
GET    /api/v1/pipelines/{id}/runs/{runId} -- get run detail with step results
```

#### Git Operations (operator+) -- Phase 2

```
POST   /api/v1/stacks/{name}/sync        -- git pull + detect changes
POST   /api/v1/stacks/{name}/rollback    -- checkout specific commit
GET    /api/v1/stacks/{name}/git/log     -- commit history (compose changes)
GET    /api/v1/stacks/{name}/git/diff    -- diff current vs running
GET    /api/v1/stacks/{name}/git/status  -- sync status, last commit, behind/ahead
```

#### Webhooks (operator+ for management, public for receiving) -- Phase 2

```
GET    /api/v1/webhooks                   -- list registered webhooks
POST   /api/v1/webhooks                   -- register webhook for a stack
GET    /api/v1/webhooks/{id}              -- get webhook details + recent deliveries
DELETE /api/v1/webhooks/{id}              -- unregister webhook
GET    /api/v1/webhooks/{id}/deliveries   -- list delivery history

POST   /api/v1/hooks/{id}                -- inbound webhook receiver (public, validated by signature)
                                            supports GitHub, GitLab, Gitea, Bitbucket, generic
```

#### System (viewer+) -- PARTIAL

```
GET    /api/v1/system/health           -- application health check (public)  [done]
GET    /api/v1/system/info             -- Docker engine info, disk, images   [not yet]
GET    /api/v1/system/version          -- Composer version info              [not yet]
```

#### OpenAPI -- IMPLEMENTED

```
GET    /openapi.json                   -- auto-generated OpenAPI 3.1 spec    [done]
GET    /openapi.yaml                   -- YAML variant                       [done]
GET    /docs                           -- Stoplight Elements API docs UI     [not yet]
```

### 6.2 SSE Endpoints (Server-Sent Events)

All streaming server-to-client. Uses huma's built-in `sse` package.
Each SSE endpoint holds the connection open and pushes events.

```
GET    /api/v1/sse/events              -- global domain events stream        [done]
GET    /api/v1/sse/containers/{id}/logs    -- live log stream for container  [done]
GET    /api/v1/sse/stacks/{name}/logs  -- aggregated logs for all services   [not yet]
GET    /api/v1/sse/containers/{id}/stats   -- live CPU/mem/net/disk stats    [not yet]
GET    /api/v1/sse/pipelines/{id}/runs/{runId} -- pipeline execution output  [Phase 3]
```

**SSE Event Format:**

```
event: container.state
data: {"containerId":"abc123","stack":"docs-ssh","old":"running","new":"exited","ts":"..."}

event: log
data: {"stream":"stdout","message":"Server started on :8080","ts":"..."}

event: stats
data: {"cpu":2.3,"memUsage":45000000,"memLimit":536870912,"ts":"..."}

event: pipeline.step.output
data: {"stepId":"pull","line":"Pulling image nginx:latest...","ts":"..."}
```

### 6.3 WebSocket (Terminal Only)

Bidirectional communication required for interactive terminal.
Only endpoint using WebSocket.

```
GET    /api/v1/ws/terminal/{id}  -- interactive shell session
                                    query: ?shell=/bin/sh&cols=80&rows=24
```

**Protocol:**

- Client -> Server: raw stdin bytes (keystrokes)
- Server -> Client: raw stdout/stderr bytes
- Control messages: JSON-framed resize events `{"type":"resize","cols":120,"rows":40}`
- Ping/pong keepalive every 30 seconds

---

## 7. Auth System Design

Modeled after gloryhole (Go session auth) + gatekeeper (RBAC, bootstrap).

### 7.1 Authentication Flow

```
                    Request
                       |
                       v
              +------------------+
              | Auth Middleware   |
              +------------------+
                       |
          +------------+------------+
          |            |            |
     Check for    Check for    Check for
     Session      API Key      no auth
     Cookie       Header       needed?
          |            |            |
          v            v            v
     Validate      Validate     Bypass
     in DB/cache   SHA-256      paths:
     -> user+role  -> key+role  /health
                                /api/v1/auth/login
                                /api/v1/auth/bootstrap
                                /openapi.*
                                /docs
                                /static
                       |
                       v
              +------------------+
              | RBAC Check       |
              | role >= required |
              +------------------+
                       |
                       v
                    Handler
```

### 7.2 Session Management

- **Token**: 32 bytes from `crypto/rand`, base64url-encoded
- **Storage**: `sessions` table in Postgres (persistent across restarts)
- **TTL**: 24 hours default, configurable
- **Cookie**: `composer_session`, HttpOnly, SameSite=Lax, Secure (when TLS), Path=/
- **Cleanup**: Background goroutine every 5 minutes evicts expired sessions
- **Cache**: Active sessions cached in Valkey (TTL-matched) to avoid DB hit per request

### 7.3 API Key Auth

- **Format**: `ck_<32 hex chars>` (shown once on creation)
- **Storage**: SHA-256 hash stored in DB, never the raw key
- **Header**: `Authorization: Bearer ck_...` or `X-API-Key: ck_...`
- **Comparison**: Constant-time via HMAC trick (like gatekeeper)
- **Cache**: Hashed key -> role cached in Valkey

### 7.4 Bootstrap Flow

- On first startup with 0 users, `/api/v1/auth/bootstrap` is enabled
- Frontend shows a setup wizard to create the first admin user
- Once first user exists, bootstrap endpoint returns 409 Conflict

### 7.5 RBAC Role Hierarchy

```
Admin > Operator > Viewer

Admin:    everything (user management, settings, full stack/pipeline/terminal access)
Operator: stacks CRUD, pipelines CRUD, containers start/stop, terminal exec
Viewer:   read-only (list stacks, view containers, view logs, view pipeline runs)
```

### 7.6 Security Hardening

- Bcrypt cost 12 for password hashing
- Constant-time comparison for all credential checks
- CSRF protection: mutating requests on cookie-auth require `X-Requested-With` header
- Rate limiting on login: 5 req/min per IP (token bucket)
- Security headers: `X-Content-Type-Options`, `X-Frame-Options`, CSP, HSTS
- Open redirect prevention on login `?next=` parameter
- Session fixation prevention: revoke old session on login

---

## 8. Pipeline / CI Workflow Engine

### 8.1 Concept

Pipelines are user-defined sequences of steps that automate Docker operations.
Think GitHub Actions for your compose stacks.

### 8.2 Example Pipeline YAML

```yaml
name: deploy-web-stack
description: Pull latest images and deploy with zero-downtime
triggers:
  - type: webhook
    config:
      path: /hooks/deploy-web
      secret: ${WEBHOOK_SECRET}
  - type: schedule
    config:
      cron: "0 3 * * *" # nightly at 3 AM

steps:
  - name: pull-images
    type: compose_pull
    config:
      stack: web-app
      services: [nginx, api, worker]

  - name: deploy
    type: compose_up
    config:
      stack: web-app
      force_recreate: true
    depends_on: [pull-images]

  - name: health-check
    type: http_request
    config:
      method: GET
      url: http://localhost:8080/health
      expect_status: 200
      retries: 5
      retry_delay: 3s
    depends_on: [deploy]

  - name: notify
    type: notify
    config:
      type: webhook
      target: https://hooks.slack.com/...
      template: "Deployed web-app: {{.status}}"
    depends_on: [health-check]
    continue_on_error: true
```

### 8.3 Execution Engine

- Steps form a DAG (directed acyclic graph) via `depends_on`
- Independent steps run concurrently (Go goroutines)
- Each step has its own timeout, output capture, and status tracking
- Pipeline run output is streamed to connected SSE clients in real-time
- Cancellation propagates via `context.Context`
- Results are persisted in DB with full output logs

---

## 9. Data Model

### 9.1 PostgreSQL Schema

```sql
-- Enable extensions
CREATE EXTENSION IF NOT EXISTS "pgcrypto";  -- for gen_random_uuid if needed

-- Users
CREATE TABLE users (
    id          TEXT PRIMARY KEY,        -- ULID
    email       TEXT NOT NULL UNIQUE,
    password_hash TEXT NOT NULL,
    role        TEXT NOT NULL DEFAULT 'viewer'
                CHECK (role IN ('admin', 'operator', 'viewer')),
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_login_at TIMESTAMPTZ
);

-- Sessions (persistent, not in-memory)
CREATE TABLE sessions (
    id          TEXT PRIMARY KEY,        -- crypto/rand base64url
    user_id     TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    role        TEXT NOT NULL,           -- denormalized for fast lookup
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at  TIMESTAMPTZ NOT NULL
);
CREATE INDEX idx_sessions_expires ON sessions(expires_at);
CREATE INDEX idx_sessions_user ON sessions(user_id);

-- API Keys
CREATE TABLE api_keys (
    id          TEXT PRIMARY KEY,        -- "ck_" prefixed
    name        TEXT NOT NULL,
    hashed_key  TEXT NOT NULL UNIQUE,    -- SHA-256 hex
    role        TEXT NOT NULL CHECK (role IN ('admin', 'operator', 'viewer')),
    created_by  TEXT NOT NULL REFERENCES users(id),
    last_used_at TIMESTAMPTZ,
    expires_at  TIMESTAMPTZ,            -- NULL = never expires
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_api_keys_hash ON api_keys(hashed_key);

-- Stack metadata (filesystem is source of truth for compose content)
CREATE TABLE stacks (
    name        TEXT PRIMARY KEY,
    path        TEXT NOT NULL UNIQUE,
    source      TEXT NOT NULL DEFAULT 'local'
                CHECK (source IN ('local', 'git')),
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Git configuration for git-backed stacks
CREATE TABLE stack_git_configs (
    stack_name    TEXT PRIMARY KEY REFERENCES stacks(name) ON DELETE CASCADE,
    repo_url      TEXT NOT NULL,
    branch        TEXT NOT NULL DEFAULT 'main',
    compose_path  TEXT NOT NULL DEFAULT 'compose.yaml',
    auto_sync     BOOLEAN NOT NULL DEFAULT true,
    auth_method   TEXT NOT NULL DEFAULT 'none'
                  CHECK (auth_method IN ('none', 'token', 'ssh_key', 'basic')),
    credentials   TEXT,                          -- encrypted JSON blob (AES-256-GCM)
    last_sync_at  TIMESTAMPTZ,
    last_commit   TEXT,                          -- SHA
    sync_status   TEXT NOT NULL DEFAULT 'synced'
                  CHECK (sync_status IN ('synced', 'behind', 'diverged', 'error', 'syncing'))
);

-- Webhook endpoints (inbound)
CREATE TABLE webhooks (
    id          TEXT PRIMARY KEY,        -- ULID
    stack_name  TEXT NOT NULL REFERENCES stacks(name) ON DELETE CASCADE,
    provider    TEXT NOT NULL DEFAULT 'generic'
                CHECK (provider IN ('github', 'gitlab', 'gitea', 'bitbucket', 'generic')),
    secret      TEXT NOT NULL,           -- HMAC secret for signature validation
    branch_filter TEXT,                  -- only trigger on this branch (null = any)
    auto_redeploy BOOLEAN NOT NULL DEFAULT true,
    events      JSONB,                   -- array of event types to accept
    created_by  TEXT NOT NULL REFERENCES users(id),
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_webhooks_stack ON webhooks(stack_name);

-- Webhook delivery log
CREATE TABLE webhook_deliveries (
    id          TEXT PRIMARY KEY,        -- ULID
    webhook_id  TEXT NOT NULL REFERENCES webhooks(id) ON DELETE CASCADE,
    event       TEXT NOT NULL,           -- e.g. "push", "ping"
    branch      TEXT,
    commit_sha  TEXT,
    status      TEXT NOT NULL DEFAULT 'received'
                CHECK (status IN ('received', 'processing', 'success', 'failed', 'skipped')),
    action      TEXT,                    -- "synced"|"redeployed"|"skipped"|"error"
    error       TEXT,
    payload     JSONB,                   -- raw JSON (truncated to 4KB)
    processed_at TIMESTAMPTZ,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_deliveries_webhook ON webhook_deliveries(webhook_id, created_at DESC);

-- Pipelines
CREATE TABLE pipelines (
    id          TEXT PRIMARY KEY,        -- ULID
    name        TEXT NOT NULL,
    description TEXT,
    config      JSONB NOT NULL,          -- steps + triggers
    created_by  TEXT NOT NULL REFERENCES users(id),
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Pipeline Runs
CREATE TABLE pipeline_runs (
    id          TEXT PRIMARY KEY,        -- ULID
    pipeline_id TEXT NOT NULL REFERENCES pipelines(id) ON DELETE CASCADE,
    status      TEXT NOT NULL DEFAULT 'pending'
                CHECK (status IN ('pending', 'running', 'success', 'failed', 'cancelled')),
    triggered_by TEXT NOT NULL,          -- user ID or trigger description
    started_at  TIMESTAMPTZ,
    finished_at TIMESTAMPTZ,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_runs_pipeline ON pipeline_runs(pipeline_id, created_at DESC);

-- Pipeline Step Results
CREATE TABLE pipeline_step_results (
    id          TEXT PRIMARY KEY,        -- ULID
    run_id      TEXT NOT NULL REFERENCES pipeline_runs(id) ON DELETE CASCADE,
    step_id     TEXT NOT NULL,
    step_name   TEXT NOT NULL,
    status      TEXT NOT NULL
                CHECK (status IN ('pending', 'running', 'success', 'failed', 'skipped')),
    output      TEXT,                    -- captured stdout/stderr
    error       TEXT,
    duration_ms INTEGER,
    started_at  TIMESTAMPTZ,
    finished_at TIMESTAMPTZ
);
CREATE INDEX idx_step_results_run ON pipeline_step_results(run_id);

-- Audit Log (append-only)
CREATE TABLE audit_log (
    id          TEXT PRIMARY KEY,        -- ULID
    user_id     TEXT,
    action      TEXT NOT NULL,           -- e.g. "stack.deploy", "user.create"
    resource    TEXT NOT NULL,           -- e.g. "stacks/web-app"
    detail      JSONB,                   -- structured context
    ip_address  TEXT,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_audit_created ON audit_log(created_at DESC);

-- Settings (key-value config store)
CREATE TABLE settings (
    key         TEXT PRIMARY KEY,
    value       TEXT NOT NULL,
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
```

---

## 10. Testing Strategy (TDD)

### 10.1 Test Pyramid

```
         /  E2E Tests  \          -- Playwright: critical user flows
        / (< 10 tests)  \
       /------------------\
      / Integration Tests  \      -- Docker SDK, DB, real compose ops
     /   (50-100 tests)     \
    /------------------------\
   /     Unit Tests           \   -- Domain models, services, handlers
  /      (200+ tests)         \
 /------------------------------\
```

### 10.2 Unit Tests (TDD -- tests first, no external deps)

**Domain layer** (zero deps, pure logic):

- `stack/aggregate_test.go` -- stack creation, validation, state transitions, git source
- `stack/compose_test.go` -- compose parsing, validation
- `pipeline/aggregate_test.go` -- step ordering, DAG validation, trigger parsing
- `pipeline/run_test.go` -- execution state machine
- `auth/user_test.go` -- password hashing, role checks
- `auth/session_test.go` -- token generation, expiry
- `auth/apikey_test.go` -- key format, hashing

**Application services** (with mocked repositories):

- `stack_service_test.go` -- service orchestration, event publishing
- `pipeline_service_test.go` -- pipeline CRUD, run lifecycle
- `pipeline_executor_test.go` -- DAG execution, cancellation, timeouts
- `auth_service_test.go` -- login flow, bootstrap, API key management

**API handlers** (using humatest):

- `handler/stack_test.go` -- request/response validation, auth checks
- `handler/pipeline_test.go`
- `handler/auth_test.go` -- login, logout, bootstrap, RBAC
- `handler/webhook_test.go` -- signature validation, payload parsing

### 10.3 Integration Tests (`//go:build integration`)

Uses testcontainers-go to spin up real Postgres + Valkey containers.
No Docker daemon operations -- just DB and cache.

```go
//go:build integration
```

- `store/postgres/user_repo_test.go` -- real Postgres CRUD
- `store/postgres/stack_repo_test.go` -- real Postgres CRUD
- `store/postgres/migrations_test.go` -- goose migrations apply cleanly
- `cache/valkey_test.go` -- real Valkey get/set/pub/sub
- `git/client_test.go` -- real git clone/pull (against a temp bare repo)

### 10.4 E2E Smoke Tests (`//go:build e2e`)

Uses testcontainers-go (Postgres, Valkey, DinD modules) to run the
full application against a real Docker daemon. These are live tests.

```go
//go:build e2e
```

**Infrastructure:**

- testcontainers-go v0.41+ for container lifecycle
- `modules/postgres` -- real Postgres with init scripts
- `modules/valkey` -- real Valkey
- `modules/dind` -- isolated Docker daemon for compose operations
- `modules/compose` -- test compose up/down natively

**Test scenarios:**

```
e2e/
├── auth_test.go           -- bootstrap first user -> login -> RBAC enforcement
├── stack_crud_test.go     -- create local stack -> deploy -> verify containers
│                             running -> stop -> delete (against DinD)
├── stack_git_test.go      -- create git-backed stack -> clone -> deploy ->
│                             simulate webhook push -> verify auto-redeploy
├── compose_ops_test.go    -- deploy stack -> pull images -> restart ->
│                             verify container recreation (against DinD)
├── terminal_test.go       -- connect WebSocket -> exec into container ->
│                             send command -> receive output
├── pipeline_test.go       -- create pipeline -> run -> watch SSE output ->
│                             verify step results in DB
├── webhook_test.go        -- register webhook -> POST GitHub-style payload ->
│                             verify git pull + redeploy triggered
├── logs_sse_test.go       -- deploy stack -> connect SSE log stream ->
│                             verify log lines arrive in real-time
└── testdata/
    ├── compose-test.yaml  -- minimal compose for testing (nginx:alpine)
    ├── compose-multi.yaml -- multi-service stack for testing
    ├── init.sql           -- Postgres init script
    └── test-repo/         -- bare git repo for git-backed stack tests
```

**Build & run:**

```makefile
test:             go test ./...                                    # unit only
test-integration: go test -tags=integration -count=1 -timeout=5m ./...
test-e2e:         go test -tags=e2e -v -count=1 -timeout=15m ./e2e/...
test-all:         go test -tags="e2e,integration" -v -count=1 -timeout=20m ./...
```

---

## 11. Project Structure

```
composer/
├── cmd/
│   └── composerd/
│       └── main.go                    # Entry point, wires everything
│
├── internal/
│   ├── domain/                        # DDD domain layer (ZERO external deps)
│   │   ├── stack/
│   │   │   ├── aggregate.go           # Stack aggregate root
│   │   │   ├── aggregate_test.go      # TDD: test first
│   │   │   ├── compose.go            # ComposeFile value object, validation
│   │   │   ├── compose_test.go
│   │   │   ├── events.go             # Stack domain events
│   │   │   ├── repository.go         # StackRepository interface
│   │   │   └── status.go             # StackStatus enum
│   │   │
│   │   ├── container/
│   │   │   ├── entity.go             # Container entity
│   │   │   ├── entity_test.go
│   │   │   ├── stats.go              # ResourceStats value object
│   │   │   ├── log.go                # LogEntry value object
│   │   │   └── repository.go         # ContainerRepository interface
│   │   │
│   │   ├── pipeline/
│   │   │   ├── aggregate.go          # Pipeline aggregate root
│   │   │   ├── aggregate_test.go
│   │   │   ├── step.go               # Step entity + StepType configs
│   │   │   ├── step_test.go
│   │   │   ├── trigger.go            # Trigger value object
│   │   │   ├── run.go                # PipelineRun entity
│   │   │   ├── run_test.go
│   │   │   ├── events.go             # Pipeline domain events
│   │   │   └── repository.go         # PipelineRepository interface
│   │   │
│   │   ├── auth/
│   │   │   ├── user.go               # User aggregate
│   │   │   ├── user_test.go
│   │   │   ├── session.go            # Session entity
│   │   │   ├── session_test.go
│   │   │   ├── apikey.go             # APIKey entity
│   │   │   ├── apikey_test.go
│   │   │   ├── role.go               # Role enum + hierarchy
│   │   │   └── repository.go         # UserRepo, SessionRepo, APIKeyRepo
│   │   │
│   │   └── event/
│   │       ├── bus.go                 # EventBus interface
│   │       └── events.go             # All domain event types
│   │
│   ├── app/                           # Application services
│   │   ├── stack_service.go
│   │   ├── stack_service_test.go
│   │   ├── container_service.go
│   │   ├── container_service_test.go
│   │   ├── pipeline_service.go
│   │   ├── pipeline_service_test.go
│   │   ├── pipeline_executor.go       # DAG executor for pipeline runs
│   │   ├── pipeline_executor_test.go
│   │   ├── auth_service.go
│   │   ├── auth_service_test.go
│   │   └── terminal_service.go
│   │
│   ├── infra/                         # Infrastructure implementations
│   │   ├── docker/
│   │   │   ├── client.go             # Docker SDK wrapper
│   │   │   ├── client_test.go
│   │   │   ├── compose.go            # docker compose CLI wrapper
│   │   │   ├── compose_test.go
│   │   │   ├── events.go             # Docker event listener -> domain events
│   │   │   └── terminal.go           # Container exec + pty attach
│   │   │
│   │   ├── store/
│   │   │   ├── postgres/
│   │   │   │   ├── db.go             # pgxpool connection, migrations runner
│   │   │   │   ├── migrations.go     # Embedded SQL migrations (embed.FS)
│   │   │   │   ├── user_repo.go      # UserRepository impl
│   │   │   │   ├── session_repo.go   # SessionRepository impl
│   │   │   │   ├── apikey_repo.go    # APIKeyRepository impl
│   │   │   │   ├── stack_repo.go     # StackRepository impl
│   │   │   │   ├── webhook_repo.go   # WebhookRepository impl
│   │   │   │   ├── pipeline_repo.go  # PipelineRepository impl
│   │   │   │   └── audit_repo.go     # AuditLogRepository impl
│   │   │   └── migrations/
│   │   │       ├── 001_initial.up.sql
│   │   │       └── 001_initial.down.sql
│   │   │
│   │   ├── cache/
│   │   │   ├── valkey.go             # Valkey client wrapper
│   │   │   └── valkey_test.go
│   │   │
│   │   ├── git/
│   │   │   ├── client.go             # go-git wrapper (clone, pull, log, diff, commit, push)
│   │   │   ├── client_test.go
│   │   │   └── webhook.go            # Webhook signature validation (GitHub/GitLab/Gitea/etc.)
│   │   │
│   │   ├── fs/
│   │   │   ├── compose_store.go      # Read/write compose files on disk
│   │   │   └── compose_store_test.go
│   │   │
│   │   └── eventbus/
│   │       ├── memory.go             # In-process event bus impl
│   │       └── memory_test.go
│   │
│   └── api/                           # HTTP API layer
│       ├── server.go                  # Huma API setup, route registration
│       ├── middleware/
│       │   ├── auth.go               # Session + API key auth middleware
│       │   ├── auth_test.go
│       │   ├── ratelimit.go          # Per-IP token bucket
│       │   ├── ratelimit_test.go
│       │   ├── security.go           # CSRF, security headers
│       │   └── logging.go            # Structured request logging
│       │
│       ├── handler/
│       │   ├── auth.go               # Login, logout, bootstrap, session
│       │   ├── auth_test.go
│       │   ├── user.go               # User CRUD
│       │   ├── stack.go              # Stack CRUD + operations
│       │   ├── stack_test.go
│       │   ├── container.go          # Container endpoints
│       │   ├── pipeline.go           # Pipeline CRUD + runs
│       │   ├── pipeline_test.go
│       │   ├── system.go             # Health, info, version
│       │   └── sse.go                # SSE streaming endpoints
│       │
│       ├── ws/
│       │   ├── terminal.go           # WebSocket terminal handler
│       │   └── terminal_test.go
│       │
│       └── dto/                       # Request/response types (Huma schemas)
│           ├── auth.go
│           ├── stack.go
│           ├── container.go
│           ├── pipeline.go
│           └── system.go
│
├── web/                               # Astro frontend (separate build)
│   ├── astro.config.mjs
│   ├── tsconfig.json
│   ├── package.json
│   ├── bun.lock
│   │
│   ├── src/
│   │   ├── layouts/
│   │   │   ├── Layout.astro           # Base layout (nav, sidebar)
│   │   │   └── AuthLayout.astro       # Layout for login/bootstrap pages
│   │   │
│   │   ├── pages/
│   │   │   ├── index.astro            # Dashboard (stack overview)
│   │   │   ├── login.astro            # Login page
│   │   │   ├── setup.astro            # Bootstrap/first-run wizard
│   │   │   ├── stacks/
│   │   │   │   ├── index.astro        # Stack list
│   │   │   │   ├── [name].astro       # Stack detail
│   │   │   │   └── new.astro          # Create stack
│   │   │   ├── pipelines/
│   │   │   │   ├── index.astro        # Pipeline list
│   │   │   │   ├── [id].astro         # Pipeline detail + runs
│   │   │   │   └── new.astro          # Create pipeline
│   │   │   └── settings/
│   │   │       ├── index.astro        # Settings overview
│   │   │       ├── users.astro        # User management
│   │   │       └── keys.astro         # API key management
│   │   │
│   │   ├── components/
│   │   │   ├── ui/                    # Shadcn/ui components (copy-pasted)
│   │   │   │   ├── button.tsx
│   │   │   │   ├── card.tsx
│   │   │   │   ├── badge.tsx
│   │   │   │   ├── dialog.tsx
│   │   │   │   ├── input.tsx
│   │   │   │   ├── select.tsx
│   │   │   │   ├── tabs.tsx
│   │   │   │   ├── toast.tsx
│   │   │   │   ├── command.tsx        # Command palette (Cmd+K)
│   │   │   │   └── ...
│   │   │   │
│   │   │   ├── stack/
│   │   │   │   ├── StackList.tsx      # React island: stack sidebar list
│   │   │   │   ├── StackDetail.tsx    # React island: full stack view
│   │   │   │   ├── StackEditor.tsx    # React island: CodeMirror compose editor
│   │   │   │   ├── StackActions.tsx   # Deploy/stop/restart/pull buttons
│   │   │   │   └── StackStatus.tsx    # Status badge with SSE updates
│   │   │   │
│   │   │   ├── container/
│   │   │   │   ├── ContainerCard.tsx  # Container info card
│   │   │   │   ├── ContainerLogs.tsx  # React island: live log viewer (SSE)
│   │   │   │   └── ContainerStats.tsx # React island: CPU/mem charts (SSE)
│   │   │   │
│   │   │   ├── terminal/
│   │   │   │   └── Terminal.tsx       # React island: xterm.js WebSocket terminal
│   │   │   │
│   │   │   ├── pipeline/
│   │   │   │   ├── PipelineEditor.tsx # Pipeline YAML/visual editor
│   │   │   │   ├── PipelineRunner.tsx # Live run viewer (SSE)
│   │   │   │   └── RunHistory.tsx     # Past run list
│   │   │   │
│   │   │   └── layout/
│   │   │       ├── Sidebar.tsx        # React island: app sidebar
│   │   │       ├── Header.tsx         # Top navigation
│   │   │       └── CommandPalette.tsx # Cmd+K search/actions
│   │   │
│   │   ├── lib/
│   │   │   ├── api/
│   │   │   │   ├── client.ts          # Generated from OpenAPI (openapi-fetch)
│   │   │   │   └── types.ts           # Generated from OpenAPI (openapi-typescript)
│   │   │   ├── sse.ts                 # SSE client helper (EventSource wrapper)
│   │   │   ├── ws.ts                  # WebSocket client (terminal only)
│   │   │   ├── auth.ts               # Auth state management
│   │   │   └── utils.ts
│   │   │
│   │   └── styles/
│   │       └── globals.css            # Tailwind base + shadcn theme
│   │
│   └── public/
│       ├── favicon.svg
│       └── logo.svg
│
│
├── e2e/                               # E2E smoke tests (//go:build e2e)
│   ├── auth_test.go
│   ├── stack_crud_test.go
│   ├── stack_git_test.go
│   ├── compose_ops_test.go
│   ├── terminal_test.go
│   ├── pipeline_test.go
│   ├── webhook_test.go
│   ├── logs_sse_test.go
│   └── testdata/
│       ├── compose-test.yaml
│       ├── compose-multi.yaml
│       ├── init.sql
│       └── test-repo/                 # Bare git repo fixture
│
├── scripts/
│   ├── generate-client.sh             # OpenAPI -> TypeScript client
│   ├── hash-password.go               # CLI bcrypt hash utility
│   └── dev.sh                         # Start both Go + Astro dev servers
│
├── deploy/
│   ├── Dockerfile                     # Multi-stage: build Go + Astro -> single image
│   ├── Dockerfile.dev                 # Dev image with hot-reload
│   └── compose.yaml                   # Self-hosting compose (composer + postgres + valkey)
│
├── Makefile                           # build, test, lint, dev, generate, docker
├── go.mod
├── go.sum
├── .air.toml                          # Go hot-reload config
├── .golangci.yml                      # Linter config
├── ARCHITECTURE.md                    # This file
└── .github/
    └── workflows/
        ├── ci.yml                     # Test + lint + build on PR
        └── release.yml                # Build + push Docker image on tag
```

---

## 12. Frontend Architecture

### 12.1 Astro + React Islands

Astro renders pages server-side for fast initial load. Interactive components
are React islands that hydrate on the client (`client:load`). This gives us:

- Fast TTFB (server-rendered HTML)
- Interactive where needed (editors, terminals, live data)
- Small JS bundle (only islands are shipped as JS)

Same architecture as gatekeeper and gloryhole dashboards.

### 12.2 Design System: "Lovelace" (shared with gatekeeper/gloryhole)

Dark-only, warm charcoal base with pastel-neon accents. Consistent with
the project's design system.

#### Color Palette (Tailwind v4 `@theme` directive in `globals.css`)

**Base (warm charcoal, never pure gray):**
| Token | Hex | Usage |
|-------|-----|-------|
| `cp-950` | `#15161e` | Sidebar, deepest surface |
| `cp-900` | `#1d1f28` | Page background |
| `cp-800` | `#282a36` | Card/popover surfaces |
| `cp-700` | `#343647` | Muted surfaces, secondary borders |
| `cp-600` | `#414457` | Input borders, grid lines |

**Accents (pastel-neon):**
| Token | Hex | Semantic |
|-------|-----|----------|
| `cp-purple` | `#c574dd` | Primary brand, active nav, focus rings |
| `cp-green` / `cp-green-bright` | `#5adecd` / `#17e2c7` | Success, healthy, running |
| `cp-red` / `cp-red-bright` | `#f37e96` / `#ff4870` | Destructive, errors, exited |
| `cp-peach` / `cp-peach-bright` | `#f1a171` / `#ff8037` | Warnings, partial state |
| `cp-blue` / `cp-blue-bright` | `#8796f4` / `#546eff` | Info, secondary actions |
| `cp-cyan` / `cp-cyan-bright` | `#79e6f3` / `#3edced` | CPU metrics, system |
| `cp-yellow` | `#ffd866` | Latency, timing |

**Foreground**: `#e0e0e0` (off-white), muted: `#bdbdc1`

#### Typography

- **Body/headings**: `Space Grotesk` (self-hosted via `@fontsource`, 400-700)
- **Data/monospace**: `JetBrains Mono` (via `@fontsource`, `.font-data` utility)
- **Typography constants** in `src/lib/typography.ts` (imported as `T`):
  - Page titles: `text-lg font-semibold`
  - Card titles: `text-sm`
  - Stat values: `text-2xl font-bold tabular-nums font-data`
  - Labels: `text-xs font-medium uppercase tracking-wider text-muted-foreground`
  - Table cells: `text-xs` with `.font-data` for numeric data

#### Visual Effects

- `.glow-purple` / `.glow-green`: multi-layer `text-shadow` neon glow (logo)
- `animate-fade-in-up`: staggered card entrance (translateY 8px + opacity, 0.4s)
- `active:scale-[0.97]`: micro-press feedback on all buttons and tabs
- Custom scrollbar: 6px thin, charcoal track, purple hover thumb
- Button hover: `hover:shadow-lg hover:shadow-primary/20` (purple glow)

#### Tailwind v4 Configuration

No `tailwind.config.*` file. All theme config via CSS `@theme` directive:

```css
@import "tailwindcss";
@theme {
  --color-cp-950: #15161e;
  --color-cp-900: #1d1f28;
  /* ... full palette ... */
  --color-background: #1d1f28;
  --color-foreground: #e0e0e0;
  --color-primary: #c574dd;
  --color-card: #282a36;
  --font-sans: "Space Grotesk", system-ui, sans-serif;
  --font-mono: "JetBrains Mono", "Fira Code", monospace;
  --radius: 0.5rem;
}
```

#### shadcn/ui Components (customized)

Standard shadcn pattern: Radix primitives + CVA + `cn()` utility.
Customizations matching gatekeeper/gloryhole:

- `button.tsx`: `xs` + `icon-sm` sizes, `active:scale-[0.97]`, purple glow shadow
- `card.tsx`: `rounded-xl` (not default `rounded-lg`)
- `select.tsx`: `active:scale-[0.98]`, `hover:bg-accent/50`
- `tabs.tsx`: `active:scale-[0.97]`, active state with shadow
- All use CSS variable tokens, accent colors applied at call site

#### Layout Pattern

```
+------+--------------------------------------------------+
| 60px |                   Header (h-14)                   |
| wide |  [hamburger]   Page Title        [health dot]     |
|      +--------------------------------------------------+
|      |                                                    |
| Side |              Main Content (p-6)                    |
| bar  |              overflow-y-auto                       |
| cp-  |                                                    |
| 950  |  Stack list     Stack detail / Editor / Terminal   |
|      |  (left col)     (main area, responsive grid)       |
|      |                                                    |
|      |                         [scroll-to-top FAB]        |
+------+--------------------------------------------------+
```

- Sidebar: `w-60`, `bg-cp-950`, collapsible nav sections, stack status list
- Header: `h-14`, `bg-cp-950/50` translucent, health indicator with ping animation
- Active nav: `bg-cp-purple/10 text-cp-purple`
- Mobile: hamburger toggle, sidebar as fullscreen overlay
- Logo: "COMPOSER" in `glow-purple` text effect

### 12.3 Data Flow

```
Astro Page (SSR)
  |-- fetches initial data via REST API on server
  |-- renders HTML shell with Astro components
  |-- passes initial data as props to React islands
  |
  React Island (client:load hydration)
    |-- receives initial data as props
    |-- establishes SSE connection for live updates
    |-- manages local state with React hooks
    |-- calls REST API for mutations (via generated typed client)
    |-- receives SSE events to update state in real-time
```

### 12.4 Generated API Client

```bash
# In CI or dev workflow:
# 1. Go server starts and generates openapi.json
# 2. openapi-typescript generates types
# 3. openapi-fetch creates a typed client

bunx openapi-typescript http://localhost:8080/openapi.json -o src/lib/api/types.ts
```

Usage in frontend:

```typescript
import createClient from "openapi-fetch";
import type { paths } from "./api/types";

const api = createClient<paths>({ baseUrl: "/api/v1" });

// Fully typed -- IDE autocomplete + compile-time errors
const { data, error } = await api.GET("/stacks/{name}", {
  params: { path: { name: "web-app" } },
});
// data is typed as StackDetailResponse
```

---

## 13. Deployment

### 13.1 Docker (primary)

```yaml
# deploy/compose.yaml
services:
  composer:
    image: ghcr.io/erfianugrah/composer:latest
    ports:
      - "8080:8080"
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock:ro
      - /opt/stacks:/stacks
      - composer_ssh:/home/composer/.ssh # SSH keys for git
    environment:
      - COMPOSER_STACKS_DIR=/stacks
      - COMPOSER_PORT=8080
      - COMPOSER_DB_URL=postgres://composer:composer@postgres:5432/composer?sslmode=disable
      - COMPOSER_VALKEY_URL=valkey://valkey:6379
    depends_on:
      postgres:
        condition: service_healthy
      valkey:
        condition: service_started

  postgres:
    image: postgres:17-alpine
    volumes:
      - postgres_data:/var/lib/postgresql/data
    environment:
      - POSTGRES_USER=composer
      - POSTGRES_PASSWORD=composer
      - POSTGRES_DB=composer
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U composer"]
      interval: 5s
      timeout: 3s
      retries: 5

  valkey:
    image: valkey/valkey:8-alpine
    volumes:
      - valkey_data:/data

volumes:
  postgres_data:
  valkey_data:
  composer_ssh:
```

### 13.2 Single Binary

```bash
# Build
make build

# Run (requires external Postgres + optionally Valkey)
./composerd \
  --stacks-dir=/opt/stacks \
  --db-url=postgres://user:pass@localhost:5432/composer \
  --valkey-url=valkey://localhost:6379 \
  --port=8080
```

### 13.3 Resource Budget

- **Idle RAM**: < 50MB (vs Dockge ~120MB, Portainer ~150MB)
- **Docker image**: ~180MB (alpine + Go binary + bundled docker CLI + compose + buildx + git)
- **Go binary**: ~8MB (stripped, pure Go, no CGO)
- **Startup time**: < 1 second
- **Host requirements**: Container runtime with socket only (no docker-cli, compose, git on host)

---

## 14. Development Workflow

### 14.1 Setup

```bash
# Prerequisites: Go 1.25+, bun 1.2+, Docker

git clone https://github.com/erfianugrah/composer
cd composer

# Install Go deps
go mod download

# Install frontend deps
cd web && bun install && cd ..

# Start development (both servers with hot-reload)
make dev
```

### 14.2 Makefile Targets

```makefile
dev          # Start Go (air hot-reload) + Astro dev server concurrently
build        # Build Go binary + Astro static assets
test         # Run all Go tests
test-unit    # Run domain + app layer tests only
test-int     # Run integration tests (needs Docker + Valkey)
lint         # Run golangci-lint + eslint
generate     # Generate OpenAPI spec -> TypeScript client
docker       # Build Docker image
docker-dev   # Build dev Docker image with hot-reload
clean        # Remove build artifacts
```

### 14.3 TDD Cycle

```
1. Write failing test in internal/domain/stack/aggregate_test.go
2. Run: make test-unit (RED)
3. Implement in internal/domain/stack/aggregate.go
4. Run: make test-unit (GREEN)
5. Refactor if needed
6. Repeat
```

---

## 15. Implementation Phases

### Phase 1: Foundation (MVP)

- [x] Go module setup + project structure
- [x] Domain models: Stack, Container, Auth (with TDD tests)
- [x] Auth system: User, Session, RBAC, bootstrap flow
- [x] Postgres storage (pgx + goose migrations)
- [x] Docker SDK client: list, inspect, logs, stats, exec
- [x] Compose CLI wrapper: up, down, restart, pull, validate
- [x] Huma REST API: stacks CRUD + operations + auth
- [x] SSE: domain events stream + container log streaming
- [x] WebSocket terminal (backend handler)
- [x] Astro frontend: login, dashboard, stack list, stack detail
- [x] Playwright E2E browser tests (6 tests)
- [x] RBAC enforcement in all handlers (viewer/operator)
- [x] In-process event bus with pub/sub
- [~] Docker events listener (SDK method exists, goroutine not wired)
- [x] Frontend: xterm.js terminal component
- [x] Frontend: CodeMirror compose editor
- [x] Frontend: OpenAPI client (openapi-fetch)
- [x] User management handler (/api/v1/users CRUD)
- [x] API key management handler (/api/v1/keys)
- [x] Container individual endpoints (/api/v1/containers)
- [x] Rate limiting, security headers middleware
- [x] Embedded frontend in Go binary (embed.FS)
- [x] Makefile + Dockerfile + GHCR CI

### Phase 2: Git & Webhooks -- COMPLETE

- [x] Git-backed stacks (go-git: clone, pull, log, diff)
- [x] Webhook receiver (GitHub/GitLab/Gitea signature validation)
- [x] GitOps flow: webhook -> pull -> diff -> auto-redeploy
- [x] Git API endpoints (sync, log, status)
- [x] Webhook CRUD API + management UI
- [x] Git history viewer in frontend
- [x] API key management REST endpoints

### Phase 3: Pipelines & CI -- COMPLETE

- [x] Domain models: Pipeline, Step, Run (with TDD tests)
- [x] Pipeline DAG executor (concurrent steps, cancellation, continue-on-error)
- [x] Pipeline REST API (7 endpoints)
- [x] Pipeline repository (JSONB config storage)
- [x] Pipeline service (async execution)
- [x] Pipeline frontend UI (list, run, history)
- [ ] Webhook triggers for pipelines
- [ ] Schedule triggers (cron)

### Phase 4: Polish -- PARTIAL

- [ ] Valkey integration (session cache, event pub/sub)
- [x] Container stats streaming (CPU/mem/net/disk SSE)
- [x] Command palette (Cmd+K)
- [x] Audit log (middleware + repository)
- [ ] OpenAPI TypeScript client generation in CI
- [ ] E2E smoke tests (testcontainers-go)
- [ ] Documentation site

### Phase 5: Advanced

- [ ] Multi-host agent support (like Dockge's agents)
- [ ] Podman auto-detection + documentation
- [ ] Image build support (Dockerfile in stack)
- [ ] Notifications (email, webhook, Slack)
- [ ] Stack templates / marketplace
- [ ] Compose file diff viewer
- [ ] File watcher trigger for pipelines
- [ ] OAuth/OIDC via goth (Google, GitHub, etc.)

---

## 16. Infrastructure Details

### 16.1 Logging (zap)

Structured logging via `go.uber.org/zap`. JSON in production, console in dev.

```go
// Production: JSON, info level, sampling
logger, _ := zap.NewProduction()

// Development: console, debug level, stacktraces on warn+
logger, _ := zap.NewDevelopment()
```

- All application services receive `*zap.Logger` via constructor injection
- HTTP middleware logs request/response: method, path, status, duration, request_id
- Docker operations log: stack name, operation, duration, error
- Sensitive fields (passwords, tokens) are NEVER logged
- Child loggers via `logger.With(zap.String("stack", name))` for context

### 16.2 Configuration (humacli + env vars)

Huma's built-in CLI handles flags and env vars. Single `Options` struct.

```go
type Options struct {
    Port         int    `help:"HTTP port" short:"p" default:"8080"`
    DBUrl        string `help:"Postgres connection URL" default:"postgres://composer:composer@localhost:5432/composer?sslmode=disable"`
    ValkeyURL    string `help:"Valkey connection URL" default:"valkey://localhost:6379"`
    StacksDir    string `help:"Directory for compose stacks" default:"/opt/stacks"`
    DataDir      string `help:"Directory for app data (SSH keys, etc.)" default:"/opt/composer"`
    DockerHost   string `help:"Docker/Podman socket" default:""`  // auto-detect if empty
    LogLevel     string `help:"Log level (debug|info|warn|error)" default:"info"`
    LogFormat    string `help:"Log format (json|console)" default:"json"`
}
```

Set via CLI flags (`--port=8080`), env vars (`COMPOSER_PORT=8080`), or both.

### 16.3 Graceful Shutdown

```go
// In cmd/composerd/main.go
hooks.OnStart(func() {
    srv := &http.Server{Addr: fmt.Sprintf(":%d", opts.Port), Handler: router}
    go srv.ListenAndServe()

    // Wait for interrupt
    quit := make(chan os.Signal, 1)
    signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
    <-quit

    // Graceful shutdown with 30s timeout
    ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
    defer cancel()

    // Close in order: HTTP server, WebSocket connections, SSE streams,
    // pipeline runners, Docker event listeners, DB pool, Valkey
    srv.Shutdown(ctx)
    terminalHub.CloseAll()
    sseBroker.Close()
    pipelineExecutor.CancelAll()
    dockerEvents.Stop()
    dbPool.Close()
    valkeyClient.Close()
})
```

### 16.4 Docker/Podman Compatibility

Auto-detect container runtime. Supports Docker and Podman transparently.

```go
func detectSocket() string {
    // 1. Explicit DOCKER_HOST or COMPOSER_DOCKER_HOST env var
    if host := os.Getenv("COMPOSER_DOCKER_HOST"); host != "" { return host }
    if host := os.Getenv("DOCKER_HOST"); host != "" { return host }

    // 2. Docker socket (default)
    if _, err := os.Stat("/var/run/docker.sock"); err == nil {
        return "unix:///var/run/docker.sock"
    }
    // 3. Rootful Podman
    if _, err := os.Stat("/run/podman/podman.sock"); err == nil {
        return "unix:///run/podman/podman.sock"
    }
    // 4. Rootless Podman
    if xdg := os.Getenv("XDG_RUNTIME_DIR"); xdg != "" {
        sock := filepath.Join(xdg, "podman", "podman.sock")
        if _, err := os.Stat(sock); err == nil { return "unix://" + sock }
    }
    return client.DefaultDockerHost
}
```

**Always create client with `WithAPIVersionNegotiation()`** for Podman compat.
Pass detected `DOCKER_HOST` to `docker compose` subprocess.

### 16.5 Compose Stack Features

**Multi-file compose support:**

- `compose.yaml` (primary)
- `compose.override.yaml` (auto-detected, merged)
- Custom files via stack config: `composeFiles: ["compose.yaml", "compose.prod.yaml"]`

**.env file support:**

- `.env` files alongside compose.yaml are auto-loaded by `docker compose`
- Composer reads `.env` for display in UI (environment variable editor)
- Secrets in `.env` are masked in UI and API responses

**Volume management:**

- On stack delete: optionally remove named volumes (`--volumes` flag)
- Volume listing per stack via Docker API

### 16.6 CORS (Development)

In dev mode (Astro on :4321, Go on :8080), CORS middleware allows cross-origin:

```go
if isDev {
    router.Use(cors.Handler(cors.Options{
        AllowedOrigins:   []string{"http://localhost:4321"},
        AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
        AllowedHeaders:   []string{"Authorization", "Content-Type", "X-Requested-With", "X-API-Key"},
        AllowCredentials: true,
    }))
}
```

In production, the Go binary serves the embedded Astro assets (same origin, no CORS needed).

### 16.7 Request ID / Tracing

Every request gets a unique ID via middleware:

```
X-Request-ID: 01JRZXYZ... (ULID)
```

- Generated if not present in inbound request
- Propagated to all log entries via `zap.String("request_id", id)`
- Returned in response headers
- Passed to Docker operations for correlation

### 16.8 Credential Encryption

Git credentials stored in `stack_git_configs.credentials` are encrypted at rest:

- AES-256-GCM with a server-side encryption key
- Key derived from `COMPOSER_ENCRYPTION_KEY` env var (or auto-generated on first run, stored in data dir)
- Credentials decrypted in-memory only when performing git operations
- If encryption key is lost, git credentials must be re-entered

### 16.9 Database Migrations (goose v3)

```go
//go:embed migrations/*.sql
var migrations embed.FS

provider, err := goose.NewProvider(
    goose.DialectPostgres,
    sqlDB,          // *sql.DB via pgx stdlib adapter (short-lived, for migrations only)
    migrations,
    goose.WithSessionLocker(goose.NewSessionLocker(...)), // advisory lock
)
results, err := provider.Up(ctx)
```

Migrations run automatically on startup. Advisory lock prevents concurrent migration
from multiple instances. Main app uses `pgxpool` directly (not `database/sql`).

### 16.10 Health Checks

```
GET /api/v1/system/health  (public, no auth)
```

Response:

```json
{
  "status": "healthy",
  "checks": {
    "postgres": { "status": "up", "latency_ms": 2 },
    "valkey": { "status": "up", "latency_ms": 1 },
    "docker": { "status": "up", "runtime": "docker", "api_version": "1.54" }
  },
  "version": "0.1.0",
  "uptime_seconds": 3600
}
```

---

## 17. CI/CD & GHCR

All builds via GitHub Actions. Docker images pushed to GHCR (`ghcr.io/erfianugrah/composer`).

### 17.1 CI Workflow (`.github/workflows/ci.yml`)

Triggers: push to `main`, all PRs.

```yaml
jobs:
  lint:
    - golangci-lint (Go)
    - eslint + tsc --noEmit (frontend)

  test-unit:
    - go test ./internal/domain/... ./internal/app/...
    - No Docker daemon needed

  test-integration:
    - go test ./internal/infra/... -tags=integration
    - Uses Docker-in-Docker service
    - Uses Valkey service container

  build-check:
    - go build ./cmd/composerd/
      - cd web && bun run build
    - Verify both compile cleanly

  openapi-check:
    - Start composerd, fetch /openapi.json
    - Run openapi-typescript to generate types
    - tsc --noEmit to verify types still compile
    - Catches API drift between backend and frontend
```

### 17.2 Release Workflow (`.github/workflows/release.yml`)

Triggers: push tag `v*`.

```yaml
jobs:
  build-and-push:
    strategy:
      matrix:
        platform: [linux/amd64, linux/arm64]

    steps:
      - Build Astro frontend (bun run build)
      - Build Go binary (CGO_ENABLED=0, embed frontend assets)
      - Multi-arch Docker image via docker/build-push-action
      - Push to ghcr.io/erfianugrah/composer:latest
      - Push to ghcr.io/erfianugrah/composer:$TAG
      - Create GitHub Release with binary artifacts
```

### 17.3 Dockerfile (multi-stage, self-contained)

The image bundles ALL required binaries. No assumption that the host has
Docker CLI, compose, git, or any tools installed. The host only needs a
container runtime (Docker or Podman) with a socket.

```dockerfile
# Stage 1: Build frontend
FROM oven/bun:1-alpine AS frontend
WORKDIR /app/web
COPY web/package.json web/bun.lock ./
RUN bun install --frozen-lockfile
COPY web/ ./
RUN bun run build  # -> dist/

# Stage 2: Build Go binary
FROM golang:1.25-alpine AS backend
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=frontend /app/web/dist ./web/dist
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /composerd ./cmd/composerd/

# Stage 3: Extract docker CLI + compose plugin from official Docker image
FROM docker:28-cli AS docker-bins
# docker CLI:    /usr/local/bin/docker
# compose plugin: /usr/local/libexec/docker/cli-plugins/docker-compose
# buildx plugin:  /usr/local/libexec/docker/cli-plugins/docker-buildx

# Stage 4: Runtime (fully self-contained)
FROM alpine:3.21

# Install only git + SSH + certs (small, no docker packages from apk)
RUN apk add --no-cache git openssh-client ca-certificates tzdata

# Bundle docker CLI + compose plugin from the official Docker image
COPY --from=docker-bins /usr/local/bin/docker /usr/local/bin/docker
COPY --from=docker-bins /usr/local/libexec/docker/cli-plugins/docker-compose \
     /usr/local/libexec/docker/cli-plugins/docker-compose
COPY --from=docker-bins /usr/local/libexec/docker/cli-plugins/docker-buildx \
     /usr/local/libexec/docker/cli-plugins/docker-buildx

# Composer binary
COPY --from=backend /composerd /usr/local/bin/composerd

# Non-root user
RUN addgroup -S composer && adduser -S composer -G composer
USER composer

EXPOSE 8080
ENTRYPOINT ["composerd"]
```

**What's bundled in the image (no host dependencies):**

- `composerd` (~8MB) -- our Go binary
- `docker` CLI (~30MB) -- from official `docker:28-cli` image
- `docker-compose` plugin (~55MB) -- from official `docker:28-cli` image
- `docker-buildx` plugin (~65MB) -- for future image build support
- `git` + `openssh-client` (~15MB) -- for git-backed stacks + SSH key auth
- `ca-certificates` -- for HTTPS git remotes

**The host only needs:**

- A container runtime (Docker or Podman) with a socket
- The socket mounted into the container (`/var/run/docker.sock`)

Target image size: ~180MB (dominated by docker CLI + compose + buildx binaries).

### 17.4 OpenAPI Client Generation in CI

```yaml
# Part of CI -- runs on every PR to catch API drift
generate-client:
  steps:
    - Start composerd in background (test mode, ephemeral Postgres via testcontainers)
    - Wait for /api/v1/system/health
    - bunx openapi-typescript http://localhost:8080/openapi.json \
      -o web/src/lib/api/types.ts
    - cd web && bunx tsc --noEmit
    - If types changed, commit and push (bot commit)
```

---

## 18. Verified Dependency Versions

All dependencies verified as of April 2026.

| Package            | Version   | Import Path                                   | Notes                                                               |
| ------------------ | --------- | --------------------------------------------- | ------------------------------------------------------------------- |
| **Go (minimum)**   | **1.25+** | --                                            | Required by huma, pgx, goose, testcontainers                        |
| Huma v2            | v2.37.3   | `github.com/danielgtaylor/huma/v2`            | Auto OpenAPI 3.1, SSE, chi adapter                                  |
| Chi                | v5.2.5    | `github.com/go-chi/chi/v5`                    | Via huma adapter                                                    |
| pgx                | v5.9.1    | `github.com/jackc/pgx/v5`                     | Postgres driver + pgxpool                                           |
| goose              | v3.27.0   | `github.com/pressly/goose/v3`                 | Migrations via embedded SQL + Provider API                          |
| zap                | v1.27+    | `go.uber.org/zap`                             | Structured logging, JSON + console                                  |
| Docker Client      | v0.4.0    | `github.com/moby/moby/client`                 | New canonical path. Podman compat via `WithAPIVersionNegotiation()` |
| Docker API Types   | v1.54.1   | `github.com/moby/moby/api`                    | Separate module                                                     |
| Valkey             | v1.0.73   | `github.com/valkey-io/valkey-go`              | Official client                                                     |
| WebSocket          | v1.8.14   | `github.com/coder/websocket`                  | Was nhooyr.io, transferred to Coder                                 |
| go-git             | v5.17.2   | `github.com/go-git/go-git/v5`                 | Pure Go, SSH+HTTPS auth                                             |
| ULID               | v2.1+     | `github.com/oklog/ulid/v2`                    | Sortable unique IDs                                                 |
| bcrypt             | --        | `golang.org/x/crypto/bcrypt`                  | Password hashing, cost 12                                           |
| CORS               | --        | `github.com/go-chi/cors`                      | Dev-mode cross-origin                                               |
| testcontainers-go  | v0.41.0   | `github.com/testcontainers/testcontainers-go` | E2E: postgres, valkey, dind, compose modules                        |
| testify            | --        | `github.com/stretchr/testify`                 | Assertions + require                                                |
| **Frontend**       |           |                                               |                                                                     |
| Astro              | v6.1.4    | npm: `astro`                                  | Static output mode for embedding                                    |
| React              | v19       | npm: `react`                                  | Via @astrojs/react v5                                               |
| Tailwind CSS       | v4        | npm: `@tailwindcss/vite`                      | Vite plugin, @theme CSS config                                      |
| Shadcn/ui          | v4.2.0    | CLI: `shadcn@4.2.0`                           | Official Astro support, `shadcn apply` command                      |
| openapi-typescript | v7        | npm: `openapi-typescript`                     | OpenAPI 3.1 -> TypeScript types                                     |
| openapi-fetch      | latest    | npm: `openapi-fetch`                          | 6kb typed fetch client                                              |
| xterm.js           | v5+       | npm: `@xterm/xterm`                           | Terminal emulator                                                   |
| CodeMirror         | v6        | npm: `@codemirror/lang-yaml`                  | YAML editor                                                         |
| Recharts           | v2.15+    | npm: `recharts`                               | Container stats charts                                              |
