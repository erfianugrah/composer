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
- **event/** -- Event bus interface + all domain event types (stack, container, pipeline)

The domain layer imports only the Go standard library (+ `golang.org/x/crypto/bcrypt`).

### Application (internal/app/)

Orchestrates domain objects and infrastructure:
- **AuthService** -- Bootstrap, login/logout, session validation, API key management
- **StackService** -- CRUD, deploy/stop/restart/pull, event publishing
- **GitService** -- Git-backed stack creation, sync, sync+redeploy (GitOps flow)
- **PipelineService** -- Pipeline CRUD, async run execution
- **PipelineExecutor** -- DAG step executor with concurrency, timeouts, cancellation

### Infrastructure (internal/infra/)

Implements domain interfaces with real technology:
- **docker/** -- Docker Engine SDK client (container ops) + compose CLI wrapper + event listener
- **store/postgres/** -- pgx repository implementations (users, sessions, keys, stacks, git configs, webhooks, pipelines, runs, audit)
- **eventbus/** -- In-memory pub/sub event bus
- **git/** -- go-git wrapper (clone, pull, log, diff, commit, push) + webhook signature validation (GitHub, GitLab, Gitea)
- **cache/** -- Valkey client for session and API key caching
- **notify/** -- Notification dispatcher (webhook, Slack) [Phase 5]

### API (internal/api/)

HTTP layer translating between the web and application services:
- **server.go** -- Huma API setup, route registration, dependency wiring (12 handler groups)
- **handler/** -- Auth, User, Key, Stack, Container, Git, Pipeline, Webhook CRUD, Webhook receiver, SSE handlers
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

No polling. No Socket.IO.

## Domain Events

Events flow through an in-process bus (phase 4: Valkey pub/sub for multi-instance):

```
StackService.Deploy()
  → publishes StackDeployed event
  → EventBus fans out to all SSE subscribers
  → SSE handler sends to connected browsers
```

## Database

PostgreSQL with goose migrations. 11 tables in the schema:
- 5 with full Go implementations: users, sessions, api_keys, stacks, stack_git_configs
- 6 reserved for future phases: webhooks, webhook_deliveries, pipelines, pipeline_runs, pipeline_step_results, audit_log, settings

Migrations run automatically on startup with advisory locking.

## Full Design Document

For the complete design spec including domain models, all planned endpoints, pipeline engine design, and GitOps flows, see [ARCHITECTURE.md](../ARCHITECTURE.md) in the repo root.
