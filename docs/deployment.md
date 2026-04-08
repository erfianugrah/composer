# Deployment

Composer ships as a self-contained Docker image. The image bundles all required binaries:
- `composerd` (8MB Go binary)
- `docker` CLI v28
- `docker compose` plugin v2.40
- `docker buildx` plugin
- `git` + `openssh-client`

The host only needs a container runtime with a socket. Nothing else needs to be installed.

## Docker Compose (recommended)

```bash
mkdir -p /opt/composer && cd /opt/composer
curl -O https://raw.githubusercontent.com/erfianugrah/composer/main/deploy/compose.yaml

# Edit compose.yaml to set POSTGRES_PASSWORD
docker compose up -d
```

Includes Composer + PostgreSQL. Access at `http://localhost:8080`.

## Unraid

### Via Docker Template (Community Apps)

1. In the Unraid web UI, go to **Docker > Add Container**
2. Set:
   - **Repository:** `ghcr.io/erfianugrah/composer:latest`
   - **Network Type:** Bridge
   - **Port:** `8080` -> `8080`
3. Add paths:
   - **Docker Socket:** Host: `/var/run/docker.sock`, Container: `/var/run/docker.sock`, Mode: `rw`
   - **Stacks:** Host: `/mnt/user/appdata/composer/stacks`, Container: `/opt/stacks`, Mode: `rw`
4. Add variables:
   - `PUID` = `99`
   - `PGID` = `100`
   - `COMPOSER_DB_URL` = `postgres://composer:password@postgres-container-ip:5432/composer?sslmode=disable`
   - `COMPOSER_LOG_LEVEL` = `info`
5. Click **Apply**

### Via Docker Compose (requires Compose Manager plugin or CLI)

```yaml
services:
  composer:
    image: ghcr.io/erfianugrah/composer:latest
    ports:
      - "8080:8080"
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock
      - /mnt/user/appdata/composer/stacks:/opt/stacks
      - /mnt/user/appdata/composer/ssh:/home/composer/.ssh
    environment:
      PUID: "99"
      PGID: "100"
      COMPOSER_DB_URL: "postgres://composer:password@postgres:5432/composer?sslmode=disable"
    depends_on:
      - postgres

  postgres:
    image: postgres:17-alpine
    volumes:
      - /mnt/user/appdata/composer/postgres:/var/lib/postgresql/data
    environment:
      POSTGRES_USER: composer
      POSTGRES_PASSWORD: password
      POSTGRES_DB: composer
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
   - `COMPOSER_DB_URL` = your Postgres connection string

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
- PostgreSQL accessible

### With Podman

```bash
# Podman socket must be active
systemctl --user enable --now podman.socket

# Composer auto-detects the Podman socket
COMPOSER_PORT=8080 \
COMPOSER_DB_URL="postgres://user:pass@localhost:5432/composer?sslmode=disable" \
composerd
```

Or run as a Podman container (same image, just use `podman` instead of `docker`):

```bash
podman run -d \
  --name composer \
  -p 8080:8080 \
  -v /run/podman/podman.sock:/var/run/docker.sock \
  -v composer-stacks:/opt/stacks \
  -e COMPOSER_DB_URL="postgres://user:pass@host:5432/composer?sslmode=disable" \
  -e PUID=$(id -u) \
  -e PGID=$(id -g) \
  ghcr.io/erfianugrah/composer:latest
```

## Networking Notes

Composer needs to reach:
1. **Docker socket** -- to manage containers and compose stacks
2. **PostgreSQL** -- for persistent state (users, sessions, stack metadata)
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
