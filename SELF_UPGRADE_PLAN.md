# Self-Upgrade: Composer Managing Composer

## Status

- [ ] Design doc (this file)
- [ ] Section 1: container self-discovery
- [ ] Section 2: helper-container upgrade endpoint
- [ ] Section 3: fallback for non-compose deployments
- [ ] Tests
- [ ] Docs (`configuration.md`, `security.md`)

## Problem

Composer's normal flow is `git pull → docker compose pull → docker compose up -d`.
When the stack being acted on is composer itself, step 3 stops and recreates
the composer container mid-request. The process doing the restart dies before
it can confirm success or return the response to the client. Operators today
work around this with an external `docker compose pull && up -d` command run
from SSH / watchtower / their host's cron — which breaks the "self-hosted
all the way down" story.

## Why simple fixes don't work

- **Async via `?async=true`**: the goroutine runs inside composer's process;
  dies when composer is SIGKILL'd by the daemon.
- **`compose up` in a trailing `&`**: the subprocess inherits composer's mount
  namespace; daemon reaps it when the composer container exits.
- **`syscall.Exec` in-place binary swap**: container image is read-only;
  `/usr/local/bin/composerd` can't be replaced without a new container.
- **Blue-green on an alternate port**: requires coordination composer doesn't
  have (proxy, shared volume contention, port exclusion).

## Solution: detached helper container

