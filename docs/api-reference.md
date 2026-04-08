# API Reference

Composer exposes a REST API with auto-generated OpenAPI 3.1 spec.

**Live spec:** `GET /openapi.json` or `GET /openapi.yaml`

**Total endpoints:** 53

## Authentication

All endpoints except health, bootstrap, login, OpenAPI spec, and webhook receiver require authentication via session cookie or API key.

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

### API Keys (3 endpoints, operator+)

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/api/v1/keys` | List API keys (secrets redacted) |
| `POST` | `/api/v1/keys` | Create key (plaintext shown once!) |
| `DELETE` | `/api/v1/keys/{id}` | Revoke key |

### Stacks (9 endpoints)

| Method | Path | Role | Description |
|--------|------|------|-------------|
| `GET` | `/api/v1/stacks` | Viewer+ | List all stacks with status |
| `POST` | `/api/v1/stacks` | Operator+ | Create stack |
| `GET` | `/api/v1/stacks/{name}` | Viewer+ | Get stack detail + containers |
| `PUT` | `/api/v1/stacks/{name}` | Operator+ | Update compose content |
| `DELETE` | `/api/v1/stacks/{name}` | Operator+ | Delete stack. `?remove_volumes=true` |
| `POST` | `/api/v1/stacks/{name}/up` | Operator+ | Deploy (docker compose up) |
| `POST` | `/api/v1/stacks/{name}/down` | Operator+ | Stop (docker compose down) |
| `POST` | `/api/v1/stacks/{name}/restart` | Operator+ | Restart all services |
| `POST` | `/api/v1/stacks/{name}/pull` | Operator+ | Pull latest images |

### Containers (5 endpoints)

| Method | Path | Role | Description |
|--------|------|------|-------------|
| `GET` | `/api/v1/containers` | Viewer+ | List all containers |
| `GET` | `/api/v1/containers/{id}` | Viewer+ | Get container detail |
| `POST` | `/api/v1/containers/{id}/start` | Operator+ | Start container |
| `POST` | `/api/v1/containers/{id}/stop` | Operator+ | Stop container |
| `POST` | `/api/v1/containers/{id}/restart` | Operator+ | Restart container |

### Git Operations (3 endpoints, requires git-backed stack)

| Method | Path | Role | Description |
|--------|------|------|-------------|
| `POST` | `/api/v1/stacks/{name}/sync` | Operator+ | Git pull + detect compose changes |
| `GET` | `/api/v1/stacks/{name}/git/log` | Viewer+ | Commit history (filtered to compose file). `?limit=20` |
| `GET` | `/api/v1/stacks/{name}/git/status` | Viewer+ | Sync status, branch, last commit |

### Pipelines (7 endpoints, operator+)

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/api/v1/pipelines` | List pipelines |
| `POST` | `/api/v1/pipelines` | Create pipeline (steps + triggers) |
| `GET` | `/api/v1/pipelines/{id}` | Get pipeline detail |
| `DELETE` | `/api/v1/pipelines/{id}` | Delete pipeline |
| `POST` | `/api/v1/pipelines/{id}/run` | Trigger pipeline run (async) |
| `GET` | `/api/v1/pipelines/{id}/runs` | List runs for pipeline |
| `GET` | `/api/v1/pipelines/{id}/runs/{runId}` | Get run detail |

### Webhooks (4 endpoints, operator+)

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/api/v1/webhooks` | List all webhooks |
| `POST` | `/api/v1/webhooks` | Create webhook (returns secret + URL) |
| `GET` | `/api/v1/webhooks/{id}` | Get webhook detail + secret |
| `DELETE` | `/api/v1/webhooks/{id}` | Delete webhook |

### Webhook Receiver (1 endpoint, public)

| Method | Path | Description |
|--------|------|-------------|
| `POST` | `/api/v1/hooks/{id}` | Receive webhook delivery. Validates HMAC signature. Triggers GitOps sync. |

Supported providers: GitHub (`X-Hub-Signature-256`), GitLab (`X-Gitlab-Token`), Gitea (`X-Gitea-Signature`), Generic (`X-Webhook-Signature`).

### System (1 endpoint)

| Method | Path | Auth | Description |
|--------|------|------|-------------|
| `GET` | `/api/v1/system/health` | None | Health check |

### SSE Streams (3 endpoints, viewer+)

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/api/v1/sse/events` | Global domain events (stack deployed/stopped, container state) |
| `GET` | `/api/v1/sse/containers/{id}/logs` | Live container log stream. `?tail=100&since=5m` |
| `GET` | `/api/v1/sse/containers/{id}/stats` | Live CPU/memory/network/disk stats (~1/sec) |

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

### OpenAPI Spec

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/openapi.json` | OpenAPI 3.1 spec (JSON) |
| `GET` | `/openapi.yaml` | OpenAPI 3.1 spec (YAML) |

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
