# Security

## Docker Socket Access

Composer requires access to the Docker (or Podman) socket to manage containers and compose stacks. This is inherent to its purpose -- it's a Docker management tool.

### What this means

The Docker socket grants the ability to:
- Create, start, stop, remove containers
- Pull images
- Execute commands inside containers
- Access Docker volumes and networks

This is equivalent to root access on the host. There is no way to meaningfully restrict this for a Docker management tool that needs to run `docker compose up/down`.

### Mitigations we apply

| Mitigation | How |
|------------|-----|
| **Non-root process** | `composerd` runs as the `composer` user (not root), via `su-exec` privilege drop |
| **Minimal capabilities** | `cap_drop: ALL` + only CHOWN, SETUID, SETGID, DAC_OVERRIDE (for entrypoint PUID/PGID setup) |
| **Read-only rootfs** | Container filesystem is immutable (`read_only: true`). Only `/tmp` is writable (tmpfs) |
| **No new privileges** | `security_opt: no-new-privileges:true` prevents privilege escalation |
| **No `:ro` on socket** | We intentionally do NOT use `:ro` on the Docker socket mount -- it's security theater on Unix sockets (the `read()` flag doesn't restrict `sendmsg()`/`recvmsg()` which is how sockets communicate) |

### Recommendations

1. **Run Composer on a trusted network** -- don't expose port 8080 to the internet without a reverse proxy with TLS. See [reverse-proxy.md](reverse-proxy.md) for Caddy, Traefik, and nginx configs
2. **Use strong passwords** -- the bootstrap password becomes the admin account
3. **Use API keys with minimal roles** -- for automation, create Operator or Viewer keys, not Admin

## Authentication

### Session Cookies

- 32 bytes of `crypto/rand`, base64url-encoded
- Stored in PostgreSQL (persistent across restarts)
- `HttpOnly`, `SameSite=Lax`, `Path=/`
- 24-hour TTL with background cleanup every 5 minutes
- Session fixation prevention: old sessions are revoked on new login

### API Keys

- Format: `ck_` prefix + 32 random hex bytes (67 characters total)
- Only the SHA-256 hash is stored in the database -- the plaintext key is shown once on creation and never again
- Compared using constant-time comparison (HMAC-based)
- Optional expiry date
- Each key has an assigned role (Admin, Operator, or Viewer)

### Password Security

- Hashed with bcrypt (cost 12)
- Minimum 8 characters, maximum 72 bytes (bcrypt limit)
- Constant-time comparison for all credential checks
- Failed login attempts don't reveal whether the email exists

## RBAC

Three roles in a strict hierarchy:

```
Admin > Operator > Viewer
```

| Permission | Admin | Operator | Viewer |
|-----------|-------|----------|--------|
| View stacks, containers, logs | Yes | Yes | Yes |
| Create/update/delete stacks | Yes | Yes | No |
| Deploy/stop/restart/pull | Yes | Yes | No |
| Terminal exec | Yes | Yes | No |
| Manage users | Yes | No | No |
| Manage API keys | Yes | Yes (own) | No |
| System settings | Yes | No | No |

RBAC is enforced at the handler level via `middleware.CheckRole()`. The WebSocket terminal endpoint additionally uses chi's `RequireRole` middleware.

## CSRF Protection

Mutating API requests from cookie-based sessions should include an `X-Requested-With` header (planned -- not yet enforced).

## Security Headers

Planned for Phase 1 completion:
- `X-Content-Type-Options: nosniff`
- `X-Frame-Options: DENY`
- `Content-Security-Policy`
- `Strict-Transport-Security` (when behind TLS)

## Reporting Vulnerabilities

If you find a security vulnerability, please report it privately via GitHub Security Advisories rather than opening a public issue.
