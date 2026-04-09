# Composer

A lightweight, self-hosted Docker Compose management platform with GitOps, pipelines, and RBAC.

Think Dockge's simplicity meets Portainer's power -- built from scratch with Go, Astro, and Shadcn/ui.

## Features

- **Stack management** -- Create, deploy, stop, restart, pull, delete, build Docker Compose stacks via REST API or web UI
- **Three creation modes** -- From template (10 presets), clone from Git repo, or paste raw YAML
- **Build & Deploy** -- `docker compose up --build` for projects with Dockerfiles. Build images live
- **Background jobs** -- Long-running compose operations run async (`?async=true`). Jobs drawer in UI with live status polling
- **REST API first** -- 87 endpoints with auto-generated OpenAPI 3.1 spec. Every operation is scriptable
- **Stack console** -- Run `docker compose` commands per stack without SSH access. Usable by humans, scripts, and LLM agents
- **Docker resource management** -- Networks, volumes, images: list, create, remove, prune from the web UI
- **Dockge migration** -- Import stacks from external directories with one click
- **Stack conversion** -- Convert local stacks to git-backed and vice versa (neither Dockge nor Portainer can do this)
- **Real-time logs** -- SSE streaming of container logs (per-container and stack-level aggregated)
- **Container terminal** -- Interactive shell via WebSocket (xterm.js)
- **Container management** -- Global container page with start/stop/restart, stats, health badges
- **Docker events** -- Real-time Docker event stream on dashboard
- **GitOps** -- Git-backed stacks with webhook-triggered auto-redeploy (GitHub, GitLab, Gitea) + delivery history + dirty-state detection
- **Pipelines** -- CI-esque workflows with DAG execution, concurrent steps, 8 step types, cron scheduling
- **RBAC** -- Admin / Operator / Viewer roles with session cookies + API keys
- **OAuth/OIDC** -- Login with GitHub or Google accounts
- **Audit log** -- All mutating API operations logged with user, action, IP. Queryable via API
- **Compose editor** -- CodeMirror 6 with Docker Compose schema autocompletion and syntax highlighting
- **Compose diff** -- Compare disk content vs running Docker config
- **Security** -- Credentials and SSH keys encrypted at rest (AES-256-GCM), session tokens hashed, CSRF protection, CSP headers
- **Dual database** -- SQLite (default, zero config) or PostgreSQL for multi-instance
- **Command palette** -- Cmd+K fuzzy search for quick navigation
- **Lovelace UI** -- Dark theme with pastel-neon accents, Astro 6 + React 19 + Shadcn/ui

## Quick Start

```bash
# Single container with SQLite (no external DB needed)
docker run -d --name composer -p 8080:8080 \
  -v /var/run/docker.sock:/var/run/docker.sock \
  -v composer_data:/opt/composer \
  -v composer_stacks:/opt/stacks \
  ghcr.io/erfianugrah/composer:latest

# Open http://localhost:8080
# First visit: create admin account via bootstrap
```

Or with Docker Compose + PostgreSQL + Valkey: `docker compose -f deploy/compose.yaml up -d`

See [docs/getting-started.md](docs/getting-started.md) for detailed setup.

## Documentation

| Document | Description |
|----------|-------------|
| [Getting Started](docs/getting-started.md) | Installation, first run, bootstrap |
| [Configuration](docs/configuration.md) | All environment variables, encryption, PUID/PGID |
| [API Reference](docs/api-reference.md) | 87 REST endpoints, SSE streams, WebSocket |
| [Deployment](docs/deployment.md) | Docker, Unraid, TrueNAS, bare metal, Podman |
| [Security](docs/security.md) | Docker socket, RBAC, encryption, hardening |
| [Architecture](docs/architecture.md) | DDD, tech stack, domain model |
| [Design Spec](docs/design.md) | Full design document (domain models, all endpoints) |
| [Reverse Proxy](docs/reverse-proxy.md) | Caddy, Traefik, nginx configs for TLS |
| [Contributing](docs/contributing.md) | Dev setup, TDD workflow, test tiers |

## Tech Stack

| Backend | Frontend |
|---------|----------|
| Go 1.26 | Astro 6 |
| Huma v2 (OpenAPI 3.1) | React 19 |
| SQLite + PostgreSQL (database/sql) | Shadcn/ui + Tailwind CSS 4 |
| AES-256-GCM encryption | xterm.js (terminal) |
| go-git (GitOps) | CodeMirror 6 (editor + autocomplete) |
| Valkey (cache) | Playwright (45 tests) |
| zap (logging) | Lovelace theme |
| Docker SDK v28 | SSE + WebSocket streaming |

## Status

87 API endpoints, 9 pages, 34 components, 45 Playwright tests, 16k+ lines of Go.

## License

MIT
