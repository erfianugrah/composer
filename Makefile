.PHONY: dev build test test-unit test-integration test-e2e test-frontend lint clean docker generate

# ── Development ──────────────────────────────────────────────────

dev: ## Start Go (air) + Astro dev server
	@echo "Start Go backend and Astro frontend in separate terminals:"
	@echo "  Terminal 1: COMPOSER_PORT=8080 COMPOSER_DB_URL=postgres://composer:composer@localhost:5432/composer?sslmode=disable COMPOSER_LOG_FORMAT=console go run ./cmd/composerd/"
	@echo "  Terminal 2: cd web && bun run dev"

# ── Build ────────────────────────────────────────────────────────

build: build-frontend build-backend ## Build everything

build-backend: ## Build Go binary
	CGO_ENABLED=0 go build -ldflags="-s -w" -o composerd ./cmd/composerd/

build-frontend: ## Build Astro static site
	cd web && bun install --frozen-lockfile && bun run build

# ── Test ─────────────────────────────────────────────────────────

test: test-unit ## Run unit tests (fast, no Docker needed)

test-unit: ## Run Go unit tests only
	go test ./internal/domain/... ./internal/app/... ./internal/infra/eventbus/... ./internal/infra/crypto/... ./internal/infra/cache/... ./internal/infra/notify/...

test-integration: ## Run Go integration tests (needs Docker for Postgres)
	go test -tags=integration -count=1 -timeout=5m -p 1 ./...

test-e2e: ## Run Go E2E smoke tests (needs Docker daemon)
	go test -tags=e2e -v -count=1 -timeout=15m ./e2e/...

test-frontend: ## Run Playwright browser tests
	cd web && bun run build && bun run test

test-all: test-integration test-frontend ## Run everything

# ── Lint ─────────────────────────────────────────────────────────

lint: ## Run linters
	go vet ./...
	@echo "Go vet: OK"

# ── Docker ───────────────────────────────────────────────────────

docker: ## Build Docker image
	docker build -f deploy/Dockerfile -t composer:local .

docker-run: docker ## Build and run Docker image (needs Postgres + Valkey)
	docker compose -f deploy/compose.yaml up --build

# ── Generate ─────────────────────────────────────────────────────

generate: ## Generate OpenAPI TypeScript client (needs running server)
	@echo "Start server first: COMPOSER_PORT=8080 COMPOSER_LOG_FORMAT=console go run ./cmd/composerd/"
	cd web && bunx openapi-typescript http://localhost:8080/openapi.json -o src/lib/api/types.ts

# ── Clean ────────────────────────────────────────────────────────

clean: ## Remove build artifacts
	rm -f composerd
	rm -rf web/dist web/.astro web/test-results web/playwright-report

# ── Help ─────────────────────────────────────────────────────────

help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-20s\033[0m %s\n", $$1, $$2}'
