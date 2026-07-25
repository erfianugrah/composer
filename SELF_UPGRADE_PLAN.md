# Self-Upgrade: Composer Managing Composer

> **Design doc - rev 3.** Rev 1 had four design gaps (legacy-label assumption,
> non-compose path, ephemeral job tracking, helper image). Rev 2 corrected them
> but introduced a new technical error in the Gap-1 fix (the "daemon project
> registry" does not exist) and left the trigger side unspecified. Rev 3 fixes
> the compose-path mechanics for real, makes the **existing webhook receiver
> the primary trigger**, and folds in operational lessons from watchtower's
> ephemeral-orchestrator self-update (nicholas-fedor/watchtower#1491), which
> independently converged on the same helper-container design.

## Status

- [x] Design review (rev 3, 2026-07-04)
- [x] Prerequisite: shutdown drain + stale-run reconciliation (see below)
- [x] Self-identification via container inspect
- [x] Compose-based upgrade path (label-derived compose file paths)
- [x] Non-compose upgrade path (`docker run` reconstruction) - covers Unraid
- [x] Singleton DB row for cross-restart job tracking
- [x] Webhook trigger routing (system-scoped webhook)
- [x] Manual trigger `POST /api/v1/system/upgrade` + status endpoint
- [x] Boot-time: orphan-helper sweep + upgrade-row reconciliation
- [x] `release.yml` webhook step (fires the trigger on image push; opt-in via repo var/secret)
- [x] Tests (unit + conformance suite in `internal/conformance/selfupgrade/`)
- [x] Docs (`configuration.md`, `security.md`, `api-reference.md`)
- [x] Live E2E (2026-07-25, Docker Desktop): compose path AND docker-run path,
      both `completed`; found+fixed 4 runtime bugs (host-path translation,
      volume-by-name mounts, COMPOSE_FILE IFS, lazy reconciliation)
- [ ] Live E2E on a native-Linux daemon (NixOS router) + Unraid via template

## Problem

Composer's normal flow is `git pull -> docker compose pull -> docker compose up -d`.
When the target stack is composer itself, step 3 stops and recreates the
composer container mid-request. Worse, the compose CLI runs as a **subprocess
of composerd** (`exec.CommandContext`): when the daemon SIGTERMs composerd,
`appCancel()` kills the compose child mid-`up`. If death lands between "old
container removed" and "new container created", composer is simply gone -
`restart: unless-stopped` cannot help because the container no longer exists.

Anything composerd parents dies with it; anything the **daemon** parents
survives. That asymmetry is the whole design constraint.

## Why simple fixes don't work

- **Point the existing webhook at composer's own stack**: the webhook ->
  `GitService.SyncAndRedeploy` path ends in `compose.Pull` + `compose.Up` as
  composerd subprocesses - exactly the suicidal pattern above.
- **Async via `?async=true`**: the goroutine runs inside composer's process;
  dies when composer is SIGKILL'd by the daemon.
- **`compose up` in a trailing `&`**: the subprocess inherits composer's
  process group; dies with it.
- **`syscall.Exec` in-place binary swap**: container image is read-only.
- **Blue-green on an alternate port**: requires coordination composer doesn't
  have (proxy, shared volume contention, port exclusion). Also: composer
  publishes `${COMPOSER_PORT:-8080}:8080`, and Docker holds published ports
  while the old container runs - any create-before-stop ordering fails with
  `port is already allocated` (watchtower's multi-year bug; their rename-based
  self-update skipped port-mapped containers entirely for this reason).

## Trigger: the existing webhook receiver (primary) + manual POST (fallback)

Detection is **push-based, not polled**. The pieces already exist:

- `POST /api/v1/hooks/{id}` - raw chi route, HMAC-validated per-webhook
  secret, CSRF-exempt, auth-exempt (signature instead of session), delivery
  history + audit trail. (`internal/api/handler/webhook*.go`, `server.go`)
- `GitService.SyncAndRedeploy` already treats the webhook as "a new image is
  available" - it always pulls and redeploys even when the compose file is
  unchanged (git_service.go).
- `release.yml` already builds + pushes the image on `v*` tags. Add a final
  step: POST to the composer's hook URL with the HMAC secret. GitHub can also
  deliver `release` events directly to the hook URL (provider `generic`).

What is missing is **routing**: webhooks today bind to a `stack_name` and
dispatch to `GitService.SyncAndRedeploy`. For self-upgrade we add a
system-scoped binding:

- Webhook CRUD gains a reserved scope: `stack_name = "_system"` (or a
  nullable `scope` column - pick at implementation time; `_system` is the
  zero-migration option if stack_name is free-form, otherwise add a small
  migration).
- The webhook handler (`handler.NewWebhookHandler`) dispatches `_system`
  hooks to `SelfUpgradeService.Request(ctx, triggeredBy:"webhook:"+id)`
  instead of `GitService`.
- Manual path preserved: `POST /api/v1/system/upgrade` (admin-only) calls the
  same `SelfUpgradeService.Request` - same code path, different auth.

Both entry points converge on one service method; there is exactly one
upgrade state machine regardless of trigger.

## Solution: detached helper container, two deployment paths

Composer launches a **one-shot helper container** on the host Docker daemon
via the SDK (not via `docker compose` exec - that keeps the subprocess inside
composer's process tree). The helper is parented by the daemon, not by
composer, so it survives composer's death.

The helper:

1. Pulls the target image.
2. Waits for the ack sentinel (see Coordination below).
3. Runs the deployment-path-specific upgrade command.
4. Polls the NEW composer container's health status (the image's own
   HEALTHCHECK hits the public `GET /api/v1/system/health` - a ready-made
   readiness probe) until `healthy` or timeout (~120s).
5. Writes its outcome where the new composer can read it (exit code +
   container logs; the helper stays around for inspection - `--rm` is NOT
   used; cleanup is the new composer's job).
6. Does NOT prune the old image. Rollback material is deleted only by the
   new composer after a healthy boot (deferred cleanup).

Helper container properties (from watchtower's postmortems):

- Created via SDK with `AutoRemove: false`, no port bindings, no compose
  project labels.
- Labeled `io.composer.upgrade-helper=true` (+ `io.composer.upgrade-id`).
  On boot, composer sweeps any containers with this label left over from a
  crashed attempt (inspect -> collect logs -> remove) BEFORE starting a new
  upgrade.
- Uses **composer's own (target) image** as the helper image (rev 2 Gap-4
  decision stands: the Dockerfile bundles docker CLI + compose plugin;
  override entrypoint with `Entrypoint: []string{}` and run
  `/bin/sh -c '<script>'`). Helper runs as root (entrypoint.sh bypassed) -
  fine, it only writes to the shared data volume's sentinel path.

### Compose path (REVISED - rev 3)

Rev 2 claimed `docker compose -p <project> up -d` works via a "daemon
project registry - no path on disk needed". **This is false.** Verified
against current compose source (`pkg/api/labels.go` in docker/compose v2):
there is no daemon-side registry of compose files, and `compose up` without
`-f` fails with "no configuration file provided". The labels rev 1 relied on
are NOT legacy either - compose still stamps every container with:

- `com.docker.compose.project`
- `com.docker.compose.project.working_dir` (absolute host path)
- `com.docker.compose.project.config_files` (absolute host paths, comma-joined)
- `com.docker.compose.project.environment_file` (when `--env-file` was used)

These are absolute **host** paths. The compose CLI resolves relative paths
(env_file, volume binds, build contexts) against the project directory, so
the helper must see the same paths. Composer therefore:

1. Reads the four labels off its OWN container (via `SelfContainerID()` +
   `ContainerInspect`).
2. Launches the helper with bind mounts mapping each host path to the SAME
   absolute path inside the helper (`<working_dir>:<working_dir>:ro`, plus
   each config file and env file's parent dir as needed - simplest correct
   approach: mount working_dir and each config file's dir, all ro).
3. Helper runs:
   `docker compose --project-directory <working_dir> -f <file1> [-f <file2>...]
    [--env-file <env_file>] -p <project> up -d --no-build --remove-orphans composer`

   Note the explicit **`composer` service argument**: composer's own
   deploy/compose.yaml also runs postgres + valkey; scoping the recreate to
   the composer service avoids needlessly bouncing stateful dependencies.

Caveat carried from docker/compose#10389: the file on disk may have changed
since the container was created. For an upgrade this is acceptable (arguably
desirable - you get the current file), but the helper should log a diff-worthy
warning if the config-hash label (`com.docker.compose.config-hash`) won't
match after recreate. Compose handles that reconciliation itself; we just
don't fight it.

### Non-compose path (`docker run` reconstruction - Unraid)

Unchanged from rev 2, and now the ONLY path for Unraid (template emits plain
`docker run`, no compose labels):

- Reconstruct the run spec from `container inspect` (env, binds, port
  bindings, network mode, restart policy, security opts, labels, caps).
- Pass the spec to the helper as a JSON env var.
- Helper: `docker stop <self> && docker rm <self> && docker run <spec> <new image>`.
  Stop+remove BEFORE create: the port-conflict lesson - create-before-stop
  can never work with published ports.
- Round-trip fidelity caveat stands (Unraid `--ip` -> IPAMConfig etc.);
  maintain a test fixture from a real Unraid composer container.

## State: singleton DB row

In-memory `JobManager` dies on restart, so upgrade state lives in the DB:

```sql
CREATE TABLE IF NOT EXISTS system_upgrade (
    id           TEXT PRIMARY KEY DEFAULT 'singleton'
                 CHECK (id = 'singleton'),
    started_at   TIMESTAMP NOT NULL,
    started_by   TEXT NOT NULL,          -- user ID, or "webhook:wh_xxx"
    from_version TEXT NOT NULL,
    target_image TEXT NOT NULL,
    helper_id    TEXT NOT NULL,
    status       TEXT NOT NULL
                 CHECK (status IN ('pending','helper_running','completed','failed')),
    finished_at  TIMESTAMP,
    error        TEXT
);
```

- Old composer writes `status='pending'` before launching the helper.
- New composer on boot reads the row, inspects the helper container (kept,
  not `--rm`'d), marks `completed` or `failed` from its exit code + logs,
  then removes the helper and - only after itself reporting healthy -
  prunes the old image.
- Second trigger while `pending`/`helper_running` -> 409.
- `GET /api/v1/system/upgrade/status` returns the row. Public read (like
  `/system/health`) so the UI can show "upgrade in progress" across the
  restart window; write stays admin/HMAC only.

## Coordination: sentinel file (not sleep)

Rev 2's sentinel design stands. Old composer schedules
`$COMPOSER_DATA_DIR/upgrade-ack` to be written ~500ms after the HTTP
response returns; the helper waits for the file, deletes it, then proceeds.
Correctness does not depend on the client having received the response.
The `composer_data` volume persists across the recreate, and entrypoint.sh's
`chown -R` repairs any root-owned helper artifacts on next boot.

## Prerequisite: shutdown drain + stale-run reconciliation

Independent of (but required for) self-upgrade:

- `PipelineService.Stop()` exists but is never called from `main.go` -
  wire it into the shutdown sequence before `httpSrv.Shutdown` returns.
- On boot, mark any `runs` rows stuck in `running` as
  `interrupted` ("composer restarted") - today every SIGTERM (upgrade or
  otherwise) strands them forever.
- Note Docker's default stop grace is 10s vs main.go's 30s HTTP drain: the
  daemon will SIGKILL at 10s unless `stop_grace_period` is raised in
  deploy/compose.yaml (recommend 35s) and plumbed into the reconstructed
  run spec for the docker-run path (`--stop-timeout`).

## API surface

```
POST /api/v1/hooks/{id}            (existing; _system-scoped webhook -> upgrade)
POST /api/v1/system/upgrade        admin -> 202 {helper_container_id, from_version,
                                     target_image, deployment_type, status_url}
GET  /api/v1/system/upgrade/status public -> {status, started_at, started_by,
                                     from_version, target_image,
                                     helper_container_id, error?}
```

## Security considerations

- **Admin-only POST**; webhook path is HMAC-validated with per-hook secrets.
- **Image source constraint**: target image MUST match
  `ghcr.io/erfianugrah/composer:<tag>` (configurable via
  `COMPOSER_UPGRADE_IMAGE_PREFIX`). No arbitrary-image execution.
- **Helper image IS target image**: if it's malicious, composer was already
  going to execute it. No amplification.
- **Audit**: `started_by` in the row + standard audit middleware entries for
  both trigger paths.
- **One at a time**: enforced by the singleton PRIMARY KEY CHECK + 409.
- **Rollback not automatic**: on failure the row goes `failed`; operator
  pins a known-good tag and retries. The old image is retained until the
  new composer self-confirms healthy, so manual rollback is
  `docker run <old image>` with the same spec. Automatic rollback is
  follow-up work.

## Testing strategy

### Unit

- `SelfContainerID()`: cgroup v1/v2 formats, hostname fallback, env override.
- `DetectDeploymentType()` + label extraction (config_files parsing:
  comma-joined, multiple files, env_file present/absent).
- `ReconstructRunSpec()`: inspect -> spec -> docker-run-equivalent round
  trip. Fixture-based (incl. an Unraid fixture).
- Singleton-row repo: idempotency, status transitions, 409 conflict logic.
- Webhook routing: `_system` scope dispatches to SelfUpgradeService, not
  GitService; stack-scoped hooks unaffected.
- Boot reconciliation: stale runs marked interrupted; orphan helpers swept;
  pending row -> completed/failed from helper exit code.

### Integration (testcontainers)

- Composer-in-container; POST upgrade against a mock target image; verify
  helper spec (mounts, labels, entrypoint override, no ports); helper
  completes; new composer reconciles the row; old container gone.

### Manual / E2E

- Compose path on a laptop deploy; Unraid path via the template (verify
  template metadata survives recreation).
- release.yml webhook step against a staging composer before tagging it on.

## Migration path

1. Prerequisite PR: drain + stale-run reconciliation + `stop_grace_period`.
2. v0.16.0: compose path + webhook routing + status endpoint (self-upgrade
   for compose deployments).
3. v0.16.x: docker-run reconstruction path (Unraid).
4. release.yml webhook step last, once the receiver side is proven.

## Out of scope

- **Automatic rollback on upgrade failure** (retained old image makes manual
  rollback easy; automation later).
- **Version pinning from the UI** (helper targets whatever ref the current
  deployment uses: compose.yaml `image:` line, or `.Config.Image` for
  docker-run).
- **Polling-based update detection / "update available" badge**: the webhook
  makes it unnecessary for the upgrade path. A nice-to-have UI hint later
  (registry digest compare), deliberately not in this design.
- **HA / multi-instance coordination.**
- **Upgrading stacks composer manages**: existing deploy endpoint. This doc
  is only composer-upgrading-composer.
