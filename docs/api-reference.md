# API Reference

Composer exposes a REST API with auto-generated OpenAPI 3.1 spec.

**Live spec:** `GET /openapi.json` or `GET /openapi.yaml`

**Total endpoints:** 80 Huma-registered operations (+ WebSocket, OAuth, and webhook receiver paths registered as raw chi handlers).

The spec declares three security schemes (`cookieAuth`, `apiKeyAuth`, `bearerAuth`) and enumerates per-operation error codes (401 / 403 / 404 / 409 / 422 / 429 / 500) so generated clients can branch on specific failures. Output fields for status-like data (`StackSummary.Status`, `ContainerOutput.Health`, `GitSourceOutput.SyncStatus`, etc.) declare their enum values in the schema.

Regenerate the TypeScript client (`web/src/lib/api/types.ts`) offline via `make generate` — no running server needed.

## Authentication

All endpoints except health, bootstrap, login, templates, and public OAuth paths require authentication via session cookie or API key. The OpenAPI spec (`/openapi.json`, `/openapi.yaml`) and docs UI (`/docs`) are publicly readable for tooling integration.

Public endpoints are marked with an empty `security: []` override in the spec; every other endpoint inherits the three-way security requirement (any of cookie, X-API-Key, or Bearer).

### Session Cookie

```bash
curl -c cookies.txt -X POST /api/v1/auth/login \
  -H "Content-Type: application/json" \
  -d '{"email":"user@example.com","password":"password"}'

curl -b cookies.txt /api/v1/stacks
```

### API Key

```bash
curl -H "Authorization: Bearer ck_your_key_here" /api/v1/stacks
# or
curl -H "X-API-Key: ck_your_key_here" /api/v1/stacks
```

## Endpoints

### Auth (4 endpoints)

| Method | Path | Auth | Description |
|--------|------|------|-------------|
| `POST` | `/api/v1/auth/bootstrap` | None | Create first admin user (disabled after first user) |
| `POST` | `/api/v1/auth/login` | None | Login, returns session cookie |
| `POST` | `/api/v1/auth/logout` | Session | Destroy session, clear cookie |
| `GET` | `/api/v1/auth/session` | Session/Key | Validate session, return user info |

### Users (6 endpoints, admin only)

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/api/v1/users` | List all users |
| `POST` | `/api/v1/users` | Create user (email, password, role) |
| `GET` | `/api/v1/users/{id}` | Get user by ID |
| `PUT` | `/api/v1/users/{id}` | Update user (email, role) |
| `DELETE` | `/api/v1/users/{id}` | Delete user |
| `PUT` | `/api/v1/users/{id}/password` | Change password (admin or self) |

### API Keys (4 endpoints, operator+)

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/api/v1/keys` | List API keys (secrets redacted) |
| `GET` | `/api/v1/keys/{id}` | Get key details |
| `POST` | `/api/v1/keys` | Create key (plaintext shown once!) |
| `DELETE` | `/api/v1/keys/{id}` | Revoke key |

### Stacks (20 endpoints)

Compose operations (`up`, `build`, `down`, `restart`, `pull`) accept `?async=true` to run as a background job. When async, the response includes a `job_id` instead of stdout/stderr. Poll `GET /api/v1/jobs/{id}` for status.

