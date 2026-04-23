# Self-Upgrade: Composer Managing Composer

> **Design doc — not implemented yet.** Revised after review identified four
> design gaps in the first draft (legacy-label assumption, non-compose
> deployment path, ephemeral job tracking, helper image selection). Target:
> v0.9.0, after bootstrap work.

## Status

- [ ] Design review (this doc, revised 2026-04-23)
- [ ] Self-identification via container inspect
- [ ] Compose-based upgrade path
- [ ] Non-compose upgrade path (`docker run` reconstruction) — covers Unraid template
- [ ] Singleton DB row for cross-restart job tracking
- [ ] Helper image decision (use composer's own image)
- [ ] Tests
- [ ] Docs (`configuration.md`, `security.md`, `api-reference.md`)

## Problem

Composer's normal flow is `git pull → docker compose pull → docker compose up -d`.
When the target stack is composer itself, step 3 stops and recreates the
composer container mid-request. The process doing the restart dies before
it can confirm success or return the response to the client. Operators today
work around this with an external `docker compose pull && up -d` command run
from SSH / watchtower / their host's cron — which breaks the "self-hosted all
the way down" story.

## Why simple fixes don't work

- **Async via `?async=true`**: the goroutine runs inside composer's process;
  dies when composer is SIGKILL'd by the daemon.
- **`compose up` in a trailing `&`**: the subprocess inherits composer's
  mount namespace; daemon reaps it when the composer container exits.
- **`syscall.Exec` in-place binary swap**: container image is read-only;
  `/usr/local/bin/composerd` can't be replaced without a new container.
- **Blue-green on an alternate port**: requires coordination composer doesn't
  have (proxy, shared volume contention, port exclusion).

## Solution: detached helper container, two deployment paths

Composer launches a **one-shot helper container** on the host Docker daemon
via the SDK (not via `docker compose` exec — that keeps the subprocess inside
composer's namespace). The helper is parented by the daemon, not by composer,
so it survives composer's death.

The upgrade command inside the helper branches on deployment type:

| Deployment | Helper command |
|---|---|
| `docker compose` (detected via `com.docker.compose.project` label) | `docker compose -p <project> pull && docker compose -p <project> up -d` |
| Plain `docker run` (no compose label — Unraid template case) | `docker pull <image> && docker stop <self> && docker rm <self> && docker run <reconstructed flags> <image>` |

## Design gaps in the first draft (corrected here)

### ❌ Gap 1: assumed legacy labels that aren't safe

First draft assumed `com.docker.compose.project.working_dir` and
`com.docker.compose.project.config_files` exist. Per
[docker/compose#10389](https://github.com/docker/compose/issues/10389):

> `com.docker.compose.project.working_dir` and `com.docker.compose.project.config_files`
> are **legacy labels used in earlier version of Docker Desktop**... AFAICT
> Desktop has been updated so it now relies on `--project-name` for
> equivalent commands and we could remove those labels. They're not a safe
> way to track the compose model used to create an application, as file
> might have been updated in between.

**Correction**: rely only on `com.docker.compose.project` (stable, maintained).
Use `docker compose -p <project> up -d` which looks up the project's compose
file via the daemon's project registry — no path on disk needed from our side.

### ❌ Gap 2: ignored non-compose deployments (Unraid template)

First draft said "return 422, use your platform's update flow" for
non-compose deployments. But **production Unraid uses the template which emits
a plain `docker run` command** — no compose labels. That's the primary
deployment target! Punting here defeats the purpose.

**Correction**: separate non-compose path that reconstructs the `docker run`
flags from `container inspect`:

```go
type reconstructedRunSpec struct {
    Name        string
    Image       string             // from args, not from inspect (we want NEW image)
    Env         []string           // .Config.Env
    Binds       []string           // .HostConfig.Binds
    PortBindings nat.PortMap        // .HostConfig.PortBindings
    NetworkMode string             // .HostConfig.NetworkMode
    RestartPolicy container.RestartPolicy // .HostConfig.RestartPolicy
    SecurityOpt []string           // .HostConfig.SecurityOpt
    Labels      map[string]string  // .Config.Labels (preserves Unraid template markers)
    Capabilities + CapDrops + etc.
}
```

The helper receives this spec as JSON env var, `docker stop` + `docker rm` the
old container, then `ContainerCreate + Start` with the new spec + new image.
This works for Unraid because Unraid's template-driven container just becomes
"the container being recreated" — the template metadata is in the labels, so
Unraid keeps track of it after the recreation.

**Caveat**: some host-level flags can't round-trip perfectly (e.g. Unraid's
`--ip` flag corresponds to `.NetworkSettings.Networks.<net>.IPAMConfig.IPv4Address`
which isn't always populated). Need to test against the actual Unraid template
output and add missing field plumbing iteratively. This is inherently fiddly;
maintain a test fixture from a real Unraid composer container.

### ❌ Gap 3: in-memory `JobManager` lost on restart

First draft: "New composer marks job completed based on container inspect."
But `app.JobManager` is in-memory (`map[string]*Job` in `app/jobs.go`). Old
composer creates job, dies, new composer boots with empty job map — the
client's job ID is invalid after restart.

**Correction**: persist the upgrade state as a **singleton DB row** keyed
by `"system.upgrade"`. Only one upgrade at a time, so a singleton row is
sufficient:

```sql
-- Migration in store/migrations:
CREATE TABLE IF NOT EXISTS system_upgrade (
    id           TEXT PRIMARY KEY DEFAULT 'singleton'
                 CHECK (id = 'singleton'),
    started_at   TIMESTAMP NOT NULL,
    started_by   TEXT NOT NULL,          -- user ID that initiated
    from_version TEXT NOT NULL,
    target_image TEXT NOT NULL,
    helper_id    TEXT NOT NULL,
    status       TEXT NOT NULL            -- pending, helper_running, completed, failed
                 CHECK (status IN ('pending','helper_running','completed','failed')),
    finished_at  TIMESTAMP,
    error        TEXT
);
```

Old composer writes row with `status='pending'`. Helper updates to
`'helper_running'` when it picks up the job (via docker events or file
sentinel — see Gap 5). New composer on boot reads the row, inspects the
helper container (still around, since `--rm` fires only after exit), marks
`'completed'` or `'failed'` based on helper exit code.

`GET /api/v1/system/upgrade/status` returns the row. No per-job-ID
endpoint — there's only ever one upgrade row.

### ❌ Gap 4: helper image may not have `docker compose` plugin

First draft specified `docker:28-cli` as the helper image. That's Alpine
with the Docker CLI, but the compose v2 plugin
(`/usr/libexec/docker/cli-plugins/docker-compose`) isn't guaranteed.

**Correction**: use **composer's own image** as the helper. Composer's
Dockerfile already bundles `docker` + `docker-compose` (verified in
`deploy/Dockerfile` stages). Override the entrypoint:

```go
helperCmd := []string{
    "/bin/sh", "-c",
    "sleep 5 && " + upgradeCommand,
}

cfg := container.Config{
    Image:      newImageRef,  // same as target — we're pulling it anyway
    Entrypoint: []string{},   // bypass composer's main
    Cmd:        helperCmd,
    // ...
}
```

The helper pulls `newImageRef` → runs the upgrade command (compose or
docker-run depending on path) → exits. Advantages:
- No separate image to pin / track
- Helper version matches composer version (the image we're upgrading TO)
- `docker compose` plugin definitely available — composer uses it itself

Disadvantage: the helper pulls the full composer image before it can do
anything. Acceptable — the pull would happen in step 3 of the upgrade
anyway. Just moves it earlier.

### ❌ Gap 5: hand-wavy sleep-5-seconds coordination

First draft's helper command starts with `sleep 5` to give composer time to
flush its HTTP response. Flaky if the response takes longer than 5s or the
client's TCP stack buffers the handshake differently.

**Correction**: **sentinel file coordination**. Composer writes
`$COMPOSER_DATA_DIR/upgrade-ack` after its response is flushed; helper
watches for the file before starting its work:

```go
// Old composer's handler:
func UpgradeHandler(...) (...) {
    ... create row, launch helper ...
    resp := &UpgradeResponse{...}

    // huma flushes resp when this function returns. Schedule the ack
    // file write AFTER the response flush by using a goroutine that
    // waits briefly then writes.
    go func() {
        time.Sleep(500 * time.Millisecond)  // small margin; response is local I/O
        os.WriteFile(filepath.Join(dataDir, "upgrade-ack"), []byte(row.ID), 0600)
    }()

    return resp, nil
}

// Helper's command:
// while [ ! -f /data/upgrade-ack ]; do sleep 1; done && rm /data/upgrade-ack && <upgrade commands>
```

Still has a small window (response not yet flushed when file is written)
but much smaller than 5s blind sleep, and correctness doesn't depend on
response being flushed — even if the client times out, the upgrade
proceeds. Client's job is to poll `/api/v1/system/upgrade/status` for
progress, not rely on the initial 202.

## API surface

```
POST /api/v1/system/upgrade
Authentication: admin
Request body: {} or {"pull_new_image": true}  (pull_new_image defaults true)
Response: 202 Accepted
{
  "helper_container_id": "c0ffee...",
  "from_version": "0.8.2",
  "target_image": "ghcr.io/erfianugrah/composer:latest-amd64",
  "deployment_type": "compose" | "docker_run",
  "status_url": "/api/v1/system/upgrade/status"
}

GET /api/v1/system/upgrade/status
Authentication: admin  (or public?  leaning public so the UI can show
"upgrade in progress" during the restart window)
Response:
{
  "status": "helper_running",
  "started_at": "...",
  "started_by": "user_abc",
  "from_version": "0.8.2",
  "target_image": "...",
  "helper_container_id": "...",
  "details": "Helper container pulling new image..."
}
```

## Security considerations

- **Admin-only for POST** — upgrade is "run helper container with Docker
  socket" + "recreate composer with new image". Catastrophic if abused.
- **Image source constraint**: target image MUST match the pattern
  `ghcr.io/erfianugrah/composer:<tag>` (configurable via
  `COMPOSER_UPGRADE_IMAGE_PREFIX` env var). No arbitrary-image execution
  via this endpoint.
- **Helper image IS target image**: the helper pulls the image composer
  will recreate with; if the image is malicious, composer was already
  going to execute it. No amplification.
- **Audit**: row in `system_upgrade` table includes `started_by`, accessible
  via the standard audit log query too.
- **One upgrade at a time**: the singleton row's PRIMARY KEY CHECK enforces
  this at the DB level. Second POST while `status='pending'` or
  `'helper_running'` returns 409.
- **Rollback not automatic**: if the new image fails to start, the row gets
  `status='failed'` but composer is stuck on an unhealthy container.
  Recovery: operator sets a known-good image tag in compose.yaml / Unraid
  template and retries, OR uses platform-native restart. Automatic rollback
  is v0.10+ work.

## Unraid path (integrated, not separate)

First draft treated Unraid as a fallback. In the revised design, Unraid IS
a first-class case because it's the primary non-compose deployment. The
docker-run reconstruction path handles it natively — once implemented,
Unraid users can use both:

- **Unraid Docker tab** → "Update Ready" → one click (works once the v0.8.2
  manifest fix propagates, doesn't touch composer's code)
- **`POST /api/v1/system/upgrade`** → same end result via composer's own
  plumbing (works identically on Unraid, bare Docker, and compose)

Both coexist; pick based on preference.

## Testing strategy

### Unit

- `SelfContainerID()`: parse various `/proc/self/cgroup` formats
  (cgroup v1, cgroup v2, hostname fallback, env var override)
- `DetectDeploymentType()`: reads compose project label; returns
  `compose` or `docker_run`
- `ReconstructRunSpec()`: round-trip test — inspect output → spec →
  equivalent docker run command. Fixture-based.
- Singleton-row ORM: CREATE/UPDATE idempotency; status transitions
  (pending → helper_running → completed | failed)

### Integration (testcontainers)

- Run composer in a container via testcontainers
- POST `/system/upgrade` with a mock target image (e.g. alpine:3 or
  a pinned-minor version of composer)
- Verify helper container is created with correct command
- Wait for helper to complete
- Verify new composer boots, reads the row, marks it completed
- Verify old composer is gone (`docker inspect` returns 404)

### Manual / E2E

- Compose deployment path: `docker compose up` on a laptop, POST
  upgrade, watch `docker ps` for helper + restart
- Unraid path: deploy composer via the Unraid template, POST
  upgrade from the web UI, verify template metadata (IP, network,
  labels) preserved after recreation

## Migration path

1. ✅ v0.8.2 (Unraid manifest fix — gives Unraid users a native update path
   today without any new code).
2. v0.9.0: implement the compose path first — lower risk, simpler flag
   mapping, covers users who deploy composer via `docker compose up`.
3. v0.9.1 or v0.9.2: implement the docker-run reconstruction path for
   Unraid + plain `docker run` deployments.
4. Document both paths in `docs/configuration.md`: platform-native
   (Unraid "Update Ready", watchtower for compose) remains the
   recommended default; `/system/upgrade` is the "self-hosted all the
   way down" path for operators who want no external orchestrators.

## Out of scope

- **Automatic rollback on upgrade failure**: requires keeping the old
  image ID and recreating with it on boot failure. Straightforward once
  basic upgrade works; v0.10+.
- **Version pinning from the UI**: the endpoint upgrades to whatever tag
  the current deployment references (compose.yaml image: line, or
  `.Config.Image` from container inspect for docker-run path). A UI
  control for "pick version from GHCR tag list" is nice-to-have.
- **Multi-instance coordination (HA)**: if composer ever runs as an HA
  pair, self-upgrade needs leader election + one-at-a-time rollout.
  Not urgent — composer is a self-hosting platform, not a SaaS.
- **Upgrading stacks composer manages**: the existing deploy endpoint
  already handles that. This doc is only about composer upgrading itself.
