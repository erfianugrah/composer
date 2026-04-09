# Deployment

Composer ships as a self-contained Docker image. The image bundles all required binaries:
- `composerd` (Go binary)
- `docker` CLI v28
- `docker compose` plugin v2.40
- `docker buildx` plugin
- `sops` v3.12.2 (SOPS encrypted secret decryption)
- `git` + `openssh-client`

The host only needs a container runtime with a socket. Nothing else needs to be installed.

## Single Container (SQLite -- simplest)

No external database required. SQLite is embedded and stores data in `/opt/composer/composer.db`.

```bash
docker run -d --name composer -p 8080:8080 \
  -v /var/run/docker.sock:/var/run/docker.sock \
  -v composer_data:/opt/composer \
  -v composer_stacks:/opt/stacks \
  --security-opt no-new-privileges:true \
  ghcr.io/erfianugrah/composer:latest
```

Access at `http://localhost:8080`.

## Docker Compose with PostgreSQL

For production with PostgreSQL + Valkey caching:

```bash
mkdir -p /opt/composer && cd /opt/composer
curl -O https://raw.githubusercontent.com/erfianugrah/composer/main/deploy/compose.yaml

# Edit compose.yaml to set POSTGRES_PASSWORD
docker compose up -d
```

Includes Composer + PostgreSQL + Valkey. Access at `http://localhost:8080`.

## Unraid

### Single Container (recommended)

Only one container needed -- Composer with built-in SQLite:

1. Go to **Docker > Add Container**
2. Set:
   - **Name:** `composer`
   - **Repository:** `ghcr.io/erfianugrah/composer:latest`
   - **Network Type:** Bridge
   - **Extra Parameters:** `--security-opt no-new-privileges:true`
3. Add port mapping:
   - Container Port: `8080`, Host Port: `8080`, Connection Type: TCP
4. Add paths:
   - `/var/run/docker.sock` -> `/var/run/docker.sock` (Mode: `rw`) -- Docker socket
   - `/mnt/user/appdata/composer/data` -> `/opt/composer` (Mode: `rw`) -- Database + keys
   - `/mnt/user/appdata/composer/stacks` -> `/opt/stacks` (Mode: `rw`) -- Stack files
   - `/mnt/user/appdata/composer/ssh` -> `/home/composer/.ssh` (Mode: `rw`) -- SSH keys (optional)
5. Add variables:
   - `PUID` = `99`
   - `PGID` = `100`
   - `COMPOSER_COOKIE_SECURE` = `false` (set to `true` if behind HTTPS reverse proxy)
6. **Leave `COMPOSER_DB_URL` empty** -- uses SQLite automatically
7. Click **Apply**
8. Open `http://[UNRAID-IP]:8080` and create your admin account

An XML template is available at `deploy/unraid/composer.xml` for use in Community Applications.

### With PostgreSQL (optional)

If you prefer PostgreSQL, create a Postgres container first, then set `COMPOSER_DB_URL`:

1. Create a PostgreSQL container (`postgres:17-alpine`) with:
   - `POSTGRES_USER` = `composer`, `POSTGRES_PASSWORD` = `changeme`, `POSTGRES_DB` = `composer`
   - Volume: `/mnt/user/appdata/composer/postgres` -> `/var/lib/postgresql/data`
2. Note the Postgres container's IP address
3. In the Composer container, set:
   - `COMPOSER_DB_URL` = `postgres://composer:changeme@172.17.0.X:5432/composer?sslmode=disable`

### Valkey Cache (optional)

For session caching, add a Valkey container and set `COMPOSER_VALKEY_URL` = `valkey://[VALKEY-IP]:6379`.

### XML Templates

A pre-built Unraid XML template is at `deploy/unraid/composer.xml`.
Download and place in `/boot/config/plugins/dockerMan/templates-user/` on your Unraid server.

### Via Docker Compose (if Compose Manager plugin is installed)

If you have the Docker Compose Manager plugin:

```yaml
services:
  composer:
    image: ghcr.io/erfianugrah/composer:latest
    ports:
      - "8080:8080"
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock
      - /mnt/user/appdata/composer/data:/opt/composer
      - /mnt/user/appdata/composer/stacks:/opt/stacks
    environment:
      PUID: "99"
      PGID: "100"
    # COMPOSER_DB_URL not set = SQLite (default)
    restart: unless-stopped
```

## TrueNAS SCALE (24.10+)

TrueNAS SCALE 24.10 ("Electric Eel") uses Docker natively.

### Via Custom App

1. Go to **Apps > Discover Apps > Custom App**
2. Enter the image: `ghcr.io/erfianugrah/composer:latest`
3. Map port `8080`
4. Add host path for Docker socket: `/var/run/docker.sock`
5. Add storage for stacks
6. Set environment variables:
   - `PUID` = `568` (apps user on TrueNAS)
   - `PGID` = `568`
   - Leave `COMPOSER_DB_URL` empty for SQLite, or set to a Postgres connection string

### Via Docker Compose (SSH)

SSH into TrueNAS and use the standard compose file:

```bash
docker compose -f compose.yaml up -d
```

## Bare Metal (Linux)

### With Docker

```bash
# Prerequisites: Docker + Docker Compose V2 + PostgreSQL

# Download binary
curl -L https://github.com/erfianugrah/composer/releases/latest/download/composerd-linux-amd64 -o /usr/local/bin/composerd
chmod +x /usr/local/bin/composerd

# Run (needs a Postgres instance)
COMPOSER_PORT=8080 \
COMPOSER_DB_URL="postgres://user:pass@localhost:5432/composer?sslmode=disable" \
COMPOSER_STACKS_DIR="/opt/stacks" \
COMPOSER_LOG_FORMAT=console \
composerd
```

The bare binary requires:
- `docker` CLI and `docker compose` plugin installed on the host
- `git` installed if using git-backed stacks
- No database setup needed (uses SQLite by default in `$COMPOSER_DATA_DIR/composer.db`)

### With Podman

```bash
# Podman socket must be active
systemctl --user enable --now podman.socket

# Composer auto-detects the Podman socket
COMPOSER_PORT=8080 \
COMPOSER_LOG_FORMAT=console \
composerd
# Uses SQLite by default. Set COMPOSER_DB_URL for Postgres.
```

Or run as a Podman container (same image, just use `podman` instead of `docker`):

```bash
podman run -d \
  --name composer \
  -p 8080:8080 \
  -v /run/podman/podman.sock:/var/run/docker.sock \
  -v composer-data:/opt/composer \
  -v composer-stacks:/opt/stacks \
  -e PUID=$(id -u) \
  -e PGID=$(id -g) \
  ghcr.io/erfianugrah/composer:latest
# Uses SQLite by default. Set COMPOSER_DB_URL for Postgres.
```

## Networking Notes

Composer needs to reach:
1. **Docker socket** -- to manage containers and compose stacks
2. **Database** -- SQLite (local file, no network needed) or PostgreSQL (if configured)
3. **Git remotes** (optional) -- for git-backed stacks (HTTPS or SSH)

In Docker Compose, services communicate by name (`postgres:5432`). In standalone setups, use host networking or explicit IPs.

## Updating

```bash
# Docker Compose
docker compose pull && docker compose up -d

# Unraid
# Update the image in the Docker template UI

# Bare metal binary
curl -L https://github.com/erfianugrah/composer/releases/latest/download/composerd-linux-amd64 -o /usr/local/bin/composerd
systemctl restart composerd  # if using systemd
```

Database migrations run automatically on startup via goose. No manual migration steps needed.
