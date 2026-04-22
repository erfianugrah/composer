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
- **SSH key files** on disk at `/home/composer/.ssh/` are encrypted at rest on startup (see [SSH Key Files](#ssh-key-files) for the targeting policy)
- **Per-stack SSH key files** can be specified via the `ssh_key_file` field (auth method `ssh_file`), allowing individual stacks to use dedicated keys
- **`.env` files** are written with `0600` permissions (owner read/write only)
- A unique 12-byte nonce is generated per encryption operation
- Encrypted values are prefixed with `enc:` for identification (both DB strings and key files)
- **Backwards compatible**: unencrypted data from before encryption is read normally

### SSH Key Files

On every startup Composer scans a list of SSH directories for plaintext private key files and encrypts them in place. When go-git needs an SSH key for clone/pull operations, the file is transparently decrypted in memory.

#### Targeting policy

The scan is **explicit and opt-in** — it never infers a target from `$HOME`:

- **Default target**: `/home/composer/.ssh` (the canonical container path). Every composer Docker image runs as the `composer` user with that HOME, so this covers the production case. On a developer machine the directory doesn't exist, so the scan is a no-op.
- **Additional targets**: set `COMPOSER_SSH_DIR=/path/to/dir` to have the scan also cover a custom location. Use this when running composerd outside the official container with SSH material you want encrypted.

This policy was tightened after an incident where running `composerd` ad-hoc on a developer workstation encrypted the operator's personal `~/.ssh` directory. The old behaviour unconditionally scanned `$HOME/.ssh` and `/home/composer/.ssh`; the new behaviour only touches paths the operator asked for.

#### What gets encrypted

A file is touched only if **both** conditions hold:

1. Its filename is not on the skip list (`config`, `authorized_keys`, anything starting with `known_hosts`, anything ending in `.pub`, already-encrypted files with an `enc:` prefix, empty files)
2. Its first line contains a recognised private-key BEGIN marker (`-----BEGIN OPENSSH PRIVATE KEY-----`, `-----BEGIN RSA PRIVATE KEY-----`, etc.)

This two-step check means arbitrary non-key files a user drops in the SSH directory (notes, backups, random text) are left alone even if their filename doesn't match a skip rule.

#### Mounting host keys

Mounting an SSH key file into the container (for example via `-v ~/.ssh:/home/composer/.ssh:rw`) will cause those keys to be encrypted on first boot. Mount **read-only** if you want to keep the plaintext intact on the host:

```yaml
volumes:
  - ${HOME}/.ssh:/home/composer/.ssh:ro
```

#### Recovery from accidental encryption

If a key got encrypted and you still have the corresponding `encryption.key` file, use `cmd/decryptssh` to reverse it:

```sh
COMPOSER_DATA_DIR=/path/to/data go run ./cmd/decryptssh -- ~/.ssh/id_foo
# or
COMPOSER_ENCRYPTION_KEY='...' go run ./cmd/decryptssh -- ~/.ssh/id_foo
```

The tool accepts `--dry-run` to preview, writes to a `.tmp` sibling and renames only on success so a mid-write crash leaves the encrypted original intact.

### Key Resolution

Key resolution (in priority order):
1. `COMPOSER_ENCRYPTION_KEY` env var (explicit override, SHA-256 derived)
2. `COMPOSER_DATA_DIR/encryption.key` file (auto-generated on first run)
3. If neither exists, a 32-byte random key is generated, saved to the key file, and used

The key file is created with `0600` permissions (owner-read only). Back it up -- losing it means encrypted credentials and SSH keys can't be decrypted.

### SOPS/age Encrypted Secrets

Composer can transparently decrypt SOPS-encrypted `.env` files and compose YAML files before deployment:

- **Detection**: checks for SOPS markers in dotenv (`sops_version=`), YAML (`sops:` key), and JSON (`"sops"` key) formats
- **Decryption**: shells out to the bundled `sops` binary (v3.12.2) for reliable, format-aware decryption
- **Timing**: `.env` and compose files are decrypted in-place after git pull and before `docker compose up`
- **Age key resolution** (per-stack overrides global):
  1. Per-stack `age_key` field in git credentials (stored encrypted in DB)
  2. `COMPOSER_SOPS_AGE_KEY` env var
  3. `SOPS_AGE_KEY` env var (standard SOPS convention)
  4. `SOPS_AGE_KEYS` env var (multi-line format with comments)
  5. `COMPOSER_SOPS_AGE_KEY_FILE` / `SOPS_AGE_KEY_FILE` env var
  6. `COMPOSER_DATA_DIR/age.key` file
  7. `~/.config/sops/age/keys.txt` (standard SOPS location)
- No auto-generation of age keys -- the user must bring their own or explicitly generate one
- Decrypted files are written with mode `0600`

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
| Create pipelines (shell steps) | Yes | No | No |
| Run/update/delete pipelines | Yes | Yes | No |
| Manage users | Yes | No | No |
| Manage API keys | Yes | Yes (own) | No |
| System config (SSH keys, tokens) | Yes | No | No |
| View audit log | Yes | No | No |

RBAC is enforced at the handler level via `middleware.CheckRole()`. Pipeline creation requires admin role because pipelines can execute shell commands on the host. Pipeline updates with shell/docker steps also require admin.

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
- `Content-Security-Policy: default-src 'self'; script-src 'self' 'unsafe-inline'; style-src 'self' 'unsafe-inline'; ...`
- `X-Content-Type-Options: nosniff`
- `X-Frame-Options: DENY`
- `X-XSS-Protection: 0` (modern guidance: disabled to avoid IE vulnerabilities)
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
