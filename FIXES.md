# Code Review Fixes Tracker — v0.7.0

> Generated from full codebase review of Composer v0.6.9.
> Every finding verified by reading actual source. False positives removed.
> Branch: `fix/code-review-v0.7.0`

---

## Phase 1: Security (Critical + High)

### [S1] `serverError()` leaks internal errors to API clients
- **Severity:** CRITICAL
- **File:** `internal/api/handler/errors.go:16`
- **Issue:** `huma.Error500InternalServerError(err.Error())` sends raw Go errors (DB paths, Docker socket errors, filesystem paths) directly to API clients. 30+ call sites use `serverError()`. The existing `internalError()` fn returns a generic message but is dead code — never called.
- **Fix:** Change `serverError()` to return generic message to client. Log the actual error server-side with zap. Remove dead `internalError()` or unify.
- **Risk:** API clients relying on error text for parsing will break. Frontend `apiFetch` string-matching on error messages may need updates.
- **Status:** [ ] TODO

### [S2] `loginIPLimiter` declared but never wired
- **Severity:** CRITICAL
- **File:** `internal/api/handler/auth.go:23,116`
- **Issue:** `loginIPLimiter` is created at line 30 but never referenced in the `Login` handler. Only `loginLimiter` (per-email) is checked at line 116. An attacker can spray passwords across many emails from a single IP without rate limiting.
- **Fix:** Extract client IP from request context (use `r.RemoteAddr` or `X-Real-IP` if trusted proxies configured). Add `h.loginIPLimiter.Allow(clientIP)` check before the per-email check. Need to access the raw `http.Request` — Huma's context-based handlers don't expose it directly, so use `huma.WithContext` or middleware approach.
- **Risk:** None. Additive.
- **Status:** [ ] TODO

### [S5] No `.dockerignore` file
- **Severity:** CRITICAL
- **File:** repo root (missing)
- **Issue:** No `.dockerignore` exists. `deploy/Dockerfile` builds with context `..` (repo root). `COPY . .` sends entire repo to Docker daemon — `.git/`, any `.env` files, IDE configs, test fixtures. Secrets in working tree get baked into build layer cache.
- **Fix:** Create `.dockerignore` excluding: `.git`, `.github`, `*.md`, `docs/`, `e2e/`, `web/node_modules/`, `web/.astro/`, `composerd` (built binary), `.env*`, `*.log`, `deploy/unraid/`.
- **Risk:** None. Build should still work since Dockerfile only needs Go source + web dir.
- **Status:** [ ] TODO

### [S6] API key privilege escalation — operator can create admin key
- **Severity:** HIGH
- **File:** `internal/api/handler/keys.go:94-99`
- **Issue:** `Create` checks `auth.RoleOperator` (line 95) then parses `input.Body.Role` at line 99 accepting any valid role including `"admin"`. No check that caller's role >= requested role. An operator can create an admin-level API key.
- **Fix:** After parsing the role, add: `callerRole := authmw.RoleFromContext(ctx); if !callerRole.AtLeast(role) { return 403 }`. This ensures operators can only create operator/viewer keys, not admin.
- **Risk:** Operators who previously created admin keys will get 403. Correct behavior.
- **Status:** [ ] TODO

### [S7] OAuth auto-provisions viewer for any authenticating user
- **Severity:** HIGH
- **File:** `internal/api/handler/oauth.go:122-138`
- **Issue:** Line 124 sets `role = auth.RoleViewer` for new OAuth users with no approval process or domain restriction. If GitHub/Google OAuth is configured on a public app, **anyone with a GitHub/Google account** gets read access to all stacks, containers, logs.
- **Fix:** Add `COMPOSER_OAUTH_ALLOWED_DOMAINS` env var (comma-separated). In Callback, check `strings.HasSuffix(email, "@"+domain)` before auto-provisioning. If not in allowlist, return 403. If env var unset, allow all (backwards compat).
- **Risk:** Users with non-matching domains lose access. Document in release notes.
- **Status:** [ ] TODO

