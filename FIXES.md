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
- **Fix:** Changed `serverError()` to return generic message. Logs actual error via `slog.Error`. Removed dead `internalError()`.
- **Status:** [x] DONE

### [S2] `loginIPLimiter` declared but never wired
- **Severity:** CRITICAL
- **File:** `internal/api/handler/auth.go:23,116`
- **Issue:** `loginIPLimiter` created but never used in `Login`. Attacker can spray passwords across emails from single IP.
- **Fix:** Added `StoreRemoteIP` middleware + `RemoteIPFromContext` helper. Wired `loginIPLimiter.Allow(clientIP)` before per-email check.
- **Status:** [x] DONE

### [S5] No `.dockerignore` file
- **Severity:** CRITICAL
- **File:** repo root (missing)
- **Issue:** No `.dockerignore`. `COPY . .` sends `.git/`, `.env`, credentials into build context.
- **Fix:** Created `.dockerignore` excluding `.git`, `.github`, docs, tests, node_modules, build artifacts, env files.
- **Status:** [x] DONE

### [S6] API key privilege escalation — operator can create admin key
- **Severity:** HIGH
- **File:** `internal/api/handler/keys.go:94-99`
- **Issue:** No check that caller's role >= requested role. Operator can create admin API key.
- **Fix:** Added `callerRole.AtLeast(role)` check after role parse. Returns 403 if escalation attempted.
- **Status:** [x] DONE

### [S7] OAuth auto-provisions viewer for any authenticating user
- **Severity:** HIGH
- **File:** `internal/api/handler/oauth.go:122-138`
- **Issue:** Anyone with GitHub/Google account gets viewer access. No domain restriction.
- **Fix:** Added `COMPOSER_OAUTH_ALLOWED_DOMAINS` env var (comma-separated). Checks email suffix before auto-provisioning. Existing users bypass check. Unset = allow all (backwards compat).
- **Status:** [x] DONE

### [S9] HTTP pipeline step has no SSRF protection
- **Severity:** HIGH
- **File:** `internal/app/pipeline_executor.go:198-218`
- **Issue:** HTTP step doesn't block private/link-local/loopback IPs. Cloud metadata exfiltration possible.
- **Fix:** Added `validateHTTPTarget()` — resolves hostname, blocks `IsLoopback()`, `IsPrivate()`, `IsLinkLocalUnicast()`. Escape hatch: `COMPOSER_PIPELINE_ALLOW_PRIVATE_IPS=true`.
- **Status:** [x] DONE

### [S14] `git config safe.directory '*'` — wildcard trust
- **Severity:** HIGH
- **File:** `deploy/entrypoint.sh:44`
- **Issue:** Wildcard disables all git ownership checks.
- **Fix:** Scoped to `/opt/stacks` only.
- **Status:** [x] DONE

### [S15] CI workflow missing `permissions` block
- **Severity:** HIGH
- **File:** `.github/workflows/ci.yml`
- **Issue:** No `permissions:` = default write-all on push to main.
- **Fix:** Added `permissions: contents: read` at top level.
- **Status:** [x] DONE

### [B1] `NewSession` doesn't validate role
- **Severity:** HIGH
- **File:** `internal/domain/auth/session.go:38-39`
- **Issue:** Only checks `role == ""`. Garbage role passes.
- **Fix:** Changed to `if !role.Valid()`.
- **Status:** [x] DONE

### [B7] `WriteTimeout: 0` disables write deadlines on ALL endpoints
- **Severity:** HIGH
- **File:** `cmd/composerd/main.go:288`
- **Issue:** Slow-read DoS on non-SSE endpoints.
- **Fix:** Set `WriteTimeout: 60s` + `ReadHeaderTimeout: 10s`. Added `ExtendWriteDeadline` middleware that disables deadline for `/sse/` and `/ws/` paths via `http.ResponseController`.
- **Status:** [x] DONE

---

## Phase 2: Correctness (Medium)

### [M1] Missing `ReadHeaderTimeout` — slowloris vector
- **File:** `cmd/composerd/main.go:287`
- **Fix:** Added as part of B7 fix.
- **Status:** [x] DONE

### [M2] Webhook secret partial display panics on short secrets
- **File:** `internal/api/handler/webhook_crud.go:136`
- **Fix:** Added `len(w.Secret) >= 4` guard at both Get and Update endpoints.
- **Status:** [x] DONE

### [M3] Audit middleware unconditionally trusts `X-Real-IP`
- **File:** `internal/api/middleware/audit.go:38-39`
- **Fix:** Only trusts `X-Real-IP` when `COMPOSER_TRUSTED_PROXIES` env set.
- **Status:** [x] DONE

### [M4] Sync compose deploy uses `context.Background()` with no timeout
- **File:** `internal/api/handler/stack.go:389`
- **Fix:** All 5 sync compose ops (Deploy, BuildAndDeploy, Stop, Restart, Pull) now use `context.WithTimeout(context.Background(), 10*time.Minute)`. Async `runAsync` unchanged.
- **Status:** [x] DONE

### [M5] `ImportStacks` accepts arbitrary filesystem paths
- **File:** `internal/app/stack_service.go:462-469`
- **Issue:** Symlink bypass on path traversal check.
- **Status:** [ ] DEFERRED — admin-only, existing blocklist adequate for now. Needs `filepath.EvalSymlinks`.

### [M6] Login flow: 4 DB ops, no transaction
- **File:** `internal/app/auth_service.go:91-127`
- **Issue:** Crash between session revocation and creation = locked out. Recoverable by re-login.
- **Status:** [ ] DEFERRED — requires transaction support refactor across repos.

