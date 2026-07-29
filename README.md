# Composer

A lightweight, self-hosted Docker Compose management platform with GitOps, pipelines, and RBAC.

Think Dockge's simplicity meets Portainer's power -- built from scratch with Go, Astro, and Shadcn/ui.

## Features

- **Stack management** -- Create, deploy, stop, restart, pull, delete, build Docker Compose stacks via REST API or web UI
- **Self-upgrade** -- Composer upgrades its own container via a detached helper: `_system`-scoped webhook (release.yml can fire it) or `POST /api/v1/system/upgrade`, with public status polling across the restart. Compose and plain `docker run` (Unraid) deployments supported
- **Three creation modes** -- From template (10 presets), clone from Git repo, or paste raw YAML
- **Build & Deploy** -- `docker compose up --build` for projects with Dockerfiles. Build images live
- **Background jobs** -- Long-running compose operations run async (`?async=true`). Jobs drawer in UI with live status polling
- **REST API first** -- 80+ endpoints with auto-generated OpenAPI 3.1 spec (security schemes, enum-validated fields, per-operation error codes). TypeScript client regenerated offline via `make generate`. Every operation is scriptable
- **Stack console** -- Run `docker compose` commands per stack without SSH access. Usable by humans, scripts, and LLM agents
- **Docker resource management** -- Networks, volumes, images: list, create, remove, prune from the web UI
- **Dockge migration** -- Import stacks from external directories with one click
- **Stack conversion** -- Convert local stacks to git-backed and vice versa (neither Dockge nor Portainer can do this)
- **Real-time logs** -- SSE streaming of container logs (per-container and stack-level aggregated)
- **Container terminal** -- Interactive shell via WebSocket (xterm.js)
- **Container management** -- Global container page with start/stop/restart, stats, health badges
- **Docker events** -- Real-time Docker event stream on dashboard
- **GitOps** -- Git-backed stacks with webhook-triggered auto-redeploy (GitHub, GitLab, Gitea) + delivery history + dirty-state detection; per-stack `env_path` for `.env` files that live next to the compose file in a subdirectory
- **Docker registry auth** -- Multi-registry credentials (global + per-stack overrides), encrypted at rest, materialised into an ephemeral `DOCKER_CONFIG` per deploy. Seeded via UI / API / `COMPOSER_REGISTRY_AUTHS` env
- **Pipelines** -- CI-esque workflows with DAG execution, concurrent steps, 8 step types, cron scheduling
- **RBAC** -- Admin / Operator / Viewer roles with session cookies + API keys
- **OAuth/OIDC** -- Login with GitHub or Google accounts
- **Audit log** -- All mutating API operations logged with user, action, IP. Queryable via API
- **Compose editor** -- CodeMirror 6 with Docker Compose schema autocompletion and syntax highlighting
- **Compose diff** -- Compare disk content vs running Docker config
- **Security** -- Credentials and SSH keys encrypted at rest (AES-256-GCM), SOPS/age decryption for encrypted .env and compose secrets, session tokens hashed, CSRF protection, CSP headers
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
| [API Reference](docs/api-reference.md) | 106 REST endpoints, SSE streams, WebSocket |
| [Deployment](docs/deployment.md) | Docker, Unraid, TrueNAS, bare metal, Podman |
| [Security](docs/security.md) | Docker socket, RBAC, encryption, hardening |
| [Architecture](docs/architecture.md) | DDD, tech stack, domain model |
| [Design Spec](docs/design.md) | Full design document (domain models, all endpoints) |
| [Reverse Proxy](docs/reverse-proxy.md) | Caddy, Traefik, nginx configs for TLS |
| [Contributing](docs/contributing.md) | Dev setup, TDD workflow, test tiers |

## Remote Docker hosts (mTLS)

Composer supports connecting to a remote Docker engine over TLS with mutual authentication. Both the Go SDK (container/stack/image operations) and the `docker compose` CLI (deploys, builds, pulls) honor the same environment variables -- the CLI inherits them from the process environment.

