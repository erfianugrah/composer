# Contributing

## Prerequisites

- **Go 1.26+** -- https://go.dev/dl/
- **Bun 1.2+** -- https://bun.sh
- **Docker** -- for integration tests (testcontainers spins up Postgres)

## Setup

```bash
git clone https://github.com/erfianugrah/composer
cd composer

go mod download
cd web && bun install && cd ..
```

## Development Workflow

```bash
# Terminal 1: Go backend
COMPOSER_PORT=8080 \
COMPOSER_DB_URL="postgres://composer:composer@localhost:5432/composer?sslmode=disable" \
COMPOSER_LOG_FORMAT=console \
go run ./cmd/composerd/

# Terminal 2: Astro frontend
cd web && bun run dev
```

Backend on `:8080`, frontend dev server on `:4321`.

## TDD Workflow

Tests are written BEFORE implementation:

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
# or: go test ./internal/domain/... ./internal/infra/eventbus/...
```

Domain models, value objects, event bus. No external dependencies.

### Integration Tests (needs Docker)

```bash
make test-integration
# or: go test -tags=integration -count=1 -timeout=5m -p 1 ./...
```

Uses testcontainers-go to spin up real Postgres containers. Tests:
- Repository CRUD against real Postgres
- Auth service flows (bootstrap, login, session management)
- API handlers end-to-end (HTTP -> middleware -> handler -> service -> DB)
- Docker client operations (against real Docker daemon)
- WebSocket terminal (against real Alpine container)
- Auth middleware (session cookie, API key, RBAC, bypass paths)

Use `-p 1` to run packages sequentially -- prevents testcontainer resource contention.

### Playwright Browser Tests

```bash
make test-frontend
# or: cd web && bun run build && bun run test
```

Chromium-based tests against the built Astro static site. Tests UI rendering, navigation, form interactions, theme application.

### All Tests

```bash
make test-all
```

## Project Structure

```
internal/
  domain/     # Business logic (ZERO infra imports)
  app/        # Application services (orchestration)
  infra/      # Infrastructure (Docker, Postgres, EventBus)
  api/        # HTTP handlers, middleware, WebSocket
```

**Rule: The domain layer MUST NOT import from infra/ or api/.** This is enforced and checked in CI.

## Adding a New Feature

1. Define the domain model in `internal/domain/`
2. Define repository interfaces in the domain package
3. Write domain unit tests (TDD)
4. Implement the infrastructure (Postgres repo, etc.) in `internal/infra/`
5. Write integration tests with testcontainers
6. Add the application service in `internal/app/`
7. Add the API handler in `internal/api/handler/`
8. Add DTOs in `internal/api/dto/`
9. Register the handler in `internal/api/server.go`

## Code Style

- `go vet ./...` must pass
- No TODO/FIXME/HACK comments in committed code
- Domain purity: `grep -rn '"internal/infra\|"internal/api' internal/domain/` must return nothing
- All exported types and functions need doc comments

## Commit Messages

Follow conventional commits:
- `feat:` new feature
- `fix:` bug fix
- `refactor:` code restructuring
- `test:` adding tests
- `docs:` documentation
- `ci:` CI/CD changes
- `chore:` maintenance
