# API Reference

Composer exposes a REST API with auto-generated OpenAPI 3.1 spec.

**Live spec:** `GET /openapi.json` or `GET /openapi.yaml`

## Authentication

All endpoints except health check and bootstrap require authentication via session cookie or API key.

### Session Cookie

Login returns a `Set-Cookie: composer_session=...` header. Include it in subsequent requests.

```bash
# Login
curl -c cookies.txt -X POST /api/v1/auth/login \
  -H "Content-Type: application/json" \
  -d '{"email":"user@example.com","password":"password"}'

# Authenticated request
curl -b cookies.txt /api/v1/stacks
```

### API Key

Pass in `Authorization: Bearer ck_...` header or `X-API-Key: ck_...` header.

```bash
curl -H "Authorization: Bearer ck_your_key_here" /api/v1/stacks
```

## Endpoints

### Auth

| Method | Path | Auth | Description |
|--------|------|------|-------------|
| `POST` | `/api/v1/auth/bootstrap` | None | Create first admin user (disabled after first user exists) |
| `POST` | `/api/v1/auth/login` | None | Login with email + password, returns session cookie |
| `POST` | `/api/v1/auth/logout` | Session | Destroy session, clear cookie |
| `GET` | `/api/v1/auth/session` | Session/Key | Validate current session, return user info |

### Stacks

| Method | Path | Role | Description |
|--------|------|------|-------------|
| `GET` | `/api/v1/stacks` | Viewer+ | List all stacks with status |
| `POST` | `/api/v1/stacks` | Operator+ | Create a new stack |
| `GET` | `/api/v1/stacks/{name}` | Viewer+ | Get stack detail (containers, compose, git config) |
| `PUT` | `/api/v1/stacks/{name}` | Operator+ | Update compose content |
| `DELETE` | `/api/v1/stacks/{name}` | Operator+ | Delete stack. Query: `?remove_volumes=true` |
| `POST` | `/api/v1/stacks/{name}/up` | Operator+ | Deploy (docker compose up -d) |
| `POST` | `/api/v1/stacks/{name}/down` | Operator+ | Stop (docker compose down) |
| `POST` | `/api/v1/stacks/{name}/restart` | Operator+ | Restart all services |
| `POST` | `/api/v1/stacks/{name}/pull` | Operator+ | Pull latest images |

### System

| Method | Path | Auth | Description |
|--------|------|------|-------------|
| `GET` | `/api/v1/system/health` | None | Health check (always public) |

### OpenAPI

| Method | Path | Auth | Description |
|--------|------|------|-------------|
| `GET` | `/openapi.json` | None | OpenAPI 3.1 spec (JSON) |
| `GET` | `/openapi.yaml` | None | OpenAPI 3.1 spec (YAML) |

## SSE (Server-Sent Events)

Real-time streaming endpoints. Connect with `EventSource` or any SSE client.

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/api/v1/sse/events` | Global domain events (stack deployed/stopped, container state changes) |
| `GET` | `/api/v1/sse/containers/{id}/logs` | Live container log stream. Query: `?tail=100&since=5m` |

### SSE Event Types

```
event: stack.deployed
data: {"name":"web-app","ts":"2026-04-08T10:00:00Z"}

event: stack.stopped
data: {"name":"web-app","ts":"2026-04-08T10:01:00Z"}

event: container.state
data: {"container_id":"abc123","stack":"web-app","old":"running","new":"exited","ts":"..."}

event: log
data: {"container_id":"abc123","stream":"stdout","message":"Server started","ts":"..."}
```

## WebSocket (Terminal)

Interactive container shell over WebSocket.

| Path | Auth | Description |
|------|------|-------------|
| `/api/v1/ws/terminal/{id}` | Operator+ | Interactive exec session. Query: `?shell=/bin/sh&cols=80&rows=24` |

### Protocol

- **Client -> Server (binary):** Raw stdin bytes (keystrokes)
- **Server -> Client (binary):** Raw stdout/stderr bytes
- **Client -> Server (text):** JSON control messages: `{"type":"resize","cols":120,"rows":40}`

## RBAC Roles

| Role | Access |
|------|--------|
| **Admin** | Everything: user management, settings, all stack/pipeline/terminal operations |
| **Operator** | Stacks CRUD, deploy/stop/restart/pull, terminal exec, pipelines |
| **Viewer** | Read-only: list stacks, view containers, view logs |

## Error Format

Errors follow RFC 9457 (Problem Details):

```json
{
  "status": 401,
  "title": "Unauthorized",
  "detail": "Valid session or API key required"
}
```