| Method | Path | Role | Description |
|--------|------|------|-------------|
| `GET` | `/api/v1/stacks` | Viewer+ | List all stacks with status |
| `POST` | `/api/v1/stacks` | Operator+ | Create local stack from YAML |
| `POST` | `/api/v1/stacks/git` | Operator+ | Clone a git repo and create git-backed stack |
| `POST` | `/api/v1/stacks/import` | Admin | Import stacks from external directory (Dockge migration) |
| `GET` | `/api/v1/stacks/{name}` | Viewer+ | Get stack detail + containers + Dockerfiles |
| `PUT` | `/api/v1/stacks/{name}` | Operator+ | Update compose content. Marks git stacks as `dirty` |
| `PUT` | `/api/v1/stacks/{name}/env` | Operator+ | Update `.env` file for stack |
| `DELETE` | `/api/v1/stacks/{name}` | Operator+ | Delete stack. `?remove_volumes=true` |
| `POST` | `/api/v1/stacks/{name}/up` | Operator+ | Deploy (docker compose up). `?async=true` |
| `POST` | `/api/v1/stacks/{name}/build` | Operator+ | Build & deploy (docker compose up --build). `?async=true` |
| `POST` | `/api/v1/stacks/{name}/down` | Operator+ | Stop (docker compose down). `?async=true` |
| `POST` | `/api/v1/stacks/{name}/restart` | Operator+ | Restart all services. `?async=true` |
| `POST` | `/api/v1/stacks/{name}/pull` | Operator+ | Pull latest images. `?async=true` |
| `POST` | `/api/v1/stacks/{name}/validate` | Operator+ | Validate compose syntax |
| `POST` | `/api/v1/stacks/{name}/exec` | Operator+ | Run docker compose command (console) |
| `POST` | `/api/v1/stacks/{name}/convert/git` | Operator+ | Convert local stack to git-backed |
| `POST` | `/api/v1/stacks/{name}/convert/local` | Operator+ | Detach git, convert to local |
| `GET` | `/api/v1/stacks/{name}/diff` | Viewer+ | Show pending compose changes |
| `GET` | `/api/v1/stacks/{name}/credentials` | Operator+ | Get resolved credential chain (per-stack vs global) |
| `PUT` | `/api/v1/stacks/{name}/credentials` | Operator+ | Update per-stack credential overrides |

### Containers (6 endpoints)

| Method | Path | Role | Description |
|--------|------|------|-------------|
| `GET` | `/api/v1/containers` | Viewer+ | List all containers |
| `GET` | `/api/v1/containers/{id}` | Viewer+ | Get container detail |
| `GET` | `/api/v1/containers/{id}/logs` | Viewer+ | Get container logs snapshot. `?tail=100&since=5m` |
| `POST` | `/api/v1/containers/{id}/start` | Operator+ | Start container (Compose-managed only) |
| `POST` | `/api/v1/containers/{id}/stop` | Operator+ | Stop container (Compose-managed only) |
| `POST` | `/api/v1/containers/{id}/restart` | Operator+ | Restart container (Compose-managed only) |

### Docker Console (1 endpoint, admin)

| Method | Path | Description |
|--------|------|-------------|
| `POST` | `/api/v1/docker/exec` | Run a docker command on the host (ps, images, network ls, etc.) |

### Git Operations (5 endpoints, requires git-backed stack)

Git status returns `sync_status` which can be: `synced`, `behind`, `diverged`, `dirty`, `error`, `syncing`. The `dirty` status means the compose file was edited locally and diverges from git HEAD. The `working_tree_dirty` boolean flag is also returned for convenience.

| Method | Path | Role | Description |
|--------|------|------|-------------|
| `POST` | `/api/v1/stacks/{name}/sync` | Operator+ | Git pull + detect compose changes. Clears `dirty` status on success |
| `GET` | `/api/v1/stacks/{name}/git/log` | Viewer+ | Commit history (filtered to compose file). `?limit=20` |
| `GET` | `/api/v1/stacks/{name}/git/status` | Viewer+ | Sync status, branch, last commit, `working_tree_dirty` flag |
| `POST` | `/api/v1/stacks/{name}/rollback` | Operator+ | Checkout specific git commit |
| `GET` | `/api/v1/stacks/{name}/git/diff` | Viewer+ | Working tree diff vs last commit |

