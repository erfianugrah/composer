# Composer Code Review

**Date**: 2026-07-09
**Scope**: Full codebase audit (`cmd/composerd`, `internal/{domain,app,api,infra}`, `web/src`)
**Baseline**: `review/full-audit` branch, HEAD `93bfce4`

---

## Severity Summary

| Severity | Count |
|----------|-------|
| Critical | 1 |
| High     | 1 |
| Medium   | 11 |
| Low      | 11 |

| Domain             | C | H | M | L |
|--------------------|---|---|---|---|
| Correctness        | 1 | 1 | 2 | 1 |
| Concurrency        | 0 | 0 | 0 | 1 |
| Security           | 0 | 0 | 3 | 2 |
| Error Handling     | 0 | 0 | 2 | 1 |
| Architecture       | 0 | 0 | 2 | 0 |
| Database           | 0 | 0 | 1 | 2 |
| API / OpenAPI      | 0 | 0 | 0 | 1 |
| Frontend           | 0 | 0 | 2 | 1 |
| Testing            | 0 | 0 | 0 | 1 |
| Dependencies       | 0 | 0 | 1 | 0 |

---

## 1. Correctness Bugs and Logic Errors

### C1 - StackLocks: race between Unlock and Delete

**Severity**: Critical
**File**: `internal/app/stack_locks.go:30-37` (Unlock), `stack_locks.go:40-42` (Delete)

The `Unlock` method reads the per-stack mutex from the map, releases the outer lock, then calls `m.Unlock()` outside the critical section:

```go
func (l *StackLocks) Unlock(name string) {
    l.mu.Lock()
    m := l.locks[name]  // read entry
    l.mu.Unlock()
    if m != nil {
        m.Unlock()  // unlocks OUTSIDE outer lock
    }
}
```

`Delete` removes the entry from the map under the outer lock but does NOT interact with the per-stack mutex:

```go
func (l *StackLocks) Delete(name string) {
    l.mu.Lock()
    delete(l.locks, name)
    l.mu.Unlock()
}
```

**Race scenario**: Goroutine A calls `Unlock("stack1")` -- it reads `m` (the per-stack mutex) and releases the outer lock. Before A reaches `m.Unlock()`, goroutine B calls `Delete("stack1")` under the outer lock and removes the entry. Goroutine C then calls `Lock("stack1")`, creates a *new* per-stack mutex, locks it, and starts a compose operation. Goroutine A finally calls `m.Unlock()` on the *old* mutex -- but goroutine C holds the *new* mutex, so the compose operation under C is not truly locked. Separately, a `nil` dereference is possible if `locks[name]` is concurrently deleted between the read in `Unlock` and the nil-check -- the map entry is gone but `m` still holds a pointer to the now-orphaned mutex.

**Why it matters**: Concurrent stack operations (e.g., a webhook-triggered `SyncAndRedeploy` and an API-triggered `Deploy` on the same stack) could run `docker compose` simultaneously, causing port conflicts, corrupted state, or parallel decrypt/re-encrypt cycles on SOPS secrets.

**Fix**: `Delete` must lock or drain the per-stack mutex before removing the entry. Simplest approach: `Delete` acquires the per-stack lock (creating it via `Lock` semantics if absent, then acquiring), removes the entry, then unlocks. Any concurrent `Unlock` that already read the mutex under the outer lock will unlock the same mutex, which then becomes unreachable beneath the now-deleted map key. Better still: replace the two-phase lock/unlock with a single-phase pattern where the per-stack lock is held across the entire `Lock`/`Unlock` cycle, removing the window between reading and unlocking.

---

### C2 - SOPS secrets left decrypted on deploy failure in Create + CreateGitStack

**Severity**: High
**File**: `internal/app/git_service.go:128-140` (CreateGitStack), `internal/app/stack_service.go:126-135` (Create)