### [S9] HTTP pipeline step has no SSRF protection
- **Severity:** HIGH
- **File:** `internal/app/pipeline_executor.go:198-218`
- **Issue:** HTTP step validates scheme (http/https only) but doesn't block requests to private IPs: `127.0.0.1`, `169.254.169.254` (cloud metadata), `10.x`, `172.16-31.x`, `192.168.x`, `fd00::/8`. Admin can exfiltrate cloud credentials or probe internal services.
- **Fix:** After URL parse, resolve hostname to IP and check against private/link-local ranges. Use `net.LookupHost` + check `ip.IsLoopback()`, `ip.IsPrivate()`, `ip.IsLinkLocalUnicast()`. Block if any resolved IP is private.
- **Risk:** Could break legit pipelines hitting internal services. Add `COMPOSER_PIPELINE_ALLOW_PRIVATE_IPS=true` escape hatch.
- **Status:** [ ] TODO

### [S14] `git config safe.directory '*'` — wildcard trust
- **Severity:** HIGH
- **File:** `deploy/entrypoint.sh:44`
- **Issue:** Blanket `safe.directory '*'` disables git ownership checks for all directories. If attacker writes a malicious `.git` dir into any mounted volume, git trusts it.
- **Fix:** Change to `git config --global --add safe.directory '/opt/stacks'`. If other dirs needed, add them explicitly.
- **Risk:** Git ops in directories outside `/opt/stacks` will fail with "dubious ownership." This is the intended behavior.
- **Status:** [ ] TODO

### [S15] CI workflow missing `permissions` block
- **Severity:** HIGH
- **File:** `.github/workflows/ci.yml`
- **Issue:** No `permissions:` declared. Gets default repo-wide permissions (often `write-all` for push to main). Should restrict to minimum needed.
- **Fix:** Add top-level `permissions: contents: read` to `ci.yml`. Add per-job permissions to `release.yml` where needed.
- **Risk:** None for CI (only reads). Release needs `contents: write` + `packages: write` scoped per-job.
- **Status:** [ ] TODO

### [B1] `NewSession` doesn't validate role
- **Severity:** HIGH
- **File:** `internal/domain/auth/session.go:38-39`
- **Issue:** Only checks `role == ""`. Any garbage string like `Role("superadmin")` passes. Both `NewUser` and `NewAPIKey` call `role.Valid()` — this is inconsistent. Invalid role in session undermines middleware auth checks using `session.Role.AtLeast()`.
- **Fix:** Replace `if role == ""` with `if !role.Valid()` at line 38.
- **Risk:** None. Stricter validation.
- **Status:** [ ] TODO

### [B7] `WriteTimeout: 0` disables write deadlines on ALL endpoints
- **Severity:** HIGH
- **File:** `cmd/composerd/main.go:288`
- **Issue:** Comment says for SSE/WebSocket, but `WriteTimeout: 0` means a slow-read attacker on **any** endpoint (including regular API calls) can hold connections indefinitely. This is a DoS vector.
- **Fix:** Set `WriteTimeout: 30s` on the server. In SSE and WebSocket handlers, use `http.ResponseController` to call `SetWriteDeadline(time.Time{})` (disable) or extend per-request. This limits regular API endpoints while allowing long-lived streams.
- **Risk:** Must verify SSE/WS handlers still work after setting server-level timeout. Requires testing.
- **Status:** [ ] TODO

---

## Phase 2: Correctness (Medium)

### [M1] Missing `ReadHeaderTimeout` — slowloris vector
- **File:** `cmd/composerd/main.go:287`
- **Issue:** `ReadTimeout=30s` covers entire body read. Missing `ReadHeaderTimeout` means slowloris attacks can hold connections open during header-read phase. Go docs recommend setting both.
- **Fix:** Add `ReadHeaderTimeout: 10 * time.Second` to `http.Server`.
- **Status:** [ ] TODO

### [M2] Webhook secret partial display panics on short secrets
- **File:** `internal/api/handler/webhook_crud.go:136`
- **Issue:** `w.Secret[len(w.Secret)-4:]` panics with index-out-of-range if secret is < 4 bytes.
- **Fix:** Guard: `if len(w.Secret) >= 4 { "****" + last4 } else { "****" }`.
- **Status:** [ ] TODO

### [M3] Audit middleware unconditionally trusts `X-Real-IP`
- **File:** `internal/api/middleware/audit.go:38-39`
- **Issue:** `X-Real-IP` header trusted even when `COMPOSER_TRUSTED_PROXIES` is not set. Spoofable IP in audit logs. The chi `RealIP` middleware only runs conditionally (`server.go:55-57`), but audit always trusts the header.
- **Fix:** Only use `X-Real-IP` / `X-Forwarded-For` when trusted proxies are configured. Check the server config or context value. Otherwise use `r.RemoteAddr`.
- **Status:** [ ] TODO

