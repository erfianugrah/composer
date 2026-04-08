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

Unraid doesn't have Docker Compose by default, so you create each container
individually via the Unraid Docker UI. You need **two containers**: PostgreSQL
first, then Composer.

### Step 1: Create PostgreSQL Container

1. Go to **Docker > Add Container**
2. **Template:** Click "Add Container" (or use the XML template from `deploy/unraid/composer-postgres.xml`)
3. Set:
   - **Name:** `composer-postgres`
   - **Repository:** `postgres:17-alpine`
   - **Network Type:** Bridge
4. Add port mapping:
   - Container Port: `5432`, Host Port: `5432`, Connection Type: TCP
5. Add path:
   - **Container Path:** `/var/lib/postgresql/data`
   - **Host Path:** `/mnt/user/appdata/composer/postgres`
6. Add variables:
   - `POSTGRES_USER` = `composer`
   - `POSTGRES_PASSWORD` = `changeme` (use a strong password!)
   - `POSTGRES_DB` = `composer`
7. Click **Apply**
8. Wait for the container to start, then **note its IP address** (visible in the Docker tab)

### Step 2: Create Composer Container

1. Go to **Docker > Add Container**
2. **Template:** Use the XML template from `deploy/unraid/composer.xml` if available
3. Set:
   - **Name:** `composer`
   - **Repository:** `ghcr.io/erfianugrah/composer:latest`
   - **Network Type:** Bridge
   - **Extra Parameters:** `--security-opt no-new-privileges:true`
4. Add port mapping:
   - Container Port: `8080`, Host Port: `8080`, Connection Type: TCP
5. Add paths:
   - `/var/run/docker.sock` -> `/var/run/docker.sock` (Mode: `rw`) -- Docker socket
   - `/mnt/user/appdata/composer/stacks` -> `/opt/stacks` (Mode: `rw`) -- Stack files
   - `/mnt/user/appdata/composer/ssh` -> `/home/composer/.ssh` (Mode: `rw`) -- SSH keys (optional)
6. Add variables:
   - `PUID` = `99`
   - `PGID` = `100`
   - `COMPOSER_DB_URL` = `postgres://composer:changeme@172.17.0.X:5432/composer?sslmode=disable`
     (replace `172.17.0.X` with the Postgres container's IP from Step 1,
     and `changeme` with the password you set)
   - `COMPOSER_LOG_LEVEL` = `info`
   - `COMPOSER_COOKIE_SECURE` = `false` (set to `true` if behind Caddy/nginx with HTTPS)
7. Click **Apply**
8. Open `http://[UNRAID-IP]:8080` and create your admin account

### Step 3: Create Valkey Container (cache)

1. Go to **Docker > Add Container**
2. Set:
   - **Name:** `composer-valkey`
   - **Repository:** `valkey/valkey:8-alpine`
   - **Network Type:** Bridge
3. Add port mapping:
   - Container Port: `6379`, Host Port: `6379`, Connection Type: TCP
4. Add path:
   - `/mnt/user/appdata/composer/valkey` -> `/data`
5. Click **Apply**
6. Note the container's IP address, then **edit the Composer container** and add:
   - `COMPOSER_VALKEY_URL` = `valkey://[VALKEY-IP]:6379`
7. Restart the Composer container

### XML Templates

Pre-built Unraid XML templates are in `deploy/unraid/`:
- `composer.xml` -- Composer container
- `composer-postgres.xml` -- PostgreSQL for Composer

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
      - /mnt/user/appdata/composer/stacks:/opt/stacks
      - /mnt/user/appdata/composer/ssh:/home/composer/.ssh
    environment:
      PUID: "99"
      PGID: "100"
      COMPOSER_DB_URL: "postgres://composer:changeme@composer-postgres:5432/composer?sslmode=disable"
    depends_on:
      - composer-postgres

  composer-postgres:
    image: postgres:17-alpine
    volumes:
      - /mnt/user/appdata/composer/postgres:/var/lib/postgresql/data
    environment:
      POSTGRES_USER: composer
      POSTGRES_PASSWORD: changeme
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