### Pipelines (9 endpoints, operator+)

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/api/v1/pipelines` | List pipelines |
| `POST` | `/api/v1/pipelines` | Create pipeline (steps + triggers) |
| `GET` | `/api/v1/pipelines/{id}` | Get pipeline detail |
| `DELETE` | `/api/v1/pipelines/{id}` | Delete pipeline |
| `POST` | `/api/v1/pipelines/{id}/run` | Trigger pipeline run (async) |
| `GET` | `/api/v1/pipelines/{id}/runs` | List runs for pipeline |
| `GET` | `/api/v1/pipelines/{id}/runs/{runId}` | Get run detail |
| `PUT` | `/api/v1/pipelines/{id}` | Update pipeline |
| `POST` | `/api/v1/pipelines/{id}/cancel` | Cancel running pipeline |

### Webhooks (6 endpoints, operator+)

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/api/v1/webhooks` | List all webhooks |
| `POST` | `/api/v1/webhooks` | Create webhook (returns secret + URL) |
| `GET` | `/api/v1/webhooks/{id}` | Get webhook detail (secret redacted) |
| `PUT` | `/api/v1/webhooks/{id}` | Update webhook (branch filter, auto-redeploy, provider) |
| `DELETE` | `/api/v1/webhooks/{id}` | Delete webhook |
| `GET` | `/api/v1/webhooks/{id}/deliveries` | List webhook delivery history |

### Webhook Receiver (1 endpoint, public)

| Method | Path | Description |
|--------|------|-------------|
| `POST` | `/api/v1/hooks/{id}` | Receive webhook delivery. Validates HMAC signature. Triggers async GitOps sync (returns immediately, runs in background job). |

Supported providers: GitHub (`X-Hub-Signature-256`), GitLab (`X-Gitlab-Token`), Gitea (`X-Gitea-Signature`), Generic (`X-Webhook-Signature`).

### Audit Log (1 endpoint, admin)

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/api/v1/audit` | List recent audit log entries. `?limit=50` |

### Background Jobs (2 endpoints, viewer+)

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/api/v1/jobs` | List all background jobs (newest first, max 100) |
| `GET` | `/api/v1/jobs/{id}` | Get job detail: status, output, error, duration |

Jobs are created when compose operations run with `?async=true` or when webhooks trigger GitOps sync. Completed/failed jobs are automatically cleaned up after 1 hour.

### Docker Resources (14 endpoints)

| Method | Path | Role | Description |
|--------|------|------|-------------|
| `GET` | `/api/v1/networks` | Viewer+ | List Docker networks |
| `POST` | `/api/v1/networks` | Operator+ | Create network (bridge/overlay/macvlan) |
| `DELETE` | `/api/v1/networks/{id}` | Operator+ | Remove network |
| `GET` | `/api/v1/volumes` | Viewer+ | List Docker volumes |
| `POST` | `/api/v1/volumes` | Operator+ | Create volume |
| `DELETE` | `/api/v1/volumes/{name}` | Operator+ | Remove volume |
| `POST` | `/api/v1/volumes/prune` | Admin | Prune unused volumes |
| `GET` | `/api/v1/images` | Viewer+ | List Docker images |
| `POST` | `/api/v1/images/pull` | Operator+ | Pull image by reference |
| `DELETE` | `/api/v1/images/{id}` | Operator+ | Remove image |
| `POST` | `/api/v1/images/prune` | Admin | Prune unused images |
| `GET` | `/api/v1/networks/{id}` | Viewer+ | Inspect network (full JSON) |
| `GET` | `/api/v1/volumes/{name}` | Viewer+ | Inspect volume (full JSON) |
| `GET` | `/api/v1/docker/events` | Viewer+ | Recent Docker daemon events. `?since=5m` |

### System (10 endpoints)

| Method | Path | Auth | Description |
|--------|------|------|-------------|
| `GET` | `/api/v1/system/health` | None | Health check |
| `GET` | `/api/v1/system/info` | Viewer+ | Docker engine info (version, containers, images) |
| `GET` | `/api/v1/system/version` | Viewer+ | Composer version, Go version, uptime |
| `GET` | `/api/v1/system/config` | Admin | Global config status (SSH keys, SOPS, encryption) |
| `PUT` | `/api/v1/system/config/age-key` | Admin | Set or update global age key for SOPS |
| `POST` | `/api/v1/system/config/age-key/generate` | Admin | Generate new age key pair |
| `GET` | `/api/v1/system/config/git-token` | Admin | Get global git token status |
| `PUT` | `/api/v1/system/config/git-token` | Admin | Set or remove global git access token |
| `POST` | `/api/v1/system/config/ssh-keys` | Admin | Add SSH key by pasting content |
| `DELETE` | `/api/v1/system/config/ssh-keys/{name}` | Admin | Delete an SSH key file |

