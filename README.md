# Composer

A lightweight, self-hosted Docker Compose management platform with GitOps, pipelines, and RBAC.

Think Dockge's simplicity meets Portainer's power -- built from scratch with Go, Astro, and Shadcn/ui.

## Features

- **Stack management** -- Create, deploy, stop, restart, pull Docker Compose stacks via REST API or web UI
- **REST API first** -- Auto-generated OpenAPI 3.1 spec from Go types. Every operation is scriptable
- **Real-time streaming** -- SSE for container logs and events, WebSocket for interactive terminal
- **RBAC** -- Admin / Operator / Viewer roles with session cookies + API keys
- **GitOps** (planned) -- Git-backed stacks with webhook-triggered redeploy
- **Pipelines** (planned) -- CI-esque workflows for automated deployment sequences
- **Lovelace UI** -- Dark theme with pastel-neon accents, Astro 6 + React 19 + Shadcn/ui

## Quick Start

```bash
# Clone
git clone https://github.com/erfianugrah/composer
cd composer

# Start with Docker Compose (includes Postgres)
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
| [API Reference](docs/api-reference.md) | REST endpoints, SSE streams, WebSocket |
| [Deployment](docs/deployment.md) | Docker, Unraid, TrueNAS, bare metal, Podman |
| [Security](docs/security.md) | Docker socket, RBAC, hardening |
| [Architecture](docs/architecture.md) | DDD, tech stack, domain model |
| [Reverse Proxy](docs/reverse-proxy.md) | Caddy, Traefik, nginx configs for TLS |
| [Contributing](docs/contributing.md) | Dev setup, TDD workflow, test tiers |

## Tech Stack

| Backend | Frontend |
|---------|----------|
| Go 1.26 | Astro 6 |
| Huma v2 (OpenAPI 3.1) | React 19 |
| PostgreSQL (pgx) | Shadcn/ui |
| goose (migrations) | Tailwind CSS 4 |
| zap (logging) | Playwright (tests) |
| Docker SDK | Lovelace theme |

## Status

Phase 1 (MVP) -- core stack management, auth, and UI are functional.
See [ARCHITECTURE.md](ARCHITECTURE.md) for the full roadmap.

## License

MIT
