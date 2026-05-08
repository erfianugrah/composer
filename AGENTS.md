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

## Release workflow

Version bump + tag + push must follow this sequence:

1. Bump `const Version` in `version.go`
2. Run `make generate` — regenerates `web/src/lib/api/openapi.json` and `types.ts`
   (the OpenAPI spec embeds the version string; CI checks it matches)
3. Run `make build-frontend` — rebuild so `go vet` passes (static.go embed)
4. Run `make lint` and `make test-unit` — verify locally
5. Commit all changed files (version.go + openapi.json + types.ts + any code)
6. Tag: `git tag v<new-version>`
7. Push commit and tag: `git push && git push --tags`

CI (`ci.yml`) runs on push to main and tags — lint step runs `make generate` and
checks `git diff --exit-code` on the generated files. If the spec is stale, lint fails.

Release (`release.yml`) triggers on `v*` tags — builds multi-arch Docker image and
pushes to `ghcr.io/erfianugrah/composer`.

## Architecture

DDD with bounded contexts: `cmd/composerd` (entrypoint), `internal/{domain,app,api,infra}` layers.
SQLite primary (modernc.org/sqlite), Postgres optional. Valkey optional. SOPS/age encryption.