Both `StackService.Create` and `GitService.CreateGitStack` decrypt SOPS-encrypted secrets into place before deploying, but only re-encrypt them inside an `else` block -- i.e., only when the deploy *succeeds*:

```go
// git_service.go CreateGitStack (lines 128-140)
sops.DecryptEnvFile(gitCfg.ResolveEnvPath(stackPath), ageKey)
sops.DecryptComposeSecrets(filepath.Join(stackPath, gitCfg.ComposePath), ageKey)
// ...
if _, err := s.compose.Up(deployCtx, stackPath, gitCfg.ComposePath); err != nil {
    s.log.Warn("auto-deploy failed ...")  // secrets remain decrypted on disk!
} else {
    sops.ReEncryptEnvFile(...)   // only called on success
    sops.ReEncryptComposeSecrets(...)
}
```

`stack_service.go Create` has the same pattern.

**Contrast** with `SyncAndRedeploy` (`git_service.go:225-236`), which correctly uses `defer`:

```go
sops.DecryptEnvFile(envFile, ageKey)
sops.DecryptComposeSecrets(composePath, ageKey)
defer func() {
    sops.ReEncryptEnvFile(envFile)
    sops.ReEncryptComposeSecrets(composePath)
}()
```

**Why it matters**: If `docker compose up` fails (bad image ref, port conflict, resource exhaustion), the stack's secrets remain as decrypted plaintext on disk indefinitely. For git-backed stacks, the next `Sync` may not detect the drift and won't re-encrypt them, leaving plaintext `.env` and `compose.yaml` exposed on the filesystem.

**Fix**: Mirror the `SyncAndRedeploy` pattern -- use `defer` for re-encryption in both `Create` and `CreateGitStack`. This guarantees re-encryption regardless of whether the compose operation succeeds or fails, and also ensures the `.sops` backup file is cleaned up.

---

### C3 - O(m*n) diff DP table with O(k^2) backtrack prepend

**Severity**: Medium
**File**: `internal/app/diff.go:71-85` (lcsLines), `diff.go:89-100` (backtrack prepend)

The LCS implementation uses a classic DP table of dimensions `(m+1) x (n+1)` -- quadratic memory. For 2500-line compose files (plausible for large stacks), this is 6.25M `int` entries consuming ~50 MB. Additionally, the backtrack loop uses `append([]string{a[i-1]}, result...)` which allocates a new slice and copies all previously accumulated lines on every step -- O(k^2) where k is the LCS length:

```go
result = append([]string{a[i-1]}, result...)
```

For a diff where most lines match (k ~= len(oldLines)), this is thousands of allocations and quadratic copies.

**Why it matters**: The diff endpoint is viewer-role and called by the UI whenever a user inspects a stack's compose changes. A large compose file could cause an OOM on the server or a slow response that ties up the API handler goroutine.

