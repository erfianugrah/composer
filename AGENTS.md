## Safety

**NEVER run `./composerd` or `go run ./cmd/composerd/` directly on the dev machine.**

The startup hook at `cmd/composerd/main.go` auto-encrypts ALL SSH private keys
in `$HOME/.ssh` using an AES-256-GCM key stored in `COMPOSER_DATA_DIR`. If that
dir is `/tmp`, the encryption key is lost on reboot and the SSH keys become
unrecoverable.

Safe alternatives:
- `go test ./...` — runs all tests without the startup hook
- `make test-unit` — unit tests only (domain, eventbus)
- `make test-integration` — needs Docker/testcontainers
- `docker compose up` from `deploy/` — runs in container with isolated `/home/composer/.ssh`

## Build

Frontend must be built before Go compilation (`static.go` embeds `web/dist`):

```bash
make build              # full build (frontend + backend)
make build-frontend     # bun only
make build-backend      # go only
```

`CGO_ENABLED=0` — pure Go, no CGO needed.

## Testing

```bash
make test-unit          # fast, no Docker
make test-integration   # needs Docker, -p 1 (sequential)
make test-frontend      # Playwright + Chromium
make lint               # go vet
```

## Architecture

DDD with bounded contexts: `cmd/composerd` (entrypoint), `internal/{domain,app,api,infra}` layers.
SQLite primary (modernc.org/sqlite), Postgres optional. Valkey optional. SOPS/age encryption.