### SSE Streams (5 endpoints, viewer+)

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/api/v1/sse/events` | Global domain events (stack deployed/stopped, container state) |
| `GET` | `/api/v1/sse/containers/{id}/logs` | Live container log stream. `?tail=100&since=5m` |
| `GET` | `/api/v1/sse/containers/{id}/stats` | Live CPU/memory/network/disk stats (~1/sec) |
| `GET` | `/api/v1/sse/stacks/{name}/logs` | Aggregated logs from all containers in a stack |
| `GET` | `/api/v1/sse/pipelines/{id}/runs/{runId}` | Live pipeline run step output |

### WebSocket (1 endpoint, operator+)

| Path | Description |
|------|-------------|
| `/api/v1/ws/terminal/{id}` | Interactive container shell. `?shell=/bin/sh&cols=80&rows=24` |

### Stack Templates (2 endpoints, public)

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/api/v1/templates` | List all built-in stack templates (no auth required) |
| `GET` | `/api/v1/templates/{id}` | Get template compose content |

Available templates: nginx, caddy, postgres, valkey, uptime-kuma, vaultwarden, gitea, portainer-agent, whoami, immich.

### OAuth/OIDC (2 endpoints, public)

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/api/v1/auth/oauth/{provider}` | Start OAuth flow (redirects to provider) |
| `GET` | `/api/v1/auth/oauth/{provider}/callback` | OAuth callback (creates user + session) |

Supported providers: `github`, `google`. Configure via `COMPOSER_GITHUB_CLIENT_ID` / `COMPOSER_GOOGLE_CLIENT_ID` env vars.

### OpenAPI Spec & Docs

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/openapi.json` | OpenAPI 3.1 spec (JSON) |
| `GET` | `/openapi.yaml` | OpenAPI 3.1 spec (YAML) |
| `GET` | `/docs` | Stoplight Elements interactive API docs |

## RBAC Roles

| Role | Access |
|------|--------|
| **Admin** | Everything: user/key management, all stack/pipeline/terminal/settings |
| **Operator** | Stacks CRUD, deploy/stop/restart/pull, terminal, pipelines, webhooks |
| **Viewer** | Read-only: list stacks, view containers, view logs, view stats |

## Error Format

RFC 9457 (Problem Details):

```json
{
  "status": 401,
  "title": "Unauthorized",
  "detail": "Valid session or API key required"
}
```

500 responses include the chi request ID so operators can correlate with server logs:

```json
{
  "status": 500,
  "title": "Internal Server Error",
  "detail": "an internal error occurred (request_id: abc123-xyz)"
}
```

## Validation Constraints

Every input DTO declares:

- `maxLength` on free-form strings (compose content capped at 512 KB, `.env` at 256 KB, tokens/keys at 512–16384 bytes)
- `pattern` on IDs with known formats (stack names `^[A-Za-z0-9_-]+$`, commit SHAs `^[0-9a-fA-F]+$`, webhook IDs `^wh_[0-9a-f]+$`)
- `format` on email and timestamp fields
- `enum` on role, auth method, sync status, step type, trigger type, container status/health, event type

Huma caps request bodies at 1 MB per operation by default. Larger payloads return HTTP 413.

## Partial Updates

- `PUT /api/v1/webhooks/{id}` uses pointer semantics (`*string`, `*bool`) — omit a field to keep the current value, send empty string (`""`) to clear.
- `PUT /api/v1/stacks/{name}/credentials` uses full-replace semantics — send every field you want set; empty string clears.
- `PUT /api/v1/users/{id}` uses empty-string-means-keep semantics for email and role (both are required fields and cannot be cleared).

## Webhook Secret Lifecycle

`POST /api/v1/webhooks` returns a `WebhookCreatedOutput` with the plaintext HMAC secret — shown ONCE. Subsequent reads (`GET`, `PUT`) return a `WebhookOutput` with the secret redacted to `****<last-4-chars>`. This mirrors the API key pattern.