| Variable | Required | Description |
|----------|----------|-------------|
| `COMPOSER_DOCKER_HOST` | No | Docker host URL, e.g. `tcp://<host>:2376`. Takes precedence over `DOCKER_HOST`. |
| `DOCKER_TLS_VERIFY` | Yes | Set to `1` to enable TLS verification with client certificates. |
| `DOCKER_CERT_PATH` | Yes | Directory containing `ca.pem`, `cert.pem`, and `key.pem`. |

### Certificate naming

The `DOCKER_CERT_PATH` directory must contain three PEM files:
- `ca.pem` -- CA certificate that signed the server's certificate
- `cert.pem` -- Client certificate (the CN is what the remote engine's audit log records)
- `key.pem` -- Client private key

### How it works

1. **SDK path** (`internal/infra/docker/client.go`): `NewClient` calls `dockerclient.FromEnv` before `dockerclient.WithHost`. When `DOCKER_TLS_VERIFY=1` and `DOCKER_CERT_PATH` are set, `FromEnv` configures the Docker SDK's TLS client config so all API calls (container inspect, image pull, etc.) use mTLS.

2. **CLI path** (`internal/infra/docker/compose.go`): `applyExtraEnv` and `RunPTY` both build the subprocess environment from `cmd.Environ()`, which inherits the process environment. The env vars pass through to the `docker` / `docker compose` CLI, which reads `DOCKER_TLS_VERIFY` and `DOCKER_CERT_PATH` natively.

## Multiple docker hosts

One composerd can manage stacks across several Docker daemons. The daemon configured via `COMPOSER_DOCKER_HOST` (or socket auto-detection) is the **default host**; additional daemons are registered as named **docker hosts** in the database (`docker_hosts` table) via `POST /api/v1/hosts` or Settings -> Docker Hosts in the UI.

Each registered host has:

| Field | Description |
|-------|-------------|
| `name` | Unique handle used across the API/UI (lowercase, e.g. `remote1`) |
| `endpoint` | `tcp://<host>:2376` (mTLS), `tcp://<host>:2375` (plain), or `unix:///path.sock` |
| `cert_dir` | Optional directory holding `ca.pem`/`cert.pem`/`key.pem` for mTLS endpoints |

**Assigning stacks:** pass `host: "<name>"` when creating a stack (git or local). Empty (or `local`) pins the stack to the default host - existing stacks need no migration. Deploys, auto-deploys, compose actions, and `docker compose` PTY sessions for that stack then run against its host, with the host's own mTLS material exported to the CLI (`DOCKER_HOST`/`DOCKER_TLS_VERIFY`/`DOCKER_CERT_PATH` per invocation, never shared process env).

**Elsewhere hosts apply:** the stack list/detail show a host badge; container/network/volume/image endpoints accept `?host=<name>`; SSE log/stats streams and the container terminal accept `host=<name>`; pipeline `docker_exec` steps accept an optional `"host"` config key, and compose steps automatically use the stack's host. Docker events are collected per host and domain events carry the host name. Self-upgrade always acts on the default host only.

## Tech Stack

| Backend | Frontend |
|---------|----------|
| Go 1.26 | Astro 6 |
| Huma v2 (OpenAPI 3.1) | React 19 |
| SQLite + PostgreSQL (database/sql) | Shadcn/ui + Tailwind CSS 4 |
| AES-256-GCM encryption | xterm.js (terminal) |
| SOPS + age (encrypted secrets) | CodeMirror 6 (editor + autocomplete) |
| go-git (GitOps) | Playwright (44 tests) |
| Valkey (cache) | Lovelace theme |
| zap (logging) | SSE + WebSocket streaming |
| Docker SDK v28 | |

## Status

99 API endpoints, 9 pages, 39 components, 44 Playwright tests, 181 Go test functions, 18.2k lines of Go.

## License

MIT