**Fix**: Two simple changes: (1) build the result in reverse and reverse-once at the end (O(k) instead of O(k^2)); (2) for files larger than ~1000 lines, switch to a line-by-line heuristic (e.g., Myers' diff with a band-limit or a windowed approach) to keep memory bounded.

---

### C4 - StackList repository always sets StatusUnknown; status is not persisted

**Severity**: Low
**File**: `internal/infra/store/stack_repo.go:52-53` (List), `stack_repo.go:65-66` (GetByName)

The repository hardcodes `s.Status = stack.StatusUnknown` on every read. The status is enriched later in `StackService.List` by cross-referencing container state, but for callers that hit the repo directly (tests, internal helpers), the status is always `unknown`. This is a data-inconsistency trap -- if any code path reads a stack from the repo without going through the service-layer enrichment, it gets stale data.

**Fix**: Remove the `Status` field from the domain entity (it's derived, not stored) or compute it inside the repository from joined container data. The former is simpler: add a `Status() Status` method to `Stack` that accepts container counts as input, similar to how `Container.IsRunning()` works.

---

## 2. Concurrency and Data Races

### CO1 - LogViewer: stale closure on `maxLines` prop

**Severity**: Low
**File**: `web/src/components/container/LogViewer.tsx:36-56` (useEffect)

The SSE `useEffect` dependency array is `[containerId, stackName, tail]` but the effect closure captures `maxLines` from its initial render. If a parent component changes `maxLines` after the effect is running, the stale value continues to be used in the `setLines` updater.

**Why it matters**: In practice, `maxLines` defaults to 1000 and is rarely changed dynamically. However, the stale closure means the prop is effectively frozen at mount time -- a parent that adjusts `maxLines` based on available memory would have no effect until the component remounts.

**Fix**: Add `maxLines` to the dependency array (preferred -- simple, correct), or lift the `maxLines`-capped slice logic into a `useRef` that stays current without triggering SSE reconnections (avoids reconnection cost from the dep-array change).

---

## 3. Security

### S1 - SOPS_AGE_KEY exposed in process environment

**Severity**: Medium
**File**: `internal/infra/sops/sops.go:92`

```go
cmd.Env = append(os.Environ(), "SOPS_AGE_KEY="+ageKey)
```

The age private key is placed into the child process environment, which is readable via `/proc/<pid>/environ` for the lifetime of the `sops` subprocess. On a shared host, any process running as the same UID can read it.

**Why it matters**: The `sops` binary supports `SOPS_AGE_KEY` in the environment -- it's the documented mechanism. The exposure window is bounded by the sops subprocess lifetime (typically < 1 second). On a single-tenant Docker container (the primary deployment target), this is low-risk. On a multi-tenant or shared-host deployment, it's a credential-leak vector.

**Fix**: Acceptable as-is for single-tenant deployments. For multi-tenant hardening, write the key to a temp file with mode 0600, pass `SOPS_AGE_KEY_FILE` instead, and remove the file immediately after `cmd.Run()` returns. This confines the secret to a file with a shorter lifetime and finer access control than the process environment.

---

### S2 - CSP allows `'unsafe-inline'` scripts

**Severity**: Medium
**File**: `internal/api/middleware/security.go:23-26`

```go
Content-Security-Policy: default-src 'self'; script-src 'self' 'unsafe-inline'; ...
```

The `'unsafe-inline'` directive on `script-src` disables CSP's primary XSS defense: only scripts from the declared source (`'self'`) or with a matching nonce/hash are allowed to execute. With `'unsafe-inline'`, any `<script>` tag injected into the DOM (via XSS in compose-file display, stack name, container log, pipeline output, etc.) executes with full origin authority.

**Why it matters**: The Astro frontend is server-rendered with React hydration; it does not require `'unsafe-inline'` for framework functionality. The inline allowance appears to be a legacy accommodation. An XSS in any part of the UI (e.g., a container log line containing `</span><script>alert(1)</script><span>`) would execute because the CSP whitelists inline scripts.

**Fix**: Remove `'unsafe-inline'` and test the full UI to identify any genuine inline-script dependencies. Astro/React should not need it. If inline is truly needed for a specific component or build artifact, use a nonce or hash instead -- both are supported by the CSP spec and by Astro's SSR output.

---

### S3 - CSRF middleware logic gates on session cookie presence, not auth method

**Severity**: Medium
**File**: `internal/api/middleware/csrf.go:51-62`

```go
if _, hasCookie := r.Cookie("composer_session"); hasCookie == nil {
    if r.Header.Get("X-Requested-With") == "" {
        http.Error(w, ...)
        return
    }
}
```

The CSRF check fires only when the `composer_session` cookie is present. If the cookie is absent, CSRF enforcement is completely skipped -- even if the request is authenticated by some other mechanism.

**Why it matters**: Currently, the auth middleware rejects cookie-less requests that lack an API key header, so there is no exploitable gap today. However, the CSRF middleware's logic is the inverse of what a reader expects: "if session cookie present -> enforce CSRF" rather than "if authenticated via cookie -> enforce CSRF." If a future change adds a new authentication mechanism that sets a user context *without* a `composer_session` cookie, CSRF would be silently bypassed for those requests.

**Fix**: Move CSRF enforcement into the auth middleware so it fires based on the *actual* authentication method (cookie-based -> require CSRF header; API-key-based -> skip CSRF). This keeps the CSRF decision co-located with the auth decision, where the middleware already knows which credential type was used.

---

### S4 - `extractAPIKey` accepts malformed Bearer tokens without early rejection

**Severity**: Low
**File**: `internal/api/middleware/auth.go:192-206`

The `extractAPIKey` function strips a `Bearer ` prefix but does not validate the remaining token's format (length, character set) before hashing and querying the database. An attacker sending `Authorization: Bearer x` triggers a full `SHA-256 -> DB lookup` cycle for a single-character key.

**Why it matters**: Minimal -- `auth.ValidateAPIKey` hashes the value and does a DB lookup by hash, so the worst case is wasted DB queries from malformed tokens. No cryptographic bypass occurs.

**Fix**: Add a quick-return for tokens shorter than a minimum length (e.g., 16 chars) or containing characters outside the expected key alphabet. This drops clearly-invalid tokens before the hash and DB lookup.

---

### S5 - `DecryptData` writes SOPS-encrypted data to default temp directory

**Severity**: Low
**File**: `internal/infra/sops/sops.go:120`

```go
tmp, err := os.CreateTemp("", "sops-*"+ext)
```

The temp file is created in the default temp directory (usually `/tmp`), which on some systems is world-readable. The file contains SOPS-encrypted ciphertext (not plaintext), so this is not a secret leak, but it does expose the fact that encrypted data was processed, and the ciphertext blob is written to a world-readable location briefly.

**Why it matters**: Low -- the file is deleted via `defer os.Remove` and contains only ciphertext. On a containerized deployment with a private `/tmp`, this is a non-issue. On a shared host with a world-readable `/tmp`, an attacker who can list `/tmp` could capture the ciphertext.

**Fix**: Create the temp file inside `COMPOSER_DATA_DIR` (or a `tmp/` subdirectory) so it lives in a directory with controlled permissions, consistent with `encryption.key` and other secret-handling paths.

---

## 4. Error Handling

### E1 - `marshalCredentials` silently returns nil on all errors

**Severity**: Medium
**File**: `internal/infra/store/stack_repo.go:175-186`

```go
func marshalCredentials(creds *stack.GitCredentials) *string {
    if creds == nil { return nil }
    b, err := json.Marshal(creds)
    if err != nil { return nil }       // silently drops error
    encrypted, err := crypto.Encrypt(string(b))
    if err != nil { return nil }       // silently drops error
    return &encrypted
}
```

When `crypto.Encrypt` fails (no encryption key available, rand.Reader failure), the function returns `nil`. The caller in `GitConfigRepo.Upsert` passes this directly as the `credentials` parameter to the SQL INSERT -- so credentials are stored as SQL NULL and silently lost. The code comment says "fail closed" but the failure is invisible to the caller.

**Why it matters**: If `COMPOSER_ENCRYPTION_KEY` is not set and the auto-generation path fails (e.g., `COMPOSER_DATA_DIR` is not writable), git credentials are dropped from the database. The user's stack will lose its git auth on the next deployment cycle with no error surfaced anywhere -- not in the API response, not in logs, not in the UI.

**Fix**: Return the error alongside the pointer (`(*string, error)`) and propagate it to the caller. At minimum, log a warning at the call site so the operator knows credentials were dropped. The current signature hides failures entirely.

---

### E2 - CSRF middleware writes 403 error before consuming request body

**Severity**: Medium
**File**: `internal/api/middleware/csrf.go:60-62`

```go
http.Error(w, `{"status":403, ...}`, http.StatusForbidden)
return
```

The response is written without first draining the request body. For HTTP/1.1 connections with `Connection: keep-alive`, the unread request body can cause the next request on the same connection to read leftover bytes, producing parse errors.

**Why it matters**: Most modern HTTP clients and reverse proxies drain the body before reuse, but some clients or test harnesses may see spurious 400 errors after a CSRF rejection. This is a well-known HTTP/1.1 connection-reuse footgun.

**Fix**: Before writing the error response, drain the body with `io.Copy(io.Discard, r.Body)` and close it. Or set `Connection: close` on the response to force the client to open a new connection.

---

### E3 - InlineStats EventSource `onerror` handler swallows connection state

**Severity**: Low
**File**: `web/src/components/container/InlineStats.tsx:44`

```tsx
es.onerror = () => es.close();
```

The `onerror` handler closes the EventSource but does not surface the error to the user or trigger a retry. The component silently shows the last known stats (or `"--"` if no data was received) without any indication that the connection failed.

**Why it matters**: This is the fallback path (only fires when no parent component provides batch stats via props). If the SSE endpoint is temporarily unavailable, the component shows stale or empty data with no visual feedback. The 10-second polling interval eventually reconnects, but the user has no indication of the gap.

**Fix**: Track error state and display a subtle indicator (e.g., dimmed stats with a "stale" tooltip) when the EventSource fails. Alternatively, evaluate whether the fallback `useEffect` is needed at all -- if every parent provides batch stats, this code path is dead and should be removed to reduce surface area.

---

## 5. Architecture and DDD Boundary Violations

### A1 - Application layer directly imports concrete infrastructure packages

**Severity**: Medium
**Files**: `internal/app/git_service.go:13` (imports `internal/infra/sops`), `internal/app/git_service.go:10-12` (imports `internal/infra/docker`, `internal/infra/git`), `internal/app/stack_service.go:15-17` (same imports)

The application layer (`internal/app/`) directly imports and calls concrete infrastructure packages. For example, `git_service.go` calls `sops.IsAvailable()`, `sops.ResolveAgeKey()`, `sops.DecryptEnvFile()`, etc. In DDD, the application layer should depend on domain interfaces, not on infrastructure implementations.

**Why it matters**: The SOPS operations cannot be mocked or stubbed in application-layer unit tests. Any test that exercises `CreateGitStack` or `SyncAndRedeploy` must have the `sops` binary in PATH and real age keys configured, or it skips SOPS entirely. This pushes integration complexity into the application layer that should be isolated behind an interface.

**Fix**: Define a `SecretDecryptor` interface in `internal/domain/stack/` with `DecryptEnv(envPath, ageKey string) (bool, error)` and `ReEncryptEnv(envPath string) error` methods. Move the `sops` calls into an infra adapter that implements this interface. Pass the adapter through the constructor -- the same pattern already works for `registryRepo` via `SetRegistryRepo`.

---

### A2 - Pipeline executor depends on concrete Compose type, blocking unit-testability

**Severity**: Medium
**File**: `internal/app/pipeline_executor.go:27-33`

```go
type PipelineExecutor struct {
    compose      *docker.Compose   // concrete infra type
    docker       *docker.Client    // concrete infra type (for RunDocker and container ops)
    bus          event.Bus         // interface -- good
    stackRepo    stack.StackRepository   // interface
    gitCfgRepo   stack.GitConfigRepository // interface
    stacksDir    string
    locks        *StackLocks
}
```

All dependencies are injected via the constructor, which is good -- but `compose` and `docker` are concrete types, not interfaces. `docker.Compose` shells out to the `docker compose` CLI, making any unit test of `PipelineExecutor` require a real Docker daemon.

**Why it matters**: Pipeline execution is complex (multi-step DAG with dependency ordering, timeouts, error propagation, `ContinueOnError` branching) and would benefit from unit tests that exercise the orchestration logic without real Docker. Currently, the only way to test the executor is via integration tests with testcontainers.

**Fix**: Define a `ComposeRunner` interface in the domain layer with the methods `PipelineExecutor` actually calls (`Up`, `Down`, `Pull`, `Restart`, `RunDocker`, `RunPTY`). Accept the interface in the constructor instead of the concrete `*docker.Compose`. This mirrors the interface-based approach already used for `StackRepository` and `GitConfigRepository`.

---

## 6. Database and Migrations

### D1 - go.mod declares `go 1.26.1` (unreleased Go toolchain)

**Severity**: Medium
**File**: `go.mod:3`

```
go 1.26.1
```

Go 1.26 has not been released. Using an unreleased toolchain version in `go.mod` means:
- `go install github.com/erfianugrah/composer/cmd/composerd@latest` will fail for users on stable Go releases.
- The `go` directive in `go.mod` is a minimum-version promise; declaring a future version breaks that contract.
- This likely came from a `gotip` or `golang.org/dl/go1.26rc1` installation bleeding into the module.

**Why it matters**: The module is effectively unbuildable for anyone on a stable Go release. CI may work (if the CI runner is on the same RC), but downstream consumers and `go install` users get a toolchain error.

**Fix**: Set `go 1.24.0` (or the latest stable release) and run `go mod tidy` to validate. The codebase does not use any Go 1.25+ features that would require a higher minimum.

---

### D2 - Duplicate indexes in migration 003

**Severity**: Low
**File**: `internal/infra/store/migrations/003_add_indexes.sql:6-24`

Migration 003 creates six indexes that already exist in migration 001 under different names:

| 001 index | 003 index (duplicate) |
|-----------|----------------------|
| `idx_sessions_user` | `idx_sessions_user_id` |
| `idx_sessions_expires` | `idx_sessions_expires_at` |
| `idx_webhooks_stack` | `idx_webhooks_stack_name` |
| `idx_deliveries_webhook` | `idx_webhook_deliveries_webhook_id` |
| `idx_runs_pipeline` | `idx_pipeline_runs_pipeline_id` |
| `idx_audit_created` | `idx_audit_log_created_at` |

The migration's own comment acknowledges this. `CREATE INDEX IF NOT EXISTS` prevents errors, but the duplicates consume disk space and slow writes with no query benefit.

**Why it matters**: Storage cost is negligible for the table sizes Composer manages. It's primarily a hygiene issue -- anyone reading the schema sees 11 indexes instead of 6 and may waste time investigating.

**Fix**: Since Goose migrations are immutable once applied, add a new migration that drops the duplicate indexes. Or leave as-is -- the operational impact is zero for typical deployments.

---

### D3 - No composite index on `audit_log(user_id, created_at)`

**Severity**: Low
**File**: `internal/infra/store/migrations/001_initial.sql:115`

The audit log has a single-column index on `created_at DESC` (`idx_audit_created`) but no composite index on `(user_id, created_at)`. If the admin API ever supports filtering audit entries by user -- a natural feature for "show me what user X did" -- the query will table-scan.

**Why it matters**: No current code queries `audit_log` by `user_id`, so this is forward-looking. The audit cleanup goroutine (`main.go:260`) queries by `created_at` only, which is already covered by the existing index.

**Fix**: Add `CREATE INDEX IF NOT EXISTS idx_audit_user_created ON audit_log(user_id, created_at DESC)` preemptively, or defer until the query pattern materializes.

---

## 7. API and OpenAPI Consistency

### API1 - Stack `status` field is computed from live state, not persisted

**Severity**: Low
**File**: `internal/infra/store/stack_repo.go:53` (List), `internal/api/handler/stack.go:191-204` (List handler)

The repository always returns `StatusUnknown`. The handler enriches status from container state after the repository returns. The OpenAPI spec likely declares `status` with an enum that includes `unknown`, so the spec is consistent with the actual response. However, the fact that the persistence layer cannot answer "what is the stack's status?" means any future endpoint that reads a single stack without going through the service-layer enrichment will return `unknown` even for a running stack.

**Fix**: Document in the domain entity comment that `Status` is derived from live container state and is not stored. Or remove `Status` from the domain entity altogether and add it only to the DTO. The latter is cleaner and prevents the "forgot to enrich" bug class.

---

## 8. Frontend

### F1 - InlineStats EventSource not closed on component unmount; cancelled flag set but not read

**Severity**: Medium
**File**: `web/src/components/container/InlineStats.tsx:36-56`

The `useEffect` cleanup function sets `cancelled = true` and clears the interval, but does NOT close the EventSource:

```tsx
return () => { cancelled = true; clearInterval(interval); };
```

The `es.close()` call exists inside the `"stats"` event handler (line 43) and a 3-second timeout (line 46), but not in the cleanup. If the component unmounts while the SSE connection is waiting for its first event, the EventSource remains open -- delivering events to a handler that calls `setStats` on an unmounted component.

Additionally, the `cancelled` variable is set in the cleanup (`return () => { cancelled = true; ... }`) but the event handler at line 38-42 never reads it -- it calls `setStats(...)` unconditionally even after `cancelled` is true, producing the "state update on unmounted component" warning in React 18.

**Why it matters**: React 18 warns about state updates on unmounted components. The dangling SSE connection consumes a server-side goroutine and file descriptor until the 3-second timeout fires. In rapid navigation (user clicking through stacks quickly), multiple orphaned SSE connections accumulate.

**Fix**: Add `es.close()` to the cleanup function. Move the `cancelled` check into the handler to guard `setStats`:

```tsx
return () => {
    cancelled = true;
    clearInterval(interval);
    es.close();
};
```

---

### F2 - LogViewer `maxLines` prop not in dependency array

**Severity**: Low
**File**: `web/src/components/container/LogViewer.tsx:36` and `LogViewer.tsx:56`

```tsx
useEffect(() => {
    // ... uses maxLines inside setLines updater ...
    setLines((prev) => {
        if (prev.length < maxLines) { ... }
    });
    // ...
}, [containerId, stackName, tail]);  // maxLines missing
```

The `maxLines` value captured in the closure is the one from the initial render. If a parent changes `maxLines` dynamically, the log viewer ignores the new value.

**Why it matters**: Theoretical -- `maxLines` defaults to 1000 and is not currently changed by any parent. It's a latent bug that would surface if someone adds a "compact mode" toggle.

**Fix**: Add `maxLines` to the dependency array, or store it in a `useRef` (`maxLinesRef`) and read `maxLinesRef.current` inside the setter so the prop stays current without triggering SSE reconnection.

---

### F3 - ErrorBoundary has no `componentDidCatch` for error reporting

**Severity**: Low
**File**: `web/src/components/ui/ErrorBoundary.tsx:1-27`

The error boundary uses `getDerivedStateFromError` (correct for rendering a fallback UI) but does not implement `componentDidCatch` to log the error or component stack. Without `componentDidCatch`, runtime errors caught by the boundary are invisible -- no console output, no server-side report, no observability.

**Why it matters**: The UI shows "Something went wrong" with the error message, but the developer has no visibility into what crashed or where. In production, this means silent failures with no diagnostic trail.

**Fix**: Add `componentDidCatch(error: Error, info: React.ErrorInfo)` that logs `error` and `info.componentStack` to `console.error`. For production observability, consider posting the error to a server endpoint.

---

## 9. Testing Gaps

### T1 - No race detector in test execution

**Severity**: Low
**Files**: `Makefile` (test targets), `.github/workflows/ci.yml`

The codebase has goroutine-level concurrency (event bus dispatch, pipeline execution, job manager cleanup, session cleanup, cron scheduler, docker event listener, SOPS decrypt/re-encrypt cycles) but no evidence that `go test -race ./...` runs in CI. Neither `make test-unit` nor `make test-integration` is observed to pass the `-race` flag.

**Why it matters**: The StackLocks race (C1) and any other concurrent-access bugs in goroutine-heavy code would be caught by the race detector. Without it in CI, these bugs survive until a production deployment hits the right interleaving.

**Fix**: Add `go test -race -count=1 ./...` to CI (either as a separate job or as a flag on the existing test step). The race detector adds ~10x runtime overhead but for a codebase of this size, that's seconds, not minutes.

---

## 10. Dependency and Supply-Chain Risk

### D4 - Docker SDK `v28.5.2+incompatible`

**Severity**: Medium
**File**: `go.mod:9`

```
github.com/docker/docker v28.5.2+incompatible
```

The `+incompatible` suffix means the Docker SDK module has not adopted Go modules. This means the import path is `github.com/docker/docker` (no `/v28`), so `go mod tidy` can silently pull a different major version. The module also does not declare its own dependencies in a `go.mod`, so dependency resolution is based on whatever the importer declares -- drift between what the Docker SDK actually needs and what this `go.mod` provides is possible.

**Why it matters**: The Docker SDK is a critical dependency (every Docker API call goes through it). Without proper module versioning, `dependabot` or `go get -u` could upgrade to a breaking version without warning.

**Fix**: Consider migrating to the supported SDK at `github.com/docker/cli` + `github.com/moby/moby` packages. If staying on `docker/docker`, add a build-time check that the expected SDK version matches.

---

## Appendix: Verification Trail

All findings were verified by reading the actual source files at the cited locations. Key verifications:

| Finding | File | Lines | What was checked |
|---------|------|-------|-----------------|
| C1 | `stack_locks.go` | 30-42 | Two-phase lock/unlock pattern; Delete removes from map without interacting with per-stack mutex |
| C2 | `git_service.go`, `stack_service.go` | 128-140, 126-135 | Re-encrypt only in `else` block; `SyncAndRedeploy` uses `defer` for comparison |
| C3 | `diff.go` | 71-100 | DP table is (m+1)x(n+1); prepend `append([]string{...}, result...)` in backtrack loop |
| S1 | `sops.go` | 92 | `os.Environ()` + `SOPS_AGE_KEY=` appended to child process env |
| S2 | `middleware/security.go` | 23-26 | `script-src 'self' 'unsafe-inline'` in CSP header string |
| S3 | `middleware/csrf.go` | 51-62 | CSRF enforcement gated on `composer_session` cookie presence |
| S4 | `middleware/auth.go` | 192-206 | `extractAPIKey` strips Bearer prefix, no length/format validation |
| E1 | `store/stack_repo.go` | 175-186 | `marshalCredentials` signature is `func(...) *string` with nil on all errors |
| E2 | `middleware/csrf.go` | 60-62 | `http.Error` called without draining `r.Body` |
| F1 | `InlineStats.tsx` | 36-56 | `es` not in cleanup return; `cancelled` set but not checked in handler |
| F2 | `LogViewer.tsx` | 36, 56 | `maxLines` in closure body but not in dep array |
| D1 | `go.mod` | 3 | `go 1.26.1` |
| D2 | `migrations/003_add_indexes.sql` | 6-24 | Six indexes matching existing 001 indexes by different names |
| D4 | `go.mod` | 9 | `github.com/docker/docker v28.5.2+incompatible` |

### Event Bus Verification

The `internal/infra/eventbus/memory.go` implementation was read in full. It uses a `map[uint64]chan event.Event` protected by `sync.RWMutex`. `Publish` iterates subscribers under `RLock` with non-blocking sends (`select { case ch <- evt: default: ... }`). The implementation is correct and free of data races.
