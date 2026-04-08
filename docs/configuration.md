# Configuration

All configuration is via `COMPOSER_*` environment variables. No config files, no CLI flags.

## Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `COMPOSER_PORT` | `8080` | HTTP listen port |
| `COMPOSER_DB_URL` | `postgres://composer:composer@localhost:5432/composer?sslmode=disable` | PostgreSQL connection URL |
| `COMPOSER_VALKEY_URL` | `valkey://localhost:6379` | Valkey/Redis connection URL for session caching. Empty = caching disabled |
| `COMPOSER_STACKS_DIR` | `/opt/stacks` | Directory where compose stack files are stored |
| `COMPOSER_DATA_DIR` | `/opt/composer` | Directory for app data (SSH keys, encryption keys) |
| `COMPOSER_DOCKER_HOST` | (auto-detect) | Docker/Podman socket path. Empty = auto-detect |
| `COMPOSER_LOG_LEVEL` | `info` | Log level: `debug`, `info`, `warn`, `error` |
| `COMPOSER_LOG_FORMAT` | `json` | Log format: `json` (production), `console` (development) |
| `COMPOSER_COOKIE_SECURE` | `true` | Set session cookie Secure flag. Set to `false` for local dev without TLS |
| `COMPOSER_NOTIFY_URL` | (empty) | Webhook URL for deploy/failure notifications. Empty = disabled |
| `COMPOSER_SLACK_WEBHOOK` | (empty) | Slack incoming webhook URL. Empty = disabled |
| `COMPOSER_GITHUB_CLIENT_ID` | (empty) | GitHub OAuth app client ID. Empty = disabled |
| `COMPOSER_GITHUB_CLIENT_SECRET` | (empty) | GitHub OAuth app client secret |
| `COMPOSER_GOOGLE_CLIENT_ID` | (empty) | Google OAuth client ID. Empty = disabled |
| `COMPOSER_GOOGLE_CLIENT_SECRET` | (empty) | Google OAuth client secret |
| `COMPOSER_OAUTH_CALLBACK_URL` | `http://localhost:8080` | Base URL for OAuth callbacks |
| `COMPOSER_SESSION_SECRET` | (auto) | Secret key for OAuth session store |

## Container User Mapping (PUID/PGID)

For NAS platforms (Unraid, TrueNAS) and proper file ownership:

| Variable | Default | Description |
|----------|---------|-------------|
| `PUID` | `1000` | User ID for the composer process inside the container |
| `PGID` | `1000` | Group ID for the composer process inside the container |
| `DOCKER_GID` | (auto-detect) | GID of the Docker socket on the host. Auto-detected from the mounted socket if not set |

### Common PUID/PGID values

| Platform | PUID | PGID |
|----------|------|------|
| Linux (default user) | `1000` | `1000` |
| Unraid | `99` | `100` |
| TrueNAS SCALE | `568` | `568` (apps user) |

### How Docker socket access works

The entrypoint script:
1. Adjusts the internal `composer` user to match `PUID:PGID`
2. Auto-detects the GID of the mounted Docker socket (or uses `DOCKER_GID` if set)
3. Adds the `composer` user to a group with that GID
4. Drops privileges via `su-exec` to the `composer` user
5. Runs `composerd` as PID 1

This means you never need to run the container as root or manually match Docker group IDs.

## Docker Socket Auto-Detection

If `COMPOSER_DOCKER_HOST` is empty, Composer checks these sockets in order:

1. `$DOCKER_HOST` environment variable
2. `/var/run/docker.sock` (Docker default)
3. `/run/podman/podman.sock` (Podman rootful)
4. `$XDG_RUNTIME_DIR/podman/podman.sock` (Podman rootless)

## Example: Docker Compose

```yaml
services:
  composer:
    image: ghcr.io/erfianugrah/composer:latest
    ports:
      - "8080:8080"
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock
      - stacks:/opt/stacks
    environment:
      PUID: 1000
      PGID: 1000
      COMPOSER_DB_URL: "postgres://composer:secret@postgres:5432/composer?sslmode=disable"
      COMPOSER_LOG_LEVEL: info
    depends_on:
      postgres:
        condition: service_healthy

  postgres:
    image: postgres:17-alpine
    environment:
      POSTGRES_USER: composer
      POSTGRES_PASSWORD: secret
      POSTGRES_DB: composer
    volumes:
      - pgdata:/var/lib/postgresql/data
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U composer"]
      interval: 5s
      timeout: 3s
      retries: 5

volumes:
  stacks:
  pgdata:
```

## Example: Unraid

In the Unraid Docker template:

| Field | Value |
|-------|-------|
| Repository | `ghcr.io/erfianugrah/composer:latest` |
| Port Mapping | `8080` -> `8080` |
| Path: /opt/stacks | `/mnt/user/appdata/composer/stacks` |
| Path: docker.sock | `/var/run/docker.sock` |
| Variable: PUID | `99` |
| Variable: PGID | `100` |
| Variable: COMPOSER_DB_URL | `postgres://composer:pass@postgres:5432/composer?sslmode=disable` |

Postgres needs to be running separately (another Unraid container) or use a managed database.
