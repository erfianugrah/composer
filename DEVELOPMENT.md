# Composer - Development Guide

## Prerequisites

- **Go 1.26+** -- https://go.dev/dl/
- **Bun 1.2+** -- https://bun.sh
- **Docker** (or Podman) -- for integration tests and stack management
- **PostgreSQL 17** -- via Docker or local install

## Quick Start

```bash
git clone https://github.com/erfianugrah/composer
cd composer

# Install Go dependencies
go mod download

# Install frontend dependencies
cd web && bun install && cd ..

# Start infrastructure (Postgres)
docker compose -f deploy/compose.yaml up -d postgres

# Run the server (configure via COMPOSER_* env vars)
COMPOSER_PORT=8080 \
COMPOSER_DB_URL="postgres://composer:composer@localhost:5432/composer?sslmode=disable" \
COMPOSER_LOG_FORMAT=console \
go run ./cmd/composerd/

# In another terminal: start frontend dev server
cd web && bun run dev
```

The Go server runs on `:8080`, the Astro dev server on `:4321`.

### Environment Variables

All configuration via `COMPOSER_*` environment variables:

| Variable | Default | Description |
|----------|---------|-------------|
| `COMPOSER_PORT` | `8080` | HTTP listen port |
| `COMPOSER_DB_URL` | `postgres://composer:composer@localhost:5432/composer?sslmode=disable` | Postgres connection URL |
| `COMPOSER_STACKS_DIR` | `/opt/stacks` | Directory for compose stack files |
| `COMPOSER_DOCKER_HOST` | (auto-detect) | Docker/Podman socket path |
| `COMPOSER_LOG_LEVEL` | `info` | Log level: debug, info, warn, error |
| `COMPOSER_LOG_FORMAT` | `json` | Log format: json, console |

## Project Structure

```
composer/
├── cmd/composerd/main.go          # Entry point -- wires everything
├── internal/
│   ├── domain/                    # DDD domain layer (ZERO infra imports)
│   │   ├── auth/                  # User, Session, APIKey, Role, repos
│   │   ├── container/             # Container entity, status, health
│   │   ├── event/                 # Event bus interface + event types
│   │   ├── stack/                 # Stack aggregate, GitSource, repos
│   │   └── pipeline/              # (Phase 3 -- empty)
│   ├── app/                       # Application services
│   │   ├── auth_service.go        # Login, bootstrap, session, API keys
│   │   └── stack_service.go       # CRUD, deploy, stop, restart, pull
│   ├── infra/                     # Infrastructure implementations
│   │   ├── docker/                # Docker SDK client + compose CLI wrapper
│   │   ├── store/postgres/        # pgx repos + goose migrations
│   │   ├── eventbus/              # In-memory event bus
│   │   ├── git/                   # (Phase 2 -- empty)
│   │   ├── cache/                 # (Phase 4 -- empty)
│   │   └── fs/                    # (planned -- empty)
│   └── api/                       # HTTP layer
│       ├── server.go              # Huma API setup, route registration
│       ├── handler/               # Auth, Stack, SSE handlers
│       ├── middleware/             # Auth + RBAC middleware
│       ├── ws/                    # WebSocket terminal handler
│       └── dto/                   # Request/response types (OpenAPI schemas)
├── web/                           # Astro 6 frontend
│   ├── src/pages/                 # Astro pages (login, dashboard, stacks, etc.)
│   ├── src/components/            # React islands + shadcn/ui
│   ├── src/styles/globals.css     # Lovelace theme (Tailwind v4 @theme)
│   ├── e2e/                       # Playwright browser tests
│   └── playwright.config.ts
├── e2e/                           # Go E2E smoke tests (//go:build e2e)
├── deploy/                        # Dockerfile + compose.yaml (pending)
├── ARCHITECTURE.md                # Full design document
└── DEVELOPMENT.md                 # This file
```

## Running Tests

### Go Unit Tests (fast, no external deps)

```bash
go test ./internal/domain/... ./internal/infra/eventbus/...
```

### Go Integration Tests (needs Docker for Postgres containers)

```bash
go test -tags=integration -count=1 -timeout=5m -p 1 ./...
```

Use `-p 1` to run packages sequentially and avoid testcontainer resource
contention.

### Go E2E Smoke Tests (needs Docker daemon + Postgres)

```bash
go test -tags=e2e -v -count=1 -timeout=15m ./e2e/...
```

### Frontend Build

```bash
cd web && bun run build
```

### Playwright Browser Tests (needs built frontend)

```bash
cd web && bun run test
```

This starts `astro preview` on port 4321 and runs Chromium tests.

### All Tests

```bash
# Go (unit + integration)
go test -tags=integration -count=1 -timeout=5m -p 1 ./...

# Frontend
cd web && bun run build && bun run test
```

## Architecture Decisions

| Decision | Choice | Reasoning |
|----------|--------|-----------|
| REST framework | Huma v2 | Auto OpenAPI 3.1 from Go types, built-in SSE |
| Streaming | SSE for server-push, WS for terminal only | SSE is simpler than WebSocket for unidirectional streams |
| Auth | Custom session + API key + RBAC | Tailored to our needs, patterns from gloryhole/gatekeeper |
| Frontend | Astro (static) + React islands | Fast TTFB, interactive only where needed |
| Database | Postgres via pgx | Reliable, JSONB for flexible config, pgxpool for connection pooling |
| Migrations | goose v3 | Embedded SQL, Provider API, advisory locking |
| Docker interaction | SDK for container ops, CLI for compose | SDK is lightweight; compose CLI avoids heavy `docker/compose` dep tree |
| Event bus | In-memory channels | Simple, fast. Valkey planned for Phase 4 multi-instance |
| RBAC | viewer < operator < admin | Enforced at handler level via `middleware.CheckRole()` |

## Current Status

**92 tests total** (86 Go + 6 Playwright), 0 failures.

| Layer | Status | Tests |
|-------|--------|-------|
| Domain models (auth, stack, container, events) | Done | 30 unit tests |
| Auth service (bootstrap, login, session, API keys) | Done | 5 integration tests |
| Postgres repos (users, sessions, keys, stacks, git configs) | Done | 13 integration tests |
| Docker client + compose CLI | Done | 5 integration tests |
| Event bus (pub/sub) | Done | 5 unit tests |
| API handlers (auth + stack CRUD + compose ops) | Done | 13 integration tests |
| WebSocket terminal | Done | 1 integration test |
| SSE streaming (events + logs) | Done | Handler built, no dedicated tests |
| RBAC enforcement | Done | Wired into all handlers |
| Astro frontend (login, dashboard, stacks, settings) | Done | 6 Playwright tests |
| Cross-compilation (amd64 + arm64) | Verified | CGO_ENABLED=0 |

See `ARCHITECTURE.md` for the full design document and implementation phases.
