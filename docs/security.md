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
| **No new privileges** | `security_opt: no-new-privileges:true` prevents privilege escalation |
| **Container scope validation** | Container start/stop/restart restricted to Docker Compose-managed containers |
| **Compose exec allowlist** | Stack console only permits safe subcommands (ps, logs, top, config, images, exec, etc.) |

### Recommendations

1. **Run Composer behind a reverse proxy with TLS** -- see [reverse-proxy.md](reverse-proxy.md) for Caddy, Traefik, and nginx configs
2. **Set `COMPOSER_ENCRYPTION_KEY`** -- encrypts git credentials and webhook secrets at rest
3. **Use strong passwords** -- the bootstrap password becomes the admin account
4. **Use API keys with minimal roles** -- for automation, create Operator or Viewer keys, not Admin

## Encryption at Rest

Encryption is **automatic** -- no configuration needed:

- **Git credentials** (tokens, SSH keys, passwords) are encrypted with AES-256-GCM before storage in the database
- **Webhook secrets** are encrypted with AES-256-GCM before storage in the database
- **SSH key files** on disk (`~/.ssh/`, `/home/composer/.ssh/`) are encrypted at rest on startup
- A unique 12-byte nonce is generated per encryption operation
- Encrypted values are prefixed with `enc:` for identification (both DB strings and key files)
- **Backwards compatible**: unencrypted data from before encryption is read normally

### SSH Key Files

On every startup, Composer scans SSH directories for plaintext private key files and encrypts them in place. Public keys (`.pub`), `known_hosts`, and `config` files are left untouched. When go-git needs an SSH key for clone/pull operations, the key file is transparently decrypted in memory.

This means SSH key files mounted into the container (e.g., via `-v ~/.ssh:/home/composer/.ssh:ro`) will be encrypted after the first boot. If you need the original plaintext keys, keep a copy outside the container.

### Key Resolution

Key resolution (in priority order):
1. `COMPOSER_ENCRYPTION_KEY` env var (explicit override, SHA-256 derived)
2. `COMPOSER_DATA_DIR/encryption.key` file (auto-generated on first run)
3. If neither exists, a 32-byte random key is generated, saved to the key file, and used

The key file is created with `0600` permissions (owner-read only). Back it up -- losing it means encrypted credentials and SSH keys can't be decrypted.

## Authentication

### Session Cookies

- 32 bytes of `crypto/rand`, base64url-encoded
- **Hashed with SHA-256 before storage** -- database leak does not expose usable tokens
- `HttpOnly`, `SameSite=Lax`, `Path=/`
- 7-day TTL with background cleanup every 5 minutes
- Session fixation prevention: old sessions are revoked on new login
- Role changes invalidate existing sessions

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
| Stack console (compose commands) | Yes | Yes | No |
| Create/run pipelines | Yes | No | No |
| Manage users | Yes | No | No |
| Manage API keys | Yes | Yes (own) | No |
| View audit log | Yes | No | No |

RBAC is enforced at the handler level via `middleware.CheckRole()`. Pipeline creation requires admin role because pipelines can execute shell commands on the host.

## CSRF Protection

CSRF protection is enforced via `X-Requested-With: XMLHttpRequest` header requirement on mutating requests (POST/PUT/DELETE) that use cookie-based authentication. API key and webhook requests are exempt. `SameSite=Lax` cookie attribute provides additional browser-level protection.

## OAuth/OIDC Security

When OAuth is enabled (via `COMPOSER_GITHUB_CLIENT_ID` or `COMPOSER_GOOGLE_CLIENT_ID`):

- **Auto-creation**: Users who authenticate via OAuth are automatically created with the `viewer` role. If no users exist, the first OAuth user gets `admin`.
- **Provider tracking**: The `auth_provider` column records whether a user authenticated via `local`, `github`, or `google`
- **Password**: OAuth users get a cryptographically random placeholder password (64 bytes from `crypto/rand`)
- **Session**: OAuth sessions use the same SHA-256 hashed storage as password-based sessions

## Security Headers

All responses include:
- `Content-Security-Policy: default-src 'self'; script-src 'self' 'unsafe-inline' https://unpkg.com; ...`
- `X-Content-Type-Options: nosniff`
- `X-Frame-Options: DENY`
- `X-XSS-Protection: 1; mode=block`
- `Referrer-Policy: strict-origin-when-cross-origin`
- `Permissions-Policy: camera=(), microphone=(), geolocation=()`
- `Strict-Transport-Security` (when behind TLS proxy)

## Network Security

- **OpenAPI spec** (`/openapi.json`, `/docs`) requires authentication (viewer+ role)
- **Proxy headers** (`X-Real-IP`, `X-Forwarded-For`) only trusted when `COMPOSER_TRUSTED_PROXIES` is set
- **Rate limiting** uses `RemoteAddr` directly (not spoofable headers) unless behind a trusted proxy

## Pipeline Security

Pipeline `shell_command` steps execute arbitrary commands on the host. Mitigations:
- **Admin-only**: Pipeline creation requires admin role
- **Scrubbed environment**: Shell commands run with a clean environment (no inherited DB URLs, API tokens, or OAuth secrets)
- **PATH restricted**: Only standard system directories
- **HTTP requests**: Use Go's `net/http` (not curl), only `http://` and `https://` schemes allowed

## Reporting Vulnerabilities

If you find a security vulnerability, please report it privately via GitHub Security Advisories rather than opening a public issue.