### [M7] `ConvertToLocal` deletes git config before updating stack
- **File:** `internal/app/stack_service.go:595-604`
- **Fix:** Reversed order — update stack source first, then delete git config.
- **Status:** [x] DONE

### [M8] Stack log SSE misparses Docker multiplex stream
- **File:** `internal/api/handler/sse.go:407-418`
- **Issue:** Raw `reader.Read(buf)` can split/merge Docker multiplex frames.
- **Status:** [ ] DEFERRED — needs proper stdcopy implementation. Larger refactor.

### [M9] `JobManager.Get`/`List` return mutable internal pointers
- **File:** `internal/app/jobs.go:98-102`
- **Fix:** `Get` and `List` now return struct copies.
- **Status:** [x] DONE

### [M10] Crypto key file write failure — no warning
- **File:** `internal/infra/crypto/encrypt.go:60-64`
- **Fix:** Added `fmt.Fprintf(os.Stderr, ...)` warning.
- **Status:** [x] DONE

### [M11] `MaxOpenConns(25)` applied to SQLite
- **File:** `internal/infra/store/db.go:68`
- **Fix:** SQLite gets `MaxOpenConns(1)`, Postgres keeps 25.
- **Status:** [x] DONE

### [M12] Duplicate indexes in migration 003
- **File:** `internal/infra/store/migrations/003_add_indexes.sql`
- **Fix:** Added comment documenting overlap with 001. Kept as-is (goose immutability + IF NOT EXISTS).
- **Status:** [x] DONE (documented)

### [M13] `UpdateCredentials` nil pointer dereference
- **File:** `internal/app/stack_service.go:724-725`
- **Fix:** Added nil check on `creds` before accessing fields.
- **Status:** [x] DONE

### [M14] CancelRun vs executor: logical race on DB persist
- **File:** `internal/app/pipeline_service.go:151-158`
- **Issue:** Last-write-wins on DB between cancel and executor goroutine.
- **Status:** [ ] DEFERRED — needs optimistic concurrency or version field.

### [M15] `dangerouslySetInnerHTML` with regex highlighting — fragile pattern
- **Files:** `LogViewer.tsx:175`, `NetworksPage.tsx:78`, `VolumesPage.tsx:75`
- **Issue:** Currently safe but fragile. Escaping runs first, no regex undoes it.
- **Status:** [ ] DEFERRED — long-term migration to React elements.

### [M16] SSE never reconnects on error
- **Files:** `ContainerStats.tsx:44`, `EventStream.tsx:67`
- **Status:** [ ] DEFERRED — needs reconnect with exponential backoff.

### [M17] Auth redirect via string matching duplicated across 6+ components
- **Status:** [ ] DEFERRED — needs centralized 401 handling in apiFetch.

### [M18] Postgres default password in example compose
- **File:** `deploy/compose.yaml:69`
- **Status:** [ ] DEFERRED — documentation/comment improvement.

### [M19] GitHub Actions pinned by tag not SHA
- **Status:** [ ] DEFERRED — needs SHA lookup for each action version.

---

## Phase 3: Polish (Low)

### [L1] `ErrorBoundary` defined but never used
- **Status:** [ ] DEFERRED — needs wrapping each Astro island.

### [L2] `internalError()` is dead code
- **Fix:** Removed as part of S1 fix.
- **Status:** [x] DONE

### [L3] Magic error string for flow control in git Log
- **File:** `internal/infra/git/client.go:288-301`
- **Fix:** Added `errLimitReached` sentinel, replaced string comparison with `errors.Is()`.
- **Status:** [x] DONE

### [L4] Terminal URL params not encoded
- **File:** `web/src/components/terminal/Terminal.tsx:85`
- **Fix:** Added `encodeURIComponent()` for `containerId` and `shell`.
- **Status:** [x] DONE

### [L5] Stale comment about OpenAPI auth
- **File:** `internal/api/middleware/auth.go:27-28`
- **Fix:** Updated comment to reflect actual behavior (publicly accessible).
- **Status:** [x] DONE

### [L6] `COOKIE_SECURE` defaults false
- **File:** `deploy/compose.yaml:40`
- **Fix:** Added warning comment about production HTTPS.
- **Status:** [x] DONE

### [L7] Bubble sort in `JobManager.List`
- **File:** `internal/app/jobs.go:113-119`
- **Fix:** Replaced with `slices.SortFunc`.
- **Status:** [x] DONE

### [L8] No release signing or provenance
- **Status:** [ ] DEFERRED — separate effort (cosign/SLSA).

---

## Summary

| Status | Count |
|--------|-------|
| DONE | 25 |
| DEFERRED | 12 |
| **Total** | **37** |

### Deferred items rationale:
- **M5, M6, M8, M14**: Require larger refactors (transactions, stdcopy, optimistic concurrency)
- **M15, M16, M17, L1**: Frontend changes requiring design decisions
- **M18, M19, L8**: CI/deployment improvements not blocking release

## Notes

- All findings verified by reading actual source code at exact file:line references
- 16 original claims were downgraded or removed as false positives after verification
- Key false positives: encryption key auto-generates (not "no encryption"), CSRF bypass blocked by CORS, XSS escaping is currently correct, RecordStepResult ContinueOnError works via executor compensation
- All tests pass after fixes: domain, app, infra (crypto, eventbus, sops, cache, notify, git)
