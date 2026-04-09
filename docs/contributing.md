# Contributing

## Prerequisites

- **Go 1.26+** -- https://go.dev/dl/
- **Bun 1.2+** -- https://bun.sh
- **Docker** (or Podman) -- for integration tests and stack management

## Setup

```bash
git clone https://github.com/erfianugrah/composer
cd composer

go mod download
cd web && bun install && cd ..
```

## Development

```bash
# Quickstart (SQLite -- no external dependencies)
COMPOSER_LOG_FORMAT=console go run ./cmd/composerd/

# Or with Postgres + Valkey for full production setup:
docker compose -f deploy/compose.yaml up -d postgres valkey
COMPOSER_PORT=8080 \
COMPOSER_DB_URL="postgres://composer:composer@localhost:5432/composer?sslmode=disable" \
COMPOSER_VALKEY_URL="valkey://localhost:6379" \
COMPOSER_LOG_FORMAT=console \
go run ./cmd/composerd/

# Frontend dev server (separate terminal):
cd web && bun run dev
```

Backend on `:8080`, frontend dev server on `:4321`.
Leave `COMPOSER_DB_URL` empty for SQLite (stored in `/opt/composer/composer.db`).

See [configuration.md](configuration.md) for all environment variables.

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
│   │   └── pipeline/              # Pipeline aggregate, step, run, DAG
│   ├── app/                       # Application services
│   │   ├── auth_service.go        # Login, bootstrap, session, API keys
│   │   ├── stack_service.go       # CRUD, deploy, stop, restart, pull
│   │   ├── git_service.go         # GitOps: sync, redeploy, log, status
│   │   ├── pipeline_service.go    # Pipeline CRUD, async run
│   │   ├── pipeline_executor.go   # DAG step executor
│   │   ├── cron_scheduler.go      # Cron triggers for pipelines
│   │   ├── jobs.go                # Background job manager (in-memory)
│   │   ├── templates.go           # Built-in stack templates
│   │   └── diff.go                # Compose diff algorithm
│   ├── infra/                     # Infrastructure implementations
│   │   ├── docker/                # Docker SDK client + compose CLI + event listener
│   │   ├── store/                 # database/sql repos (Postgres+SQLite) + goose migrations
│   │   ├── eventbus/              # In-memory event bus
│   │   ├── git/                   # go-git + webhook signature validation
│   │   ├── crypto/                # AES-256-GCM encryption (strings + files + SSH keys)
│   │   ├── cache/                 # Valkey session/key caching
│   │   ├── notify/                # Webhook + Slack notifications
│   │   └── fs/                    # Compose file I/O
│   └── api/                       # HTTP layer
│       ├── server.go              # Huma API, 14 handler groups, middleware
│       ├── handler/               # Auth, User, Key, Stack, Container, Git,
│       │                          # Pipeline, Webhook, Template, System, SSE,
│       │                          # Jobs, Docker Resources, Docker Console, Audit
│       ├── middleware/             # Auth, RBAC, CSRF, security headers,
│       │                          # rate limiting, audit
│       ├── ws/                    # WebSocket terminal
│       └── dto/                   # Request/response types
├── web/                           # Astro 6 + React 19 frontend
├── docs/                          # Documentation (you are here)
├── deploy/                        # Dockerfile + compose.yaml
├── scripts/                       # OpenAPI client generation
└── Makefile
```

## TDD Workflow

```
1. Write failing test in *_test.go
2. Run: make test (RED)
3. Implement the code
4. Run: make test (GREEN)
5. Refactor
```

## Test Tiers

### Unit Tests (fast, no Docker)

```bash
make test-unit
# or: go test ./internal/domain/... ./internal/infra/eventbus/... ./internal/infra/cache/...
```

### Integration Tests (needs Docker)

```bash
make test-integration
# or: go test -tags=integration -count=1 -timeout=5m -p 1 ./...
```

Uses testcontainers-go for real Postgres containers. Use `-p 1` for sequential execution.

### Playwright Browser Tests

```bash
make test-frontend
# or: cd web && bun run build && bun run test
```

### All Tests

```bash
make test-all
```

## Adding a New Feature

1. Define domain model in `internal/domain/`
2. Define repository interfaces in the domain package
3. Write domain unit tests (TDD)
4. Implement infrastructure in `internal/infra/`
5. Write integration tests with testcontainers
6. Add application service in `internal/app/`
7. Add API handler in `internal/api/handler/`
8. Add DTOs in `internal/api/dto/`
9. Register handler in `internal/api/server.go`
10. Add Playwright tests for frontend components

## Code Rules

- `go vet ./...` must pass
- No TODO/FIXME/HACK comments
- Domain purity: `internal/domain/` imports ZERO from `internal/infra/` or `internal/api/`
- All exported types need doc comments
- Mutating API tests must include `X-Requested-With: XMLHttpRequest` header (CSRF)

## Commit Messages

Follow conventional commits:
- `feat:` new feature
- `fix:` bug fix
- `refactor:` code restructuring
- `test:` adding tests
- `docs:` documentation
- `ci:` CI/CD changes
- `chore:` maintenance
