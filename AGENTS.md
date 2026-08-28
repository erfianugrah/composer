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
2. Run `make generate` — regenerates `web/src/lib/api/{openapi.json,openapi.yaml,types.ts}`
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

## OpenAPI spec

One source of truth: `internal/api/openapi.go`.
- `HumaConfig(version)` — info, servers, security schemes, tag descriptions.
- `RegisterHumaHandlers(api, deps, registerAll)` — every Huma handler. `registerAll=true` for the dumper, `false` at runtime so degraded-mode boots register only what their deps support.
- `DocumentRawRoutes(api)` — OpenAPI stubs for routes served by raw chi handlers (OAuth begin/callback, `/api/v1/hooks/{id}` webhook receiver).

Both `internal/api/server.go` (runtime) and `cmd/dumpopenapi/main.go` (build-time) call these three. Do NOT duplicate config or handler lists between them. Tests in `internal/api/openapi_test.go` enforce that the runtime-generated spec matches the committed `web/src/lib/api/openapi.json` and that every declared tag is used.

Lint the spec with `make generate-lint` (uses `web/redocly.yaml`).

## Architecture

DDD with bounded contexts: `cmd/composerd` (entrypoint), `internal/{domain,app,api,infra}` layers.
SQLite primary (modernc.org/sqlite), Postgres optional. Valkey optional. SOPS/age encryption.

## Live status vs request path

Docker daemons are never contacted synchronously on a read endpoint. A
background `StatusRefresher` (internal/app/status_refresher.go, 15s tick,
`COMPOSER_STATUS_REFRESH_MS` override) fans out to every configured host
concurrently under a 3s per-host timeout and snapshots per-stack container
counts + derived lifecycle status + per-host reachability in memory. Read
handlers (`stacks.List`, `containers.List`) serve DB rows + that snapshot
only; a host that stops answering shows its stacks as `unknown`/stale instead
of stalling the response. Rules that follow from this:

- Never add a docker call to a GET handler's request path - put the data in
  the refresher snapshot instead.
- The stored `stacks.status` column is written by compose actions only; for
  display always prefer the refresher's derived status when the host is
  reachable.
- `StackHandler.Get` may call docker (single stack, bounded) but wraps the
  call in `containerListTimeout` (3s).

## Docker host mTLS

Remote docker endpoints register under Settings -> Docker Hosts. Client
certificates are uploadable there (or via `PUT /api/v1/hosts/{id}/certs`):
PEMs are AES-256-GCM encrypted in `docker_host_certs` (migration 009) and
materialized to `<dataDir>/certs/<host_id>/` when a client is built. DB certs
win over the legacy `cert_dir`. `POST /api/v1/hosts/{id}/test` builds a
throwaway client and pings with a 3s timeout; rotation order is in
docs/runbooks/docker-host-mtls-rotation.md. The docker client/compose cache
(`internal/infra/docker.Factory`) is invalidated by `HostService.Update` /
`Delete` via `SetCacheInvalidator` - touching the factory for a new reason
must keep that wiring.

## Encryption key rotation

The at-rest key (`COMPOSER_ENCRYPTION_KEY` env / `$COMPOSER_DATA_DIR/encryption.key`)
encrypts every stored secret with AES-256-GCM (`enc:` prefix). Precedence since
v0.26.0: key file > env > auto-generated (the key file is the UI-settable key,
so it wins - mirrors the SOPS age-key rule). Rotation is safe and UI/API-driven:
`POST /api/v1/system/config/encryption-key/rotate` (admin) re-encrypts every
`enc:` DB row across the four encrypted stores (registry creds,
`stack_git_configs.credentials`, webhook secrets, `docker_host_certs`) in ONE
transaction with rollback, then writes the new encryption.key file and swaps
the crypto singleton only after commit. Rotation is DB-only: on-disk key files
(SSH deploy keys, git token) are plaintext materializations read at runtime
and are never re-encrypted (v0.26.0 shipped a file-rotation path that
ciphertexted them in place and broke git sync; removed). The new key is
returned once in the response and never logged. A half-rotated DB is the brick
scenario this design exists to prevent - never rotate by hand-editing the key
file without the re-encrypt.