Composer launches a **one-shot helper container** on the host Docker daemon
via the SDK (not via `docker compose` exec — that keeps the subprocess inside
composer's namespace). The helper is parented by the daemon, not by composer,
so it survives composer's death.

```sh
docker run --rm -d --name composer-upgrader \
  -v /var/run/docker.sock:/var/run/docker.sock \
  -v /path/to/compose:/stack:ro \
  docker:28-cli \
  sh -c "sleep 5 && docker compose -f /stack/compose.yaml pull && docker compose -f /stack/compose.yaml up -d"
```

### Flow

1. `POST /api/v1/system/upgrade` (admin only) arrives
2. Composer introspects its own container via the Docker SDK to discover:
   - Its image ref (for logging)
   - Its compose project name (`com.docker.compose.project` label)
   - Its compose working dir (`com.docker.compose.project.working_dir` label)
   - Its compose file path (`com.docker.compose.project.config_files` label)
3. Composer creates + starts the helper container with:
   - Image: `docker:<version>-cli` (pinned to avoid `latest` drift)
   - Command: `sleep N && docker compose -f <file> pull && docker compose -f <file> up -d`
   - Bind mounts: Docker socket + the compose working dir (read-only)
   - `AutoRemove: true`
   - A unique name so a second upgrade call can detect an in-flight one
4. Composer returns HTTP 202 with the helper container ID and a job row in
   the DB. The sleep gives composer time to flush the response to the client.
5. Helper pulls the new image, runs `compose up -d`, daemon stops old composer
   and starts a new one with the pulled image.
6. New composer reads the same DB on boot, sees the job row, marks it
   completed based on `container inspect` of the helper (exit 0 = success).
7. Client polls `GET /api/v1/system/version` until the new version reports.

### Self-discovery

Composer needs to identify its own container. Options:

- **`/proc/self/cgroup` parse**: cgroup v1 exposes the container ID. Works
  in most setups; fails in cgroup v2 / userns-remapped deployments.
- **`/etc/hostname`**: by default Docker sets the hostname to the short
  container ID. Brittle (users can override).
- **Env var `HOSTNAME`**: same as above, same caveats.
- **`docker inspect` by hostname**: combine with the above — use hostname
  to look up the container and read its labels.

Best: try `/proc/self/cgroup`, fall back to `HOSTNAME`, fall back to env var
`COMPOSER_SELF_CONTAINER_ID` for operators who set it explicitly.

### Compose file path resolution

Compose labels on composer's own container:

```
com.docker.compose.project = "composer"
com.docker.compose.project.working_dir = "/opt/stacks/composer"
com.docker.compose.project.config_files = "/opt/stacks/composer/compose.yaml"
```

Composer reads these from `container inspect` of itself. The helper mounts
the working dir and references the compose file by its host path.

Fallback for non-compose deployments (plain `docker run`, Unraid template
pre-`docker compose` era): env var `COMPOSER_SELF_COMPOSE_PATH`. If the
label is missing AND the env var is unset, `/system/upgrade` returns 422
with "not deployed via docker compose — use your platform's update flow
(Unraid 'Update Ready', watchtower, etc.)".

## API surface

```
POST /api/v1/system/upgrade
Authentication: admin
Request body: (empty, no config needed — everything self-discovered)
Response: 202 Accepted
{
  "helper_container_id": "c0ffee...",
  "job_id": "job_deadbeef",
  "expected_new_version": "0.9.0",
  "details": "A helper container will pull the new image and restart composer in ~5s. Poll GET /api/v1/system/version to detect completion."
}
```

Job row semantics:
- `type`: `"system.upgrade"`
- `target`: composer's own image ref
- `status`: `pending` initially. New composer boots, marks it `completed` or
  `failed` based on helper container exit status.

## Security considerations

- **Admin-only**. Upgrade is functionally "run arbitrary container image
  with docker socket" — treat accordingly.
- **Image validation**: by default the helper pulls whatever tag the current
  stack's compose.yaml references. No user-supplied image refs — the stack's
  existing config is authoritative. Prevents arbitrary-image-execution via
  this endpoint.
- **Audit log**: every upgrade attempt is recorded with caller ID + timestamp.
- **Rate limit**: one concurrent upgrade at a time (detect in-flight helper
  container by its fixed name).
- **Helper image pinning**: use `docker:28-cli` (pinned to the Docker CLI
  version composer was built against) rather than `docker:cli` or `:latest`.
  Pin version via const in composer's source. Updated per release.

## Unraid path (parallel, not alternative)

On Unraid, the platform's native "Update Ready" flow already works — once
the `:latest` manifest resolves correctly (fix in v0.8.2) Unraid will display
"update ready" and a single click recreates the container using the template's
config. `/system/upgrade` is a non-Unraid alternative; Unraid operators can
keep using the Docker tab UI.

## Testing strategy

### Unit

- `SelfContainerID()`: parse various `/proc/self/cgroup` formats (v1, v2,
  hostname fallback, env var override)
- `DiscoverComposeFile()`: parse compose labels from an `inspect` fixture;
  fallback to env var; 422 on no signal
- Job row is created before helper launch, updated by the new composer on
  boot

### Integration (testcontainers)

- Run composer in a container; POST `/system/upgrade`; verify helper
  container is created; verify helper's command references the right
  compose file; kill the helper before it finishes to avoid actually
  upgrading the test composer (we just test the dispatch)

### Manual / E2E

- Deploy composer via `docker compose up`
- Push a patch version bump to the repo
- Wait for CI to publish `:latest`
- POST `/system/upgrade`
- Watch `docker ps` — helper container appears, composer dies, composer
  restarts with new image
- Verify `GET /system/version` reports new version

## Migration path

1. Ship v0.8.2 (Unraid manifest fix — gives Unraid users the "update ready"
   path today).
2. Ship self-upgrade endpoint in v0.9.0 — adds composer-managed upgrades
   for non-Unraid deployments without breaking existing flows.
3. Document both paths in `docs/configuration.md`: platform-native (Unraid,
   watchtower) remains the recommended default; `/system/upgrade` is the
   "self-hosted all the way down" path for operators who want no external
   orchestrators.

## Out of scope

- **Rollback on upgrade failure**: if the new image fails to start, the
  helper has already exited. Recovery is "set the image tag back and
  upgrade again". Could be extended to keep the old image ID and recreate
  with it on failure, but that's v0.10+.
- **Version pinning from the UI**: the endpoint upgrades to whatever the
  compose.yaml references. If operators want to pin a specific version,
  they edit compose.yaml first, then POST `/upgrade`. A UI control for
  "pick version from GHCR tag list" is a nice future addition.
- **Multi-instance coordination**: for the hypothetical future where
  composer runs as an HA pair, self-upgrade needs to be one-instance-at-
  a-time with leader election. Not urgent — this is a self-hosting
  platform, not a SaaS.
