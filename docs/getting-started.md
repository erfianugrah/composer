# Getting Started

## Prerequisites

- Docker (or Podman) with Docker Compose V2
- A machine with the Docker socket accessible

That's it. Composer bundles everything else (Go binary, docker CLI, git, compose plugin) inside its Docker image.

## Install with Docker Compose

```bash
# Download the compose file
mkdir -p /opt/composer && cd /opt/composer
curl -O https://raw.githubusercontent.com/erfianugrah/composer/main/deploy/compose.yaml

# Start
docker compose up -d
```

Composer is now running at `http://localhost:8080`.

## First Run: Bootstrap

On first launch with zero users, the bootstrap endpoint is available.

**Via the web UI:** Navigate to `http://localhost:8080/login` -- you'll be prompted to create the first admin account.

**Via the API:**

```bash
curl -X POST http://localhost:8080/api/v1/auth/bootstrap \
  -H "Content-Type: application/json" \
  -d '{"email":"admin@example.com","password":"your-strong-password"}'
```

This creates the first admin user. The bootstrap endpoint is automatically disabled once any user exists.

## Login

```bash
# Login (returns a session cookie)
curl -c cookies.txt -X POST http://localhost:8080/api/v1/auth/login \
  -H "Content-Type: application/json" \
  -d '{"email":"admin@example.com","password":"your-strong-password"}'

# Use the session cookie for subsequent requests
curl -b cookies.txt http://localhost:8080/api/v1/stacks
```

## Create Your First Stack

```bash
curl -b cookies.txt -X POST http://localhost:8080/api/v1/stacks \
  -H "Content-Type: application/json" \
  -d '{
    "name": "hello-world",
    "compose": "services:\n  web:\n    image: nginx:alpine\n    ports:\n      - \"8090:80\"\n"
  }'
```

## Deploy It

```bash
curl -b cookies.txt -X POST http://localhost:8080/api/v1/stacks/hello-world/up
```

Your nginx container is now running on port 8090.

## What's Next

- [Configuration](configuration.md) -- environment variables, PUID/PGID
- [API Reference](api-reference.md) -- full endpoint documentation
- [Deployment](deployment.md) -- Unraid, TrueNAS, bare metal, Podman