### [M4] Sync compose deploy uses `context.Background()` with no timeout
- **File:** `internal/api/handler/stack.go:389`
- **Issue:** `h.stacks.Deploy(context.Background(), input.Name)` — no deadline. `docker compose up` could hang forever on slow image pulls or builds. Async path (via JobManager) also uses `context.Background()` at line 41.
- **Fix:** Create a context with 10-minute timeout for sync operations: `ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)`. Same for all sync compose ops (Stop, Restart, Pull, BuildAndDeploy).
- **Status:** [ ] TODO

### [M5] `ImportStacks` accepts arbitrary filesystem paths
- **File:** `internal/api/handler/stack.go:517`, `internal/app/stack_service.go:462-469`
- **Issue:** Admin-only, but `SourceDir` is an arbitrary absolute path. `stack_service.go:464-469` blocks known sensitive dirs (`/etc`, `/proc`, etc.) but uses `filepath.Abs` which doesn't resolve symlinks. Symlink → `/etc` bypasses the check.
- **Fix:** Use `filepath.EvalSymlinks` before the prefix check. Consider also requiring the path to be under a configurable base directory.
- **Status:** [ ] TODO

### [M6] Login flow: 4 DB ops, no transaction
- **File:** `internal/app/auth_service.go:91-127`
- **Issue:** `GetByEmail` → `DeleteByUserID` → `Create session` → `Update user`. If crash occurs after session revocation (line 106) but before session create (line 115), user has zero sessions and is locked out. Recoverable by re-login, but ideally atomic.
- **Fix:** Wrap lines 106-124 in a DB transaction. Requires adding `Tx(ctx, fn)` support to the session/user repos or using `store.Tx()` from `db.go`.
- **Status:** [ ] TODO

### [M7] `ConvertToLocal` deletes git config before updating stack
- **File:** `internal/app/stack_service.go:595-604`
- **Issue:** Line 596 deletes git config, line 603 updates stack source to `SourceLocal`. If the `Update` at line 603 fails, git config is already gone but stack still shows `SourceGit`. Inconsistent state.
- **Fix:** Reverse the order: update stack source first, then delete git config. If delete fails, stack is already marked local (acceptable degradation).
- **Status:** [ ] TODO

### [M8] Stack log SSE misparses Docker multiplex stream
- **File:** `internal/api/handler/sse.go:407-418`
- **Issue:** Uses raw `reader.Read(buf)` which can return partial frames, split headers, or multiple frames in one read. The 8-byte Docker header check (`if len(data) > 8 { if data[0] == 2 { stream = "stderr" } data = data[8:] }`) is fragile — a single Read can return 3 bytes (partial header) or 16000 bytes (multiple frames). Corrupts stdout/stderr attribution.
- **Fix:** Use `stdcopy.StdCopy` from Docker SDK, or implement proper 8-byte header parsing with a buffered reader that reads exactly 8 header bytes, extracts the payload length from bytes 4-7, then reads exactly that many payload bytes.
- **Status:** [ ] TODO

### [M9] `JobManager.Get`/`List` return mutable internal pointers
- **File:** `internal/app/jobs.go:98-102`
- **Issue:** `Get` returns `m.jobs[id]` directly — the same `*Job` stored in the map. Caller can mutate `Status`, `Output`, etc. outside the lock. `List` (lines 108-123) also returns pointers to internal state. Currently no caller mutates, but architecturally risky.
- **Fix:** Return copies: `j := *m.jobs[id]; return &j`. Same for `List`.
- **Status:** [ ] TODO

