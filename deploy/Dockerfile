# ── Stage 1: Build frontend ──────────────────────────────────────
FROM oven/bun:1-alpine AS frontend
WORKDIR /app/web
COPY web/package.json web/bun.lock* ./
RUN bun install --frozen-lockfile
COPY web/ ./
RUN bun run build

# ── Stage 2: Build Go binary ────────────────────────────────────
FROM golang:1.26-alpine AS backend
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=frontend /app/web/dist ./web/dist
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /composerd ./cmd/composerd/

# ── Stage 3: Docker CLI + Compose plugin from official image ────
FROM docker:28-cli AS docker-bins

# ── Stage 4: Runtime (self-contained, security-hardened) ────────
FROM alpine:3.21

# git + SSH for git-backed stacks, certs for HTTPS, su-exec for privilege drop,
# shadow for usermod/groupmod (PUID/PGID support)
RUN apk add --no-cache git openssh-client ca-certificates tzdata su-exec shadow

# Bundle docker CLI + compose + buildx from official Docker image
COPY --from=docker-bins /usr/local/bin/docker /usr/local/bin/docker
COPY --from=docker-bins /usr/local/libexec/docker/cli-plugins/docker-compose \
     /usr/local/libexec/docker/cli-plugins/docker-compose
COPY --from=docker-bins /usr/local/libexec/docker/cli-plugins/docker-buildx \
     /usr/local/libexec/docker/cli-plugins/docker-buildx

# Composer binary
COPY --from=backend /composerd /usr/local/bin/composerd

# Create default non-root user (entrypoint adjusts UID/GID at runtime)
RUN addgroup -S composer && adduser -S composer -G composer \
    && mkdir -p /opt/stacks /opt/composer /home/composer/.ssh \
    && chown -R composer:composer /opt/stacks /opt/composer /home/composer/.ssh

# Entrypoint handles PUID/PGID + Docker socket GID + privilege drop
COPY deploy/entrypoint.sh /entrypoint.sh
RUN chmod +x /entrypoint.sh

WORKDIR /opt/composer
EXPOSE 8080

# Health check
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
  CMD wget -qO- http://localhost:8080/api/v1/system/health || exit 1

# Runs as root initially for PUID/PGID setup, then drops to composer user
ENTRYPOINT ["/entrypoint.sh"]
CMD ["composerd"]
