# Architecture

Composer follows Domain-Driven Design with clean architecture layering.

## Overview

```
┌──────────────────────────────────────┐
│         Astro Frontend               │
│  (React islands, Shadcn/ui,         │
│   Lovelace dark theme)              │
├──────────┬───────────┬───────────────┤
│  REST    │  SSE      │  WebSocket    │
│  (CRUD)  │ (streams) │  (terminal)   │
├──────────┴───────────┴───────────────┤
│       Huma v2 + Chi (Go)             │
│  ┌─────────────────────────────────┐ │
│  │ API Layer (handlers, middleware)│ │
│  ├─────────────────────────────────┤ │
│  │ Application Services            │ │
│  │ (AuthService, StackService)     │ │
│  ├─────────────────────────────────┤ │
│  │ Domain Layer (zero deps)        │ │
│  │ (Stack, User, Session, Events)  │ │
│  ├─────────────────────────────────┤ │
│  │ Infrastructure                  │ │
│  │ (Docker SDK, Postgres, EventBus)│ │
│  └─────────────────────────────────┘ │
├──────────────────────────────────────┤
│  Docker/Podman + PostgreSQL          │
└──────────────────────────────────────┘
```

## Layers

### Domain (internal/domain/)

Pure business logic with zero external dependencies. Contains:
- **auth/** -- User aggregate, Session, APIKey, Role hierarchy, repository interfaces
- **stack/** -- Stack aggregate with git source support, compose content, status tracking
- **container/** -- Container entity with status and health enums
- **pipeline/** -- Pipeline aggregate, Step, Run, DAG validation, topological ordering
- **dag/** -- Kahn's-algorithm wave ordering + cycle detection shared by pipeline steps and stack batch deploys
- **event/** -- Event bus interface + all domain event types (stack, container, pipeline)

The domain layer imports only the Go standard library (+ `golang.org/x/crypto/bcrypt`).

### Application (internal/app/)

Orchestrates domain objects and infrastructure:
- **AuthService** -- Bootstrap, login/logout, session validation, API key management
- **StackService** -- CRUD, deploy/stop/restart/pull, dependency-ordered batch deploy, event publishing, git-stack dirty detection
- **GitService** -- Git-backed stack creation, sync, sync+redeploy (GitOps flow)
- **PipelineService** -- Pipeline CRUD, async run execution
- **PipelineExecutor** -- DAG step executor with concurrency, timeouts, cancellation
- **JobManager** -- In-memory background job tracking (create/start/complete/fail lifecycle, periodic cleanup)

### Infrastructure (internal/infra/)

Implements domain interfaces with real technology:
- **docker/** -- Docker Engine SDK client (container ops) + compose CLI wrapper + event listener
- **store/** -- database/sql repository implementations supporting both PostgreSQL and SQLite (users, sessions, keys, stacks, git configs, webhooks, pipelines, runs, audit)
- **crypto/** -- AES-256-GCM encryption for credentials, secrets, and SSH key files at rest (auto-encrypts on startup, transparent decrypt for go-git)
- **eventbus/** -- In-memory pub/sub event bus
- **git/** -- go-git wrapper (clone, pull, log, diff, commit, push) + webhook signature validation (GitHub, GitLab, Gitea)
- **sops/** -- SOPS-encrypted file detection and decryption via bundled `sops` binary + age key management (per-stack and global)
- **cache/** -- Valkey client for session and API key caching
- **notify/** -- Notification dispatcher (webhook, Slack) [Phase 5]

### API (internal/api/)

HTTP layer translating between the web and application services:
- **server.go** -- Huma API setup, route registration, dependency wiring (17 handler groups)
- **handler/** -- Auth, User, Key, Stack, Container, Git, Pipeline, Webhook CRUD, Webhook receiver, SSE, Jobs, Docker Resources, Docker Console, Audit handlers
- **middleware/** -- Auth (session+key), RBAC, security headers, rate limiting, audit logging
- **middleware/** -- Session/API key auth, RBAC enforcement
- **ws/** -- WebSocket terminal handler (raw chi, not huma-managed)
- **dto/** -- Request/response types that become OpenAPI schemas

## Transport

| Purpose | Transport | Why |
|---------|-----------|-----|
| CRUD operations | REST (JSON) | Standard, cacheable, OpenAPI-documented |
| Log streaming, events | SSE | Server-push, auto-reconnect, simple |
| Interactive terminal | WebSocket | Bidirectional stdin/stdout required |

No polling for real-time data (SSE handles that). Background job status uses short-interval polling (2s) in the Jobs UI when jobs are active.

## Domain Events

Events flow through an in-process bus (phase 4: Valkey pub/sub for multi-instance):

```
StackService.Deploy()
  → publishes StackDeployed event
  → EventBus fans out to all SSE subscribers
  → SSE handler sends to connected browsers
```

## Batch deploy ordering

Stacks on one host routinely share a docker network that exactly one of them
creates and the others declare `external: true`. Compose has no cross-project
ordering, so "select all, deploy" after a reboot used to fan out N parallel
`compose up` calls and fail every dependent with "network declared as
external, but could not be found" (servarr, 2026-09-07: 18 stacks). Each stack
therefore carries `depends_on` (`stacks.depends_on`, JSON array of names):
the stacks that must deploy successfully before it *when both are in the same
batch*. `PUT /stacks/{name}/depends-on` validates existence, same host, no
self reference and acyclicity in the domain layer (`stack.SetDependsOn`).
`POST /stacks/deploy-batch` sorts the requested stacks into waves with
`domain/dag` (the same orderer the pipeline executor uses), deploys each wave
concurrently through `StackService.Deploy` (so the shared `StackLocks` still
serialise per stack), and marks a stack `skipped` instead of starting it when
an in-batch dependency failed. Edges to stacks outside the request are
ignored, not auto-added; single-stack deploys ignore `depends_on` entirely.

## Database

PostgreSQL with goose migrations. 11 tables in the schema:
- 5 with full Go implementations: users, sessions, api_keys, stacks, stack_git_configs
- All 11 tables have Go implementations: users, sessions, api_keys, stacks, stack_git_configs, webhooks, webhook_deliveries, pipelines, pipeline_runs, pipeline_step_results, audit_log

Migrations run automatically on startup with advisory locking.

## Full Design Document

For the complete design spec including domain models, endpoint inventory, pipeline engine design, and GitOps flows, see [docs/design.md](design.md).
