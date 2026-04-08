# Composer

A lightweight, self-hosted Docker Compose management platform with GitOps, pipelines, and RBAC.

Think Dockge's simplicity meets Portainer's power -- built from scratch with Go, Astro, and Shadcn/ui.

## Features

- **Stack management** -- Create, deploy, stop, restart, pull Docker Compose stacks via REST API or web UI
- **REST API first** -- 53 endpoints with auto-generated OpenAPI 3.1 spec. Every operation is scriptable
- **OAuth/OIDC** -- Login with GitHub or Google accounts via goth
- **Stack templates** -- 10 built-in compose presets (nginx, postgres, immich, etc.)
- **GitOps** -- Git-backed stacks with webhook-triggered auto-redeploy (GitHub, GitLab, Gitea)
- **Pipelines** -- CI-esque workflows with DAG execution, concurrent steps, and 8 step types
- **Real-time streaming** -- SSE for container logs, stats, and events; WebSocket for interactive terminal
- **RBAC** -- Admin / Operator / Viewer roles with session cookies + API keys
- **Container stats** -- Live CPU, memory, network, and disk I/O streaming via SSE
- **Audit log** -- All mutating API operations are logged with user, action, and IP
- **Command palette** -- Cmd+K fuzzy search for quick navigation and actions
- **Lovelace UI** -- Dark theme with pastel-neon accents, Astro 6 + React 19 + Shadcn/ui

## Quick Start

```bash
# Clone
git clone https://github.com/erfianugrah/composer
cd composer

# Start with Docker Compose (includes Postgres + Valkey)
docker compose -f deploy/compose.yaml up -d

# Open http://localhost:8080
# First visit: create admin account via bootstrap
```

See [docs/getting-started.md](docs/getting-started.md) for detailed setup.

## Documentation

| Document | Description |
|----------|-------------|
| [Getting Started](docs/getting-started.md) | Installation, first run, bootstrap |
| [Configuration](docs/configuration.md) | All environment variables, PUID/PGID |
| [API Reference](docs/api-reference.md) | 53 REST endpoints, SSE streams, WebSocket |
| [Deployment](docs/deployment.md) | Docker, Unraid, TrueNAS, bare metal, Podman |
| [Security](docs/security.md) | Docker socket, RBAC, hardening |
| [Architecture](docs/architecture.md) | DDD, tech stack, domain model |
| [Design Spec](docs/design.md) | Full design document (domain models, all endpoints, roadmap) |
| [Reverse Proxy](docs/reverse-proxy.md) | Caddy, Traefik, nginx configs for TLS |
| [Contributing](docs/contributing.md) | Dev setup, TDD workflow, test tiers |

## Tech Stack

| Backend | Frontend |
|---------|----------|
| Go 1.26 | Astro 6 |
| Huma v2 (OpenAPI 3.1) | React 19 |
| PostgreSQL (pgx) | Shadcn/ui + Tailwind CSS 4 |
| go-git (GitOps) | xterm.js (terminal) |
| Valkey (cache) | CodeMirror 6 (editor) |
| zap (logging) | Playwright (tests) |
| Docker SDK | Lovelace theme |

## Status

All 5 phases complete. 60+ API endpoints, 300+ tests, 12k+ lines of Go.
See [docs/design.md](docs/design.md) for the full design spec.

## License

MIT