### [M10] Crypto key file write failure — no warning
- **File:** `internal/infra/crypto/encrypt.go:60-64`
- **Issue:** When `os.WriteFile` fails for the key file, code returns an in-memory-only key silently. Data encrypted this run becomes permanently undecryptable after restart. No warning is logged.
- **Fix:** Log a prominent warning using stderr (crypto package can't depend on zap). `fmt.Fprintf(os.Stderr, "WARNING: encryption key could not be persisted...")`. Or accept a logger param.
- **Status:** [ ] TODO

### [M11] `MaxOpenConns(25)` applied to SQLite
- **File:** `internal/infra/store/db.go:68`
- **Issue:** `SetMaxOpenConns(25)` applied to both Postgres AND SQLite. SQLite with WAL mode serializes writes. 25 concurrent connections cause `SQLITE_BUSY` errors despite the busy timeout pragma.
- **Fix:** Conditional: `if dbType == DBTypeSQLite { sqlDB.SetMaxOpenConns(1) } else { sqlDB.SetMaxOpenConns(25) }`.
- **Status:** [ ] TODO

### [M12] Duplicate indexes in migration 003
- **File:** `internal/infra/store/migrations/003_add_indexes.sql`
- **Issue:** Migration 001 already creates `idx_sessions_expires` on `sessions(expires_at)` and `idx_sessions_user` on `sessions(user_id)`. Migration 003 creates additional indexes on the same columns with different names. Result: duplicate indexes wasting space and slowing writes.
- **Fix:** Remove the duplicate index creations from 003, or add `DROP INDEX IF EXISTS` for the 001 indexes first. Since goose migrations are append-only, add a new migration 004 that drops the duplicates.
- **Status:** [ ] TODO

### [M13] `UpdateCredentials` nil pointer dereference
- **File:** `internal/app/stack_service.go:724-725`
- **Issue:** If `creds` is nil, accessing `creds.Token` at line 725 panics. Caller (`handler/stack.go:759`) always passes a non-nil pointer, but the function signature accepts `*GitCredentials` without nil guard.
- **Fix:** Add `if creds == nil { cfg.Credentials = nil; cfg.AuthMethod = stack.GitAuthNone; return s.gitCfgs.Upsert(...)  }` early return.
- **Status:** [ ] TODO

### [M14] CancelRun vs executor: logical race on DB persist
- **File:** `internal/app/pipeline_service.go:151-158`
- **Issue:** `CancelRun` calls `run.Cancel()` then `s.runs.Update(ctx, run)`. Concurrently, the executor goroutine finishes and calls `s.runs.Update(context.Background(), result)` at line 122. Last write wins — goroutine could overwrite "cancelled" with "success/failed".
- **Fix:** Use optimistic concurrency: add a `Version` field to `Run`, increment on each update, and check version in `UPDATE ... WHERE version = $N`. Or check status in the goroutine before persisting — if context was cancelled, skip the update.
- **Status:** [ ] TODO

### [M15] `dangerouslySetInnerHTML` with regex highlighting — fragile pattern
- **Files:** `web/src/components/container/LogViewer.tsx:175`, `web/src/components/docker/NetworksPage.tsx:78`, `web/src/components/docker/VolumesPage.tsx:75`
- **Issue:** Currently safe — `escapeHTML` runs first and no regex undoes the escaping. But the pattern is fragile: any future regex that manipulates `&lt;`/`&gt;` entities could introduce XSS. Three separate highlight functions (`log-highlight.ts`, `json-highlight.ts`, `dockerfile-highlight.ts`) all use this pattern.
- **Fix:** Long-term: migrate to React elements (return `JSX.Element[]` instead of HTML strings). Short-term: add DOMPurify as a safety net before `dangerouslySetInnerHTML`. Or add a comment-level audit trail documenting the safety invariant.
- **Status:** [ ] TODO

### [M16] SSE never reconnects on error
- **Files:** `web/src/components/container/ContainerStats.tsx:44`, `web/src/components/container/EventStream.tsx:67`
- **Issue:** On SSE error, both components close the EventSource but have no reconnect logic. Stats display and event stream go permanently blank on any transient network issue.
- **Fix:** Add exponential backoff reconnect: on error, wait 1s → 2s → 4s → max 30s, then retry. Use a ref to track retry count. Reset on successful connection.
- **Status:** [ ] TODO

### [M17] Auth redirect via string matching duplicated across 6+ components
- **Files:** `SystemConfig.tsx`, `DashboardOverview.tsx`, `ContainerListPage.tsx`, `UserManagement.tsx`, `PipelinePage.tsx`, `WebhookSettings.tsx`
- **Issue:** Each component checks `if (err.includes("Invalid credentials"))` or similar and does `window.location.href = "/login"`. Brittle — if backend changes error message, redirects break. Duplicated logic.
- **Fix:** Centralize in `apiFetch`: if response is 401, redirect to `/login`. Remove per-component string matching.
- **Status:** [ ] TODO

### [M18] Postgres default password in example compose
- **File:** `deploy/compose.yaml:69`
- **Issue:** `POSTGRES_PASSWORD: ${POSTGRES_PASSWORD:-composer}`. Users who miss setting the env var get an insecure DB. Not a code bug per se, but a deployment safety issue.
- **Fix:** Add a comment warning. Consider removing the default so compose fails loudly if not set, or add a startup check in the app.
- **Status:** [ ] TODO

### [M19] GitHub Actions pinned by tag not SHA
- **Files:** `.github/workflows/ci.yml`, `.github/workflows/release.yml`
- **Issue:** All actions use tag refs (`@v6`, `@v7`, `@v4`, `@v2`). Supply chain risk — compromised action tag affects all builds.
- **Fix:** Pin by full commit SHA with version comment: `uses: actions/checkout@<sha> # v6`.
- **Status:** [ ] TODO

---

## Phase 3: Polish (Low)

### [L1] `ErrorBoundary` defined but never used
- **File:** `web/src/components/ui/ErrorBoundary.tsx` (defined), never imported
- **Issue:** Any unhandled React error in CodeMirror, xterm, SSE, or any component crashes the entire Astro island with a white screen. No recovery.
- **Fix:** Wrap each `client:load` island in `<ErrorBoundary>` in the Astro pages/layouts. Show a "Something went wrong, click to reload" fallback.
- **Status:** [ ] TODO

### [L2] `internalError()` is dead code
- **File:** `internal/api/handler/errors.go:7-9`
- **Fix:** Remove or unify with `serverError()` as part of S1 fix.
- **Status:** [ ] TODO

### [L3] Magic error string for flow control in git Log
- **File:** `internal/infra/git/client.go:288-301`
- **Issue:** `return fmt.Errorf("limit reached")` used to break iteration. String comparison at line 301.
- **Fix:** Use sentinel error: `var errLimitReached = errors.New("limit reached")`.
- **Status:** [ ] TODO

### [L4] Terminal URL params not encoded
- **File:** `web/src/components/terminal/Terminal.tsx:85`
- **Issue:** `shell` and `containerId` interpolated without `encodeURIComponent`. Currently hardcoded/safe values, but defense-in-depth.
- **Fix:** `encodeURIComponent(shell)` and `encodeURIComponent(containerId)`.
- **Status:** [ ] TODO

### [L5] Stale comment about OpenAPI auth
- **File:** `internal/api/middleware/auth.go:27-28`
- **Issue:** Comment says "OpenAPI spec and docs require authentication (viewer+)" but code at line 131-132 deliberately makes non-`/api/` paths public.
- **Fix:** Update comment to match code behavior: "OpenAPI spec is served publicly (not under /api/ prefix)."
- **Status:** [ ] TODO

### [L6] `COOKIE_SECURE` defaults false
- **File:** `deploy/compose.yaml:40`
- **Issue:** `COMPOSER_COOKIE_SECURE` defaults to `false`. Production deployments behind HTTPS will miss this.
- **Fix:** Already handled in code (`auth.go:131` defaults to `true` unless explicitly `"false"`). The compose.yaml default just needs to match: change to `${COOKIE_SECURE:-true}` or remove the default.
- **Status:** [ ] TODO

### [L7] Bubble sort in `JobManager.List`
- **File:** `internal/app/jobs.go:113-119`
- **Issue:** O(n²) sort. Fine for <=100 jobs but trivially replaceable.
- **Fix:** Use `slices.SortFunc`.
- **Status:** [ ] TODO

### [L8] No release signing or provenance
- **File:** `.github/workflows/release.yml`
- **Issue:** No cosign, no SLSA provenance on Docker images or binaries.
- **Fix:** Add `provenance: true` and `sbom: true` to `docker/build-push-action`. Add cosign sign step. Separate effort — tracked here for completeness.
- **Status:** [ ] TODO

---

## Summary

| Phase | Severity | Count | Estimated Effort |
|-------|----------|-------|------------------|
| 1 | CRITICAL | 3 | Small |
| 1 | HIGH | 7 | Small–Medium |
| 2 | MEDIUM | 19 | Medium |
| 3 | LOW | 8 | Small |
| **Total** | | **37** | |

## Notes

- All findings verified by reading actual source code at exact file:line references
- 16 original claims were downgraded or removed as false positives after verification
- Key false positives: encryption key auto-generates (not "no encryption"), CSRF bypass blocked by CORS, XSS escaping is currently correct, RecordStepResult ContinueOnError works via executor compensation
