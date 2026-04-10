# Code Review Fixes

Comprehensive code review of the Composer codebase covering security, UX, and performance.
Each finding includes the exact file/line, the problematic code, and a concrete fix.

## Status Summary (as of v0.6.2)

### Security (S1-S32): 22 fixed, 3 acknowledged, 7 remaining
- **FIXED**: S1, S3, S4, S5, S6, S7, S8, S9, S10, S11, S12, S13, S14, S15, S17, S18, S21, S22, S25, S26, S30, S32
- **ACKNOWLEDGED**: S16 (error messages -- intentional), S28 (audit context -- safe), S29 (HSTS -- fixed), S31 (XSS header -- fixed)
- **REMAINING**: S2 (SSH host key -- needs known_hosts), S19 (CSP nonces -- needs Astro integration), S20 (rate limiter composite key), S23 (SOPS crash -- defer in place), S24 (API key cache -- needs Valkey), S27 (connection limits)

### UX (U1-U27): 25 fixed, 2 acknowledged, 0 remaining
- **FIXED**: U1, U2, U3, U4, U5, U6, U7, U8, U9, U10, U11, U12, U13, U14, U15, U16, U17, U19, U21, U22, U23, U24, U25, U26, U27
- **ACKNOWLEDGED**: U20 (dangerouslySetInnerHTML -- all highlighters HTML-escape first), U18 (setTimeout refresh -- works acceptably)
- **ALL UX ITEMS ADDRESSED**

### Performance (P1-P22): 11 fixed, 11 remaining
- **FIXED**: P4 (DB pool config), P5 (SSE reconnect hook), P7-P8 (font-display swap), P9 (Vite manual chunks), P13 (audit/delivery TTL), P14 (DB indexes), P16 (compose buffer limit), P19 (Docker init timeout), P20 (Docker multiplex), P22 (cache-control)
- **REMAINING**: P1 (SSE batch stats -- needs new endpoint), P2 (Valkey auth cache -- needs plumbing), P3 (N+1 stacks -- needs batch Docker query), P6 (log virtualization), P10 (codemirror meta-package), P11 (request dedup), P12 (resolveComposeFile query), P15 (cron N+1), P17 (webhook goroutine timeout), P18 (event listener reconnect), P21 (log array spread)

---

## Table of Contents

- [Security](#security)
  - [S1. Encrypt() silently returns plaintext on key failure](#s1-encrypt-silently-returns-plaintext-on-key-failure)
  - [S2. SSH host key verification disabled globally](#s2-ssh-host-key-verification-disabled-globally)
  - [S3. Pipeline privilege escalation -- operators can inject shell commands](#s3-pipeline-privilege-escalation----operators-can-inject-shell-commands)
  - [S4. OAuth callback missing session fixation prevention](#s4-oauth-callback-missing-session-fixation-prevention)
  - [S5. OAuth state cookie missing Secure flag](#s5-oauth-state-cookie-missing-secure-flag)
  - [S6. WebSocket terminal has no shell allowlist](#s6-websocket-terminal-has-no-shell-allowlist)
  - [S7. WebSocket terminal has no container scope validation](#s7-websocket-terminal-has-no-container-scope-validation)
  - [S8. Password change does not invalidate sessions](#s8-password-change-does-not-invalidate-sessions)
  - [S9. User deletion does not cascade session/API key revocation](#s9-user-deletion-does-not-cascade-sessionapi-key-revocation)
  - [S10. Stack name validation allows `..` path traversal](#s10-stack-name-validation-allows--path-traversal)
  - [S11. SSHKeyFile path allows arbitrary file reads](#s11-sshkeyfile-path-allows-arbitrary-file-reads)
  - [S12. ImportFromDir has no path restriction](#s12-importfromdir-has-no-path-restriction)
  - [S13. Docker compose exec allowlist includes dangerous subcommands](#s13-docker-compose-exec-allowlist-includes-dangerous-subcommands)
  - [S14. Docker global exec allowlist includes `compose`](#s14-docker-global-exec-allowlist-includes-compose)
  - [S15. Templates API bypasses authentication](#s15-templates-api-bypasses-authentication)
  - [S16. Internal error messages leak to clients](#s16-internal-error-messages-leak-to-clients)
  - [S17. Webhook secret encryption errors silently discarded](#s17-webhook-secret-encryption-errors-silently-discarded)
  - [S18. Stack credential encryption falls back to plaintext](#s18-stack-credential-encryption-falls-back-to-plaintext)
  - [S19. CSP allows unsafe-inline for scripts](#s19-csp-allows-unsafe-inline-for-scripts)
  - [S20. Login rate limiter keyed on email only](#s20-login-rate-limiter-keyed-on-email-only)
  - [S21. Compose files written world-readable](#s21-compose-files-written-world-readable)
  - [S22. Data directory created with 0755 permissions](#s22-data-directory-created-with-0755-permissions)
  - [S23. SOPS decryption writes plaintext to disk with no crash-safe cleanup](#s23-sops-decryption-writes-plaintext-to-disk-with-no-crash-safe-cleanup)
  - [S24. API key deletion cannot invalidate cache](#s24-api-key-deletion-cannot-invalidate-cache)
  - [S25. WebSocket origin validated against spoofable Host header](#s25-websocket-origin-validated-against-spoofable-host-header)
  - [S26. No WebSocket read size limit](#s26-no-websocket-read-size-limit)
  - [S27. No per-user connection limit for WebSocket/SSE](#s27-no-per-user-connection-limit-for-websocketsse)
  - [S28. Audit log goroutine reads cancelled request context](#s28-audit-log-goroutine-reads-cancelled-request-context)
  - [S29. HSTS header trusts unsanitized X-Forwarded-Proto](#s29-hsts-header-trusts-unsanitized-x-forwarded-proto)
  - [S30. OAuth error leaks internal details](#s30-oauth-error-leaks-internal-details)
  - [S31. X-XSS-Protection header is obsolete](#s31-x-xss-protection-header-is-obsolete)
  - [S32. generateID() fallback uses deterministic data](#s32-generateid-fallback-uses-deterministic-data)
- [UX](#ux)
  - [U1. No mobile navigation](#u1-no-mobile-navigation)
  - [U2. Container actions silently swallow errors](#u2-container-actions-silently-swallow-errors)
  - [U3. Pipeline run/delete errors silently discarded](#u3-pipeline-rundelete-errors-silently-discarded)
  - [U4. Template create/fetch errors silently discarded](#u4-template-createfetch-errors-silently-discarded)
  - [U5. GitStatus fetch errors silently dropped](#u5-gitstatus-fetch-errors-silently-dropped)
  - [U6. No React Error Boundary](#u6-no-react-error-boundary)
  - [U7. Validate button has unconditional alert() bug](#u7-validate-button-has-unconditional-alert-bug)
  - [U8. ComposeEditor destroys and recreates on every content change](#u8-composeeditor-destroys-and-recreates-on-every-content-change)
  - [U9. No password confirmation on bootstrap](#u9-no-password-confirmation-on-bootstrap)
  - [U10. Tab navigation has no ARIA roles](#u10-tab-navigation-has-no-aria-roles)
  - [U11. Command palette lacks dialog role and focus trap](#u11-command-palette-lacks-dialog-role-and-focus-trap)
  - [U12. Jobs drawer lacks dialog role and focus trap](#u12-jobs-drawer-lacks-dialog-role-and-focus-trap)
  - [U13. Form labels not associated with inputs](#u13-form-labels-not-associated-with-inputs)
  - [U14. Select elements have no accessible labels](#u14-select-elements-have-no-accessible-labels)
  - [U15. Stack action buttons overflow on small screens](#u15-stack-action-buttons-overflow-on-small-screens)
  - [U16. Grid forms don't stack on mobile](#u16-grid-forms-dont-stack-on-mobile)
  - [U17. window.alert() and window.prompt() used for UX](#u17-windowalert-and-windowprompt-used-for-ux)
  - [U18. setTimeout(fetchStack, 1000) blind refresh pattern](#u18-settimeoutfetchstack-1000-blind-refresh-pattern)
  - [U19. No unsaved-changes warning in editors](#u19-no-unsaved-changes-warning-in-editors)
  - [U20. dangerouslySetInnerHTML without HTML escaping audit](#u20-dangerouslysetinnerhtml-without-html-escaping-audit)
  - [U21. No auto-refresh on Dashboard](#u21-no-auto-refresh-on-dashboard)
  - [U22. Keyboard shortcut shows macOS-only symbol](#u22-keyboard-shortcut-shows-macos-only-symbol)
  - [U23. No client-side stack name validation](#u23-no-client-side-stack-name-validation)
  - [U24. GitCloneForm doesn't validate repository URL](#u24-gitcloneform-doesnt-validate-repository-url)
  - [U25. Terminal resize not debounced](#u25-terminal-resize-not-debounced)
  - [U26. Console command history not persisted](#u26-console-command-history-not-persisted)
  - [U27. Native select dropdowns unreadable on dark theme](#u27-native-select-dropdowns-unreadable-on-dark-theme)
- [Performance](#performance)
  - [P1. Per-container SSE connection exhausts browser limits](#p1-per-container-sse-connection-exhausts-browser-limits)
  - [P2. Valkey cache exists but is never used for auth](#p2-valkey-cache-exists-but-is-never-used-for-auth)
  - [P3. N+1 Docker API calls in stack listing](#p3-n1-docker-api-calls-in-stack-listing)
  - [P4. No database connection pool configuration](#p4-no-database-connection-pool-configuration)
  - [P5. SSE onerror kills connections permanently](#p5-sse-onerror-kills-connections-permanently)
  - [P6. LogViewer renders all lines without virtualization](#p6-logviewer-renders-all-lines-without-virtualization)
  - [P7. Font imports ship all subsets](#p7-font-imports-ship-all-subsets)
  - [P8. No font-display swap causes invisible text](#p8-no-font-display-swap-causes-invisible-text)
  - [P9. No Vite manualChunks -- React invalidated every deploy](#p9-no-vite-manualchunks----react-invalidated-every-deploy)
  - [P10. codemirror meta-package included alongside individual packages](#p10-codemirror-meta-package-included-alongside-individual-packages)
  - [P11. No request deduplication or caching in apiFetch](#p11-no-request-deduplication-or-caching-in-apifetch)
  - [P12. resolveComposeFile re-queries DB redundantly](#p12-resolvecomposefile-re-queries-db-redundantly)
  - [P13. No TTL/retention for audit_log and webhook_deliveries](#p13-no-ttlretention-for-audit_log-and-webhook_deliveries)
  - [P14. Missing database indexes](#p14-missing-database-indexes)
  - [P15. N+1 query in CronScheduler.checkSchedules](#p15-n1-query-in-cronschedulercheckschedules)
  - [P16. Compose.run buffers all stdout/stderr unbounded](#p16-composerun-buffers-all-stdoutstderr-unbounded)
  - [P17. Webhook handler fires goroutine with no timeout or shutdown hook](#p17-webhook-handler-fires-goroutine-with-no-timeout-or-shutdown-hook)
  - [P18. Docker event listener reconnect ignores context cancellation](#p18-docker-event-listener-reconnect-ignores-context-cancellation)
  - [P19. Docker client initialization blocks indefinitely](#p19-docker-client-initialization-blocks-indefinitely)
  - [P20. StreamStackLogs incorrect Docker multiplex parsing](#p20-streamstacklogs-incorrect-docker-multiplex-parsing)
  - [P21. Log array spread on every SSE event](#p21-log-array-spread-on-every-sse-event)
  - [P22. No cache-control headers for static assets](#p22-no-cache-control-headers-for-static-assets)

---

## Security

### S1. Encrypt() silently returns plaintext on key failure

**Severity**: Critical
**File**: `internal/infra/crypto/encrypt.go:86-89`

If `getKey()` fails, `Encrypt()` returns the plaintext input with a nil error. All callers
(stack repo, webhook repo) treat the return value as encrypted. SSH keys, tokens, and passwords
are stored in cleartext in the database with no warning.

```go
// Current
key, err := getKey()
if err != nil {
    return plaintext, nil
}
```

**Fix**:

```go
key, err := getKey()
if err != nil {
    return "", fmt.Errorf("encryption key unavailable: %w", err)
}
```

This forces callers to handle the failure explicitly rather than silently storing cleartext.

---

### S2. SSH host key verification disabled globally

**Severity**: Critical
**File**: `internal/infra/git/client.go:34,46`

All SSH git operations use `sshlib.InsecureIgnoreHostKey()`. An attacker who can intercept DNS
or the network can impersonate the git remote, serve a malicious repository with a weaponized
`compose.yaml`, and gain container execution when the stack auto-deploys via webhook.

```go
// Line 34
keys.HostKeyCallback = sshlib.InsecureIgnoreHostKey()
// Line 46
keys.HostKeyCallback = sshlib.InsecureIgnoreHostKey()
```

**Fix**:

```go
import "github.com/go-git/go-git/v5/plumbing/transport/ssh"
import sshlib "golang.org/x/crypto/ssh"

func hostKeyCallback() (sshlib.HostKeyCallback, error) {
    knownHostsFile := os.Getenv("COMPOSER_SSH_KNOWN_HOSTS")
    if knownHostsFile == "" {
        knownHostsFile = filepath.Join(os.Getenv("COMPOSER_DATA_DIR"), "known_hosts")
    }
    if _, err := os.Stat(knownHostsFile); err == nil {
        return ssh.NewKnownHostsCallback(knownHostsFile)
    }
    // Fallback: only if explicitly opted in
    if os.Getenv("COMPOSER_SSH_INSECURE_HOST_KEY") == "true" {
        return sshlib.InsecureIgnoreHostKey(), nil
    }
    return nil, fmt.Errorf("no known_hosts file at %s; set COMPOSER_SSH_INSECURE_HOST_KEY=true to skip verification", knownHostsFile)
}
```

Ship default `known_hosts` entries for GitHub, GitLab, and Gitea in the Docker image.

---

### S3. Pipeline privilege escalation -- operators can inject shell commands

**Severity**: Critical
**File**: `internal/api/handler/pipeline.go:242-243`

Pipeline creation correctly requires `RoleAdmin` (line 88), but pipeline update only requires
`RoleOperator` (line 243). An operator can modify any pipeline to inject a `shell_command` step,
then execute it. The shell command runs on the host at `pipeline_executor.go:172`:

```go
cmd := exec.CommandContext(ctx, "sh", "-c", command)
```

```go
// pipeline.go:242-243 -- Update only requires operator
func (h *PipelineHandler) Update(ctx context.Context, input *UpdatePipelineInput) (*dto.PipelineCreatedOutput, error) {
    if err := authmw.CheckRole(ctx, auth.RoleOperator); err != nil {
        return nil, err
    }
```

**Fix**: Require admin for any pipeline update that contains shell steps:

```go
func (h *PipelineHandler) Update(ctx context.Context, input *UpdatePipelineInput) (*dto.PipelineCreatedOutput, error) {
    // Base permission: operator can update pipelines
    if err := authmw.CheckRole(ctx, auth.RoleOperator); err != nil {
        return nil, err
    }

    // Escalated permission: shell/docker steps require admin
    for _, s := range input.Body.Steps {
        if s.Type == "shell_command" || s.Type == "docker_exec" {
            if err := authmw.CheckRole(ctx, auth.RoleAdmin); err != nil {
                return nil, huma.Error403Forbidden("shell_command and docker_exec steps require admin role")
            }
            break
        }
    }
    // ... rest of handler
```

---

### S4. OAuth callback missing session fixation prevention

**Severity**: Critical
**File**: `internal/api/handler/oauth.go:140-150`

The OAuth callback creates a new session without revoking existing sessions for the user.
Compare with the local login path at `auth_service.go:93-97` which explicitly calls
`s.sessions.DeleteByUserID()`. An attacker who stole an old session token retains access even
after the user re-authenticates via OAuth.

```go
// oauth.go:140-150 -- no session revocation before creating new session
session, err := auth.NewSession(user.ID, user.Role, 24*time.Hour)
if err != nil { ... }
if err := h.sessions.Create(r.Context(), session); err != nil { ... }
```

**Fix**: Add session revocation before creating the new session, mirroring the local login flow:

```go
// Revoke existing sessions (session fixation prevention)
_ = h.sessions.DeleteByUserID(r.Context(), user.ID)

session, err := auth.NewSession(user.ID, user.Role, 24*time.Hour)
```

---

### S5. OAuth state cookie missing Secure flag

**Severity**: Critical
**File**: `internal/api/handler/oauth.go:73-77`

The gorilla/sessions cookie store for the OAuth flow state sets `HttpOnly` and `SameSite` but
never sets `Secure = true`. The OAuth state token (which prevents CSRF during OAuth) can be
intercepted over plaintext HTTP. The main application session cookie at line 153 correctly
reads `COMPOSER_COOKIE_SECURE`, but this store does not.

```go
store := sessions.NewCookieStore([]byte(key))
store.MaxAge(300)
store.Options.HttpOnly = true
store.Options.SameSite = http.SameSiteLaxMode
// Missing: store.Options.Secure
gothic.Store = store
```

**Fix**:

```go
store := sessions.NewCookieStore([]byte(key))
store.MaxAge(300)
store.Options.HttpOnly = true
store.Options.SameSite = http.SameSiteLaxMode
store.Options.Secure = os.Getenv("COMPOSER_COOKIE_SECURE") != "false"
gothic.Store = store
```

---

### S6. WebSocket terminal has no shell allowlist

**Severity**: High
**File**: `internal/api/ws/terminal.go:40-43`

The `shell` query parameter is passed directly to `ExecAttach` with no validation. An
authenticated operator could specify any binary (`?shell=/usr/bin/python3`,
`?shell=/usr/bin/wget`) to execute inside the target container.

Note: The endpoint IS authenticated and RBAC-protected (operator+) via `server.go:187-188`.

```go
shell := r.URL.Query().Get("shell")
if shell == "" {
    shell = "/bin/sh"
}
// ...
exec, err := h.dockerClient.ExecAttach(ctx, containerID, []string{shell}, true)
```

**Fix**:

```go
shell := r.URL.Query().Get("shell")
if shell == "" {
    shell = "/bin/sh"
}

allowedShells := map[string]bool{
    "/bin/sh": true, "/bin/bash": true, "/bin/ash": true, "/bin/zsh": true,
}
if !allowedShells[shell] {
    http.Error(w, "shell not allowed; permitted: /bin/sh, /bin/bash, /bin/ash, /bin/zsh", http.StatusBadRequest)
    return
}
```

---

### S7. WebSocket terminal has no container scope validation

**Severity**: High
**File**: `internal/api/ws/terminal.go:34`

The `containerID` from the URL path is accepted blindly. An operator can exec into any
container on the host, including infrastructure containers (the Composer app itself, Postgres,
Valkey), gaining access to encryption keys, database credentials, and the Docker socket.

```go
containerID := r.PathValue("id")
```

**Fix**: Validate that the target container belongs to a managed Compose stack:

```go
containerID := r.PathValue("id")
if containerID == "" {
    http.Error(w, "container ID required", http.StatusBadRequest)
    return
}

// Verify container belongs to a Compose stack
info, err := h.dockerClient.InspectContainer(ctx, containerID)
if err != nil {
    http.Error(w, "container not found", http.StatusNotFound)
    return
}
if info.Config.Labels["com.docker.compose.project"] == "" {
    http.Error(w, "terminal access restricted to Compose stack containers", http.StatusForbidden)
    return
}
```

---

### S8. Password change does not invalidate sessions

**Severity**: High
**File**: `internal/api/handler/user.go:169-191`

When a user changes their password, existing sessions remain valid. If an attacker has a
compromised session, the password change does not revoke their access. Compare with role
changes at lines 152-153 which correctly call `DeleteByUserID`.

```go
func (h *UserHandler) ChangePassword(ctx context.Context, input *dto.ChangePasswordInput) (*struct{}, error) {
    // ... validates and updates password ...
    if err := h.users.Update(ctx, user); err != nil {
        return nil, serverError(err)
    }
    // No session invalidation here
    return nil, nil
}
```

**Fix**: Add session revocation after password change:

```go
if err := h.users.Update(ctx, user); err != nil {
    return nil, serverError(err)
}

// Invalidate all sessions so compromised tokens are revoked
if h.sessions != nil {
    _ = h.sessions.DeleteByUserID(ctx, user.ID)
}

return nil, nil
```

---

### S9. User deletion does not cascade session/API key revocation

**Severity**: High
**File**: `internal/api/handler/user.go:159-167`

Deleting a user only removes the user row. Sessions and API keys remain valid until natural
expiry (up to 7 days for sessions; API keys may never expire).

```go
func (h *UserHandler) Delete(ctx context.Context, input *dto.UserIDInput) (*struct{}, error) {
    if err := authmw.CheckRole(ctx, auth.RoleAdmin); err != nil {
        return nil, err
    }
    if err := h.users.Delete(ctx, input.ID); err != nil {
        return nil, serverError(err)
    }
    return nil, nil
}
```

**Fix**: Cascade revocation before deleting the user:

```go
func (h *UserHandler) Delete(ctx context.Context, input *dto.UserIDInput) (*struct{}, error) {
    if err := authmw.CheckRole(ctx, auth.RoleAdmin); err != nil {
        return nil, err
    }

    // Revoke all sessions and API keys before deleting
    if h.sessions != nil {
        _ = h.sessions.DeleteByUserID(ctx, input.ID)
    }
    // TODO: also revoke API keys via h.keys.DeleteByUserID(ctx, input.ID)

    if err := h.users.Delete(ctx, input.ID); err != nil {
        return nil, serverError(err)
    }
    return nil, nil
}
```

---

### S10. Stack name validation allows `..` path traversal

**Severity**: High
**File**: `internal/domain/stack/aggregate.go:112-120`

`validateName` rejects `/`, `\`, spaces, and other special characters, but does not reject
`..`. The stack path is constructed as `filepath.Join(stacksDir, name)` at
`stack_service.go:91`. A name of `..` resolves to the parent directory.

```go
func validateName(name string) error {
    if name == "" {
        return errors.New("stack name is required")
    }
    if strings.ContainsAny(name, "/ \\:*?\"<>|") {
        return fmt.Errorf("stack name %q contains invalid characters", name)
    }
    return nil
}
```

**Fix**: Enforce a strict allowlist pattern:

```go
import "regexp"

var validNameRe = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]{0,127}$`)

func validateName(name string) error {
    if name == "" {
        return errors.New("stack name is required")
    }
    if name == "." || name == ".." {
        return errors.New("stack name cannot be '.' or '..'")
    }
    if !validNameRe.MatchString(name) {
        return fmt.Errorf("stack name %q must start with alphanumeric and contain only [a-zA-Z0-9._-]", name)
    }
    return nil
}
```

---

### S11. SSHKeyFile path allows arbitrary file reads

**Severity**: High
**File**: `internal/api/dto/stack.go:23`, `internal/infra/git/client.go:30-31`

The `SSHKeyFile` field accepts any filesystem path from the user. It is read by
`crypto.DecryptFile` without path validation. An operator could supply
`ssh_key_file=/etc/shadow` or any other sensitive file.

```go
// dto/stack.go:23
SSHKeyFile string `json:"ssh_key_file,omitempty" doc:"Path to SSH key file on server"`

// git/client.go:30-31
if creds.SSHKeyFile != "" {
    keyContent, err := crypto.DecryptFile(creds.SSHKeyFile)
```

**Fix**: Validate that the path is within an allowed directory:

```go
func validateSSHKeyPath(path, dataDir string) error {
    absPath, err := filepath.Abs(path)
    if err != nil {
        return fmt.Errorf("invalid path: %w", err)
    }

    allowedDirs := []string{
        filepath.Join(dataDir, "ssh"),
        filepath.Join(os.Getenv("HOME"), ".ssh"),
        "/home/composer/.ssh",
    }
    for _, dir := range allowedDirs {
        if strings.HasPrefix(absPath, dir+string(filepath.Separator)) {
            return nil
        }
    }
    return fmt.Errorf("SSH key path must be within %v", allowedDirs)
}
```

---

### S12. ImportFromDir has no path restriction

**Severity**: High
**File**: `internal/api/handler/stack.go:512-517`

Admin-only, but accepts any host directory path. An admin could point this at `/etc/`,
`/var/run/`, or any sensitive directory. The function calls `os.ReadDir` and `os.ReadFile` on
all subdirectories.

```go
func (h *StackHandler) Import(ctx context.Context, input *dto.ImportStacksInput) (*dto.ImportStacksOutput, error) {
    if err := authmw.CheckRole(ctx, auth.RoleAdmin); err != nil {
        return nil, err
    }
    result, err := h.stacks.ImportFromDir(ctx, input.Body.SourceDir)
```

**Fix**: Validate the source directory against a blocklist of sensitive paths and ensure it
actually contains compose files:

```go
func validateImportDir(dir string) error {
    absDir, err := filepath.Abs(dir)
    if err != nil {
        return fmt.Errorf("invalid path: %w", err)
    }
    blocked := []string{"/etc", "/var/run", "/proc", "/sys", "/dev", "/root", "/boot"}
    for _, b := range blocked {
        if strings.HasPrefix(absDir, b) {
            return fmt.Errorf("import from %s is not permitted", b)
        }
    }
    return nil
}
```

---

### S13. Docker compose exec allowlist includes dangerous subcommands

**Severity**: High
**File**: `internal/api/handler/stack.go:604-607`

The compose exec allowlist includes `exec` and `cp`, which give operators shell-equivalent
access inside any stack container and the ability to copy files in/out.

```go
allowed := map[string]bool{
    "ps": true, "logs": true, "top": true, "config": true,
    "images": true, "port": true, "version": true, "ls": true,
    "events": true, "exec": true, "cp": true, "build": true,
}
```

**Fix**: Remove `exec` and `cp` from the operator allowlist (operators already have terminal
access via the WebSocket endpoint). Or require admin for these:

```go
allowed := map[string]bool{
    "ps": true, "logs": true, "top": true, "config": true,
    "images": true, "port": true, "version": true, "ls": true,
    "events": true, "build": true,
}

// exec and cp require admin role
adminOnly := map[string]bool{"exec": true, "cp": true}
if adminOnly[args[0]] {
    if err := authmw.CheckRole(ctx, auth.RoleAdmin); err != nil {
        return nil, err
    }
}
```

---

### S14. Docker global exec allowlist includes `compose`

**Severity**: High
**File**: `internal/api/handler/docker_exec.go:59-64`

The admin-only global docker exec handler allowlists the `compose` subcommand. Since the user
controls all subsequent arguments, they can run `docker compose exec <container> <any command>`
or `docker compose run <image>`.

```go
allowed := map[string]bool{
    "ps": true, "images": true, "network": true, "volume": true,
    "system": true, "info": true, "version": true, "inspect": true,
    "logs": true, "stats": true, "top": true, "port": true,
    "diff": true, "history": true, "search": true, "tag": true,
    "compose": true,
}
```

**Fix**: Remove `compose` from the global exec allowlist (compose operations already have
dedicated endpoints), or add second-level validation for compose subcommands:

```go
if args[0] == "compose" && len(args) > 1 {
    allowedCompose := map[string]bool{
        "ps": true, "logs": true, "config": true, "images": true, "ls": true, "version": true,
    }
    if !allowedCompose[args[1]] {
        return nil, huma.Error422UnprocessableEntity("compose subcommand '" + args[1] + "' is not allowed via global exec")
    }
}
```

---

### S15. Templates API bypasses authentication

**Severity**: High
**File**: `internal/api/middleware/auth.go:137-139`

The prefix match on `/api/v1/templates` means any future endpoint starting with that prefix
(e.g., `/api/v1/templates-admin`) is also unauthenticated. Templates may contain internal
infrastructure details.

```go
if strings.HasPrefix(path, "/api/v1/templates") {
    return true
}
```

**Fix**: Use trailing slash for the prefix, or switch to exact path matching:

```go
if path == "/api/v1/templates" || strings.HasPrefix(path, "/api/v1/templates/") {
    return true
}
```

---

### S16. Internal error messages leak to clients

**Severity**: High
**File**: `internal/api/handler/errors.go:11-16`

`serverError(err)` passes `err.Error()` directly into the 500 response body. Internal error
messages often contain file paths, database connection strings, Docker daemon errors, and stack
traces that aid reconnaissance.

```go
func serverError(err error) error {
    if err == nil {
        return huma.Error500InternalServerError("unknown error")
    }
    return huma.Error500InternalServerError(err.Error())
}
```

**Fix**: Log the full error server-side and return a generic message:

```go
func serverError(err error) error {
    if err == nil {
        return huma.Error500InternalServerError("an internal error occurred")
    }
    // Log the actual error for debugging (caller should have logger in scope)
    // For now, use the generic internalError helper
    return huma.Error500InternalServerError("an internal error occurred")
}
```

Longer term, add a correlation ID to the response and log it with the error for traceability.

---

### S17. Webhook secret encryption errors silently discarded

**Severity**: Medium
**File**: `internal/infra/store/webhook_repo.go:33-34`

The encryption error return is discarded. If encryption fails, `encSecret` is empty string,
causing the webhook to be stored with no secret.

```go
encSecret, _ := crypto.Encrypt(w.Secret)
```

**Fix**:

```go
encSecret, err := crypto.Encrypt(w.Secret)
if err != nil {
    return fmt.Errorf("encrypting webhook secret: %w", err)
}
```

---

### S18. Stack credential encryption falls back to plaintext

**Severity**: Medium
**File**: `internal/infra/store/stack_repo.go:170-173`

If `crypto.Encrypt` returns an error, credentials are stored as plaintext JSON in the database.

```go
encrypted, err := crypto.Encrypt(string(b))
if err != nil {
    s := string(b) // fallback to unencrypted
    return &s
}
```

**Fix**:

```go
encrypted, err := crypto.Encrypt(string(b))
if err != nil {
    return nil // or return error to caller to handle
}
return &encrypted
```

---

### S19. CSP allows unsafe-inline for scripts

**Severity**: Medium
**File**: `internal/api/middleware/security.go:18`

`'unsafe-inline'` in `script-src` largely nullifies XSS protection from CSP. The `unpkg.com`
CDN is also a broad trust anchor.

```go
w.Header().Set("Content-Security-Policy",
    "default-src 'self'; script-src 'self' 'unsafe-inline' https://unpkg.com; ...")
```

**Fix**: Use nonces for inline scripts. For the `/docs` page that loads Stoplight from unpkg,
use a separate CSP via a `<meta>` tag or a route-specific middleware. Pin unpkg URLs to
specific package versions with SRI hashes.

---

### S20. Login rate limiter keyed on email only

**Severity**: Medium
**File**: `internal/api/handler/auth.go:88`

An attacker can lock out any user by sending failed login attempts for their email address
(account lockout DoS). The global rate limiter (60 req/sec per IP) is too generous for
authentication.

```go
if !h.loginLimiter.Allow(input.Body.Email) {
```

**Fix**: Rate limit on a composite key of IP + email:

```go
ip := r.RemoteAddr
if !h.loginLimiter.Allow(ip) {
    return nil, huma.Error429TooManyRequests("too many login attempts from this IP")
}
if !h.emailLimiter.Allow(input.Body.Email) {
    return nil, huma.Error429TooManyRequests("too many login attempts for this account")
}
```

---

### S21. Compose files written world-readable

**Severity**: Medium
**File**: `internal/app/stack_service.go:103,225`

Compose files may contain environment variables with secrets. Writing them as 0644 makes them
readable by any user on the host.

```go
os.WriteFile(path, []byte(content), 0644)
```

**Fix**: Use restrictive permissions:

```go
os.WriteFile(path, []byte(content), 0600)
```

---

### S22. Data directory created with 0755 permissions

**Severity**: Medium
**File**: `internal/infra/crypto/encrypt.go:59`

The encryption key file directory is created world-readable. While the key file itself is 0600,
any user on the host can list the directory and see the `encryption.key` filename. Same issue
at `internal/infra/sops/agekey.go:99`.

```go
os.MkdirAll(dataDir, 0755)
```

**Fix**:

```go
os.MkdirAll(dataDir, 0700)
```

---

### S23. SOPS decryption writes plaintext to disk with no crash-safe cleanup

**Severity**: Medium
**File**: `internal/infra/sops/sops.go:158-185`

During deploy, SOPS-encrypted `.env` files are decrypted and written to disk as plaintext.
`ReEncryptEnvFile` restores the encrypted version after deploy, but if the process crashes
during deploy, the plaintext remains permanently.

**Fix**: Use `defer` to ensure re-encryption runs even on panic:

```go
func DeployWithSOPS(stackDir, ageKey string, deployFn func() error) error {
    decrypted, err := DecryptEnvFile(stackDir, ageKey)
    if err != nil {
        return err
    }
    if decrypted {
        defer ReEncryptEnvFile(stackDir, ageKey) // always re-encrypt
    }
    return deployFn()
}
```

---

### S24. API key deletion cannot invalidate cache

**Severity**: Medium
**File**: `internal/app/auth_service.go:177-181`

The hashed key needed for cache eviction is not available at deletion time. A revoked API key
remains usable until TTL expiry.

```go
func (s *AuthService) DeleteAPIKey(ctx context.Context, id string) error {
    // Note: we don't have the hashed key here
    return s.keys.Delete(ctx, id)
}
```

**Fix**: Store the hashed key in the API key record and pass it to cache:

```go
func (s *AuthService) DeleteAPIKey(ctx context.Context, id string) error {
    // Fetch key to get the hash for cache invalidation
    key, err := s.keys.GetByID(ctx, id)
    if err == nil && key != nil && s.cache != nil {
        _ = s.cache.DeleteAPIKey(ctx, key.HashedKey)
    }
    return s.keys.Delete(ctx, id)
}
```

---

### S25. WebSocket origin validated against spoofable Host header

**Severity**: Medium
**File**: `internal/api/ws/terminal.go:46-47`

The WebSocket origin is validated against `r.Host`, which can be spoofed if there's no reverse
proxy enforcing Host validation.

```go
conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
    OriginPatterns: []string{r.Host},
})
```

**Fix**: Use a server configuration variable:

```go
allowedOrigins := os.Getenv("COMPOSER_ALLOWED_ORIGINS")
if allowedOrigins == "" {
    allowedOrigins = r.Host // fallback
}
conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
    OriginPatterns: strings.Split(allowedOrigins, ","),
})
```

---

### S26. No WebSocket read size limit

**Severity**: Medium
**File**: `internal/api/ws/terminal.go:100`

The `coder/websocket` library defaults to unlimited read size. A malicious client can send
arbitrarily large messages to consume server memory.

```go
msgType, data, err := conn.Read(ctx)
```

**Fix**: Set a read limit after accepting the connection:

```go
conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{...})
if err != nil { return }
defer conn.CloseNow()
conn.SetReadLimit(64 * 1024) // 64KB max message size
```

---

### S27. No per-user connection limit for WebSocket/SSE

**Severity**: Medium
**Files**: `internal/api/ws/terminal.go`, `internal/api/handler/sse.go`

No mechanism limits concurrent WebSocket or SSE connections per user. An attacker with
valid credentials could open hundreds of connections, each creating a Docker exec session or
event bus subscription, exhausting server resources.

**Fix**: Add a connection tracker:

```go
type TerminalHandler struct {
    dockerClient *docker.Client
    connections  sync.Map // userID -> *int32 (atomic counter)
    maxPerUser   int32
}

func (h *TerminalHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
    userID := authmw.UserIDFromContext(r.Context())
    counter := h.getOrCreateCounter(userID)
    if atomic.AddInt32(counter, 1) > h.maxPerUser {
        atomic.AddInt32(counter, -1)
        http.Error(w, "too many terminal sessions", http.StatusTooManyRequests)
        return
    }
    defer atomic.AddInt32(counter, -1)
    // ... rest of handler
}
```

---

### S28. Audit log goroutine reads cancelled request context

**Severity**: Medium
**File**: `internal/api/middleware/audit.go:34-57`

The audit middleware fires a goroutine that reads `r.Context()` values after the handler
returns. While Go context values remain accessible after cancellation, this is fragile.

**Fix**: Capture values before entering the goroutine:

```go
// Capture before goroutine
userID := UserIDFromContext(r.Context())
action := deriveAction(r.Method, r.URL.Path)
method := r.Method
path := r.URL.Path
ip := r.RemoteAddr

go func() {
    auditCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
    defer cancel()
    repo.Log(auditCtx, store.AuditEntry{
        UserID: userID,
        Action: action,
        // ...use captured values, not r.Context()
    })
}()
```

---

### S29. HSTS header trusts unsanitized X-Forwarded-Proto

**Severity**: Low
**File**: `internal/api/middleware/security.go:21-23`

The HSTS header is set based on `X-Forwarded-Proto`, which clients can spoof when
`COMPOSER_TRUSTED_PROXIES` is not set (unlike the `RealIP` middleware which is gated).

```go
if r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https" {
    w.Header().Set("Strict-Transport-Security", "max-age=63072000; includeSubDomains")
}
```

**Fix**: Only trust the header when behind a trusted proxy:

```go
isHTTPS := r.TLS != nil
if os.Getenv("COMPOSER_TRUSTED_PROXIES") != "" {
    isHTTPS = isHTTPS || r.Header.Get("X-Forwarded-Proto") == "https"
}
if isHTTPS {
    w.Header().Set("Strict-Transport-Security", "max-age=63072000; includeSubDomains; preload")
}
```

---

### S30. OAuth error leaks internal details

**Severity**: Low
**File**: `internal/api/handler/oauth.go:104`

The raw error from the OAuth library is returned to the user.

```go
http.Error(w, "OAuth failed: "+err.Error(), http.StatusUnauthorized)
```

**Fix**:

```go
log.Error("OAuth authentication failed", zap.Error(err))
http.Error(w, "OAuth authentication failed", http.StatusUnauthorized)
```

---

### S31. X-XSS-Protection header is obsolete

**Severity**: Low
**File**: `internal/api/middleware/security.go:14`

Setting `X-XSS-Protection: 1; mode=block` can introduce vulnerabilities in older IE versions.
Modern guidance is to remove it or set it to `0`.

```go
w.Header().Set("X-XSS-Protection", "1; mode=block")
```

**Fix**:

```go
w.Header().Set("X-XSS-Protection", "0")
```

---

### S32. generateID() fallback uses deterministic data

**Severity**: Low
**File**: `internal/domain/auth/user.go:130-131`

If `crypto/rand.Read` fails, the fallback uses XOR of `time.Now().UnixNano()` with a constant,
producing predictable IDs.

**Fix**: Panic on `crypto/rand.Read` failure rather than falling back to weak randomness:

```go
if _, err := rand.Read(buf[8:]); err != nil {
    panic("crypto/rand.Read failed: " + err.Error())
}
```

---

## UX

### U1. No mobile navigation

**Severity**: Critical
**File**: `web/src/layouts/Layout.astro:35`

The sidebar is `hidden md:flex`. Below 768px, there is no hamburger menu, bottom navigation,
or any alternative. The app is completely unusable on mobile.

```html
<aside class="hidden md:flex w-60 flex-col border-r border-border bg-cp-950">
```

**Fix**: Add a mobile hamburger menu toggle:

```html
<!-- Mobile menu button (visible below md) -->
<button id="mobile-menu-toggle" class="md:hidden p-2" aria-label="Open navigation menu">
  <svg>...</svg>
</button>

<!-- Sidebar: shown on md+ or when toggled on mobile -->
<aside id="sidebar" class="hidden md:flex w-60 flex-col ...">
```

Add a script to toggle the `hidden` class on click, with an overlay backdrop.

---

### U2. Container actions silently swallow errors

**Severity**: Critical
**File**: `web/src/components/container/ContainerListPage.tsx:100-117`

Start, Restart, and Stop buttons call `apiFetch` but ignore the error return. Users get zero
feedback when operations fail.

```tsx
// Line 101 -- error return is ignored
await apiFetch(`/api/v1/containers/${c.id}/start`, { method: "POST" });
setTimeout(fetchContainers, 1000);
```

**Fix**: Capture and display errors:

```tsx
const { error } = await apiFetch(`/api/v1/containers/${c.id}/start`, { method: "POST" });
if (error) {
  setError(`Failed to start ${c.name}: ${error}`);
} else {
  setTimeout(fetchContainers, 1000);
}
```

---

### U3. Pipeline run/delete errors silently discarded

**Severity**: Critical
**File**: `web/src/components/pipeline/PipelinePage.tsx:67-73,108`

`handleRun` and `handleDelete` call `apiFetch` but ignore the `{ error }` return.

**Fix**: Check and display errors for both operations.

---

### U4. Template create/fetch errors silently discarded

**Severity**: High
**File**: `web/src/components/stack/TemplatePicker.tsx:26-28,34-36`

Template list fetch and template compose content fetch both ignore errors.

**Fix**: Add `error` state and render it when the template list or content fails to load.

---

### U5. GitStatus fetch errors silently dropped

**Severity**: High
**File**: `web/src/components/stack/GitStatus.tsx:41-43,47-49`

Both `fetchStatus` and `fetchLog` ignore the `error` return. The component renders nothing
with no indication of why.

**Fix**: Set error state on failure and display it.

---

### U6. No React Error Boundary

**Severity**: High
**File**: All pages

No component wraps children with an error boundary. A runtime crash in any component
(e.g., `.map()` on undefined from bad API data) will white-screen the entire page.

**Fix**: Create a top-level `ErrorBoundary` component:

```tsx
import { Component, type ReactNode } from "react";

interface Props { children: ReactNode; }
interface State { hasError: boolean; error?: Error; }

export class ErrorBoundary extends Component<Props, State> {
  state: State = { hasError: false };

  static getDerivedStateFromError(error: Error) {
    return { hasError: true, error };
  }

  render() {
    if (this.state.hasError) {
      return (
        <div className="p-6 text-center">
          <h2 className="text-lg font-semibold text-cp-red">Something went wrong</h2>
          <p className="text-sm text-muted-foreground mt-2">{this.state.error?.message}</p>
          <button onClick={() => window.location.reload()} className="mt-4 underline">
            Reload page
          </button>
        </div>
      );
    }
    return this.props.children;
  }
}
```

Wrap the `<slot />` in `Layout.astro` with it.

---

### U7. Validate button has unconditional alert() bug

**Severity**: High
**File**: `web/src/components/stack/StackDetail.tsx:304`

Due to missing braces, `alert()` fires unconditionally regardless of error/success. The
semicolon after `setActionError("")` terminates the `else` branch.

```tsx
if (error) setActionError(`Validation failed: ${error}`);
else setActionError(""); alert(data?.stderr || data?.stdout || "Valid");
//                      ^ terminates else -- alert runs always
```

**Fix**: Add braces:

```tsx
if (error) {
  setActionError(`Validation failed: ${error}`);
} else {
  setActionError("");
  alert(data?.stderr || data?.stdout || "Valid");
}
```

Better yet, replace `alert()` with an inline status message (see U17).

---

### U8. ComposeEditor destroys and recreates on every content change

**Severity**: High
**File**: `web/src/components/stack/ComposeEditor.tsx:177-220`

The `useEffect` depends on `[content, readOnly]`. After save + fetch, the editor is destroyed
and recreated, losing cursor position, scroll position, undo history, and selection.

```tsx
useEffect(() => {
    if (!editorRef.current) return;
    const state = EditorState.create({ doc: content, extensions: [...] });
    const view = new EditorView({ state, parent: editorRef.current });
    viewRef.current = view;
    return () => { view.destroy(); };
}, [content, readOnly]);  // content change triggers full recreation
```

**Fix**: Only recreate when `stackName` or `readOnly` changes. For content updates, dispatch a
transaction:

```tsx
// Effect 1: Create editor once per stack/readOnly
useEffect(() => {
    // ... create EditorView ...
    return () => { view.destroy(); };
}, [stackName, readOnly]);

// Effect 2: Sync external content changes without destroying editor
useEffect(() => {
    const view = viewRef.current;
    if (!view) return;
    const currentDoc = view.state.doc.toString();
    if (content !== currentDoc) {
        view.dispatch({
            changes: { from: 0, to: currentDoc.length, insert: content },
        });
        setDirty(false);
    }
}, [content]);
```

---

### U9. No password confirmation on bootstrap

**Severity**: High
**File**: `web/src/components/layout/LoginPage.tsx:191-197`

The first admin account creation form has `minLength={8}` but no confirmation field. A typo
locks the user out with no recovery path.

```tsx
<Input
  id="password" type="password" value={password}
  onChange={(e) => setPassword(e.target.value)}
  placeholder="Choose a strong password"
  required minLength={8}
/>
```

**Fix**: Add a confirm password field in bootstrap mode:

```tsx
{isBootstrap && (
  <div className="space-y-2">
    <label htmlFor="confirm-password" className="text-xs uppercase tracking-wider text-muted-foreground">
      Confirm Password
    </label>
    <Input
      id="confirm-password" type="password" value={confirmPassword}
      onChange={(e) => setConfirmPassword(e.target.value)}
      placeholder="Repeat password" required minLength={8}
    />
    {password !== confirmPassword && confirmPassword && (
      <p className="text-xs text-cp-red">Passwords do not match</p>
    )}
  </div>
)}
```

Disable submit when passwords don't match.

---

### U10. Tab navigation has no ARIA roles

**Severity**: High
**File**: `web/src/components/stack/StackDetail.tsx:214-228`

Tabs use plain `<button>` elements with no `role="tablist"`, `role="tab"`, `aria-selected`, or
`aria-controls`. Screen readers cannot identify this as tab navigation.

```tsx
<div className="flex gap-1 border-b border-border">
  {tabs.map((tab) => (
    <button key={tab} onClick={() => setActiveTab(tab)} className={...}>
      {tab}
    </button>
  ))}
</div>
```

**Fix**:

```tsx
<div role="tablist" className="flex gap-1 border-b border-border">
  {tabs.map((tab) => (
    <button
      key={tab}
      role="tab"
      aria-selected={activeTab === tab}
      aria-controls={`panel-${tab}`}
      onClick={() => setActiveTab(tab)}
      className={...}
    >
      {tab}
    </button>
  ))}
</div>

{/* Tab panels */}
{activeTab === "containers" && (
  <div role="tabpanel" id="panel-containers" aria-labelledby="tab-containers">
    ...
  </div>
)}
```

---

### U11. Command palette lacks dialog role and focus trap

**Severity**: High
**File**: `web/src/components/layout/CommandPalette.tsx:63-108`

The modal overlay has no `role="dialog"`, `aria-modal="true"`, or `aria-label`. Focus is not
trapped inside the dialog.

```tsx
<div className="fixed inset-0 z-50 flex items-start justify-center pt-[20vh]">
  <div className="fixed inset-0 bg-black/50" onClick={() => setOpen(false)} />
  <div className="relative w-full max-w-lg rounded-xl border ...">
```

**Fix**:

```tsx
<div
  className="fixed inset-0 z-50 flex items-start justify-center pt-[20vh]"
  role="dialog"
  aria-modal="true"
  aria-label="Command palette"
>
```

Add focus trap (e.g., via `focus-trap-react` or manual `keydown` handler for Tab).

---

### U12. Jobs drawer lacks dialog role and focus trap

**Severity**: High
**File**: `web/src/components/layout/JobsDrawer.tsx:173`

Same issue as CommandPalette. The drawer overlay has no dialog semantics.

**Fix**: Add `role="dialog"`, `aria-modal="true"`, `aria-label="Background jobs"`, and
implement focus trap.

---

### U13. Form labels not associated with inputs

**Severity**: High
**Files**:
- `web/src/components/stack/RawComposeForm.tsx:44`
- `web/src/components/stack/GitCloneForm.tsx:65,69,76,80,84,102,109`
- `web/src/components/layout/WebhookSettings.tsx:108,119,134`
- `web/src/components/stack/TemplatePicker.tsx:48`

Labels lack `htmlFor` and inputs lack `id`, so they are not programmatically associated.
(Note: `LoginPage.tsx` correctly uses `htmlFor`/`id` pairs.)

```tsx
<label className="...">Stack Name</label>
<Input value={name} onChange={...} />
```

**Fix**:

```tsx
<label htmlFor="stack-name" className="...">Stack Name</label>
<Input id="stack-name" value={name} onChange={...} />
```

Apply to every label/input pair in the affected files.

---

### U14. Select elements have no accessible labels

**Severity**: High
**Files**:
- `web/src/components/stack/GitCloneForm.tsx:85-96`
- `web/src/components/layout/ApiKeyManagement.tsx:84-92`
- `web/src/components/layout/UserManagement.tsx:93-101`
- `web/src/components/layout/WebhookSettings.tsx:120-131`

Multiple `<select>` elements lack `aria-label` or associated `<label>`.

**Fix**: Add `aria-label` or associate with a label:

```tsx
<select aria-label="Authentication method" value={authMethod} onChange={...}>
```

---

### U15. Stack action buttons overflow on small screens

**Severity**: High
**File**: `web/src/components/stack/StackDetail.tsx:172-191`

Six action buttons in a non-wrapping `flex gap-2`.

**Fix**: Add `flex-wrap`:

```tsx
<div className="flex flex-wrap gap-2" data-testid="stack-actions">
```

Or collapse into a dropdown menu on small screens.

---

### U16. Grid forms don't stack on mobile

**Severity**: High
**Files**:
- `web/src/components/stack/GitCloneForm.tsx:63,74`
- `web/src/components/layout/WebhookSettings.tsx:106`
- `web/src/components/layout/UserManagement.tsx:81`
- `web/src/components/layout/ApiKeyManagement.tsx:78`

Fixed column grids with no responsive breakpoint.

```tsx
<div className="grid grid-cols-2 gap-4">
```

**Fix**:

```tsx
<div className="grid grid-cols-1 md:grid-cols-2 gap-4">
```

```tsx
<div className="grid grid-cols-1 md:grid-cols-3 gap-4">
```

---

### U17. window.alert() and window.prompt() used for UX

**Severity**: Medium
**Files**:
- `web/src/components/stack/StackDetail.tsx:304` (alert for validate)
- `web/src/components/docker/ImagesPage.tsx:56` (alert for prune)
- `web/src/components/docker/VolumesPage.tsx:54` (alert for prune)
- `web/src/components/stack/StackDetail.tsx:158` (prompt for Git URL)
- `web/src/components/layout/UserManagement.tsx:148` (prompt for password reset)

`alert()` blocks the UI thread. `prompt()` provides no validation or masking for passwords.

**Fix**: Replace with inline status messages or modal dialog components using
the existing shadcn/ui `Dialog` component.

---

### U18. setTimeout(fetchStack, 1000) blind refresh pattern

**Severity**: Medium
**Files**:
- `web/src/components/stack/StackDetail.tsx:101`
- `web/src/components/container/ContainerListPage.tsx:102-103,112,116`

A hardcoded 1-second delay is used after actions to refresh state. For fast operations the UI
is stale for a second; for slow operations the UI shows stale data.

**Fix**: Use the SSE event stream to trigger refetch, or poll until the expected state is
observed:

```tsx
function waitForState(expected: string, maxAttempts = 10) {
  let attempts = 0;
  const poll = async () => {
    const { data } = await apiFetch(`/api/v1/stacks/${stackName}`);
    if (data?.status === expected || ++attempts >= maxAttempts) {
      setStack(data);
      return;
    }
    setTimeout(poll, 1000 * Math.min(attempts, 3));
  };
  poll();
}
```

---

### U19. No unsaved-changes warning in editors

**Severity**: Medium
**Files**:
- `web/src/components/stack/ComposeEditor.tsx`
- `web/src/components/stack/EnvEditor.tsx`

If a user has unsaved changes and navigates away, work is lost silently.

**Fix**: Add a `beforeunload` listener when `dirty` is true:

```tsx
useEffect(() => {
  if (!dirty) return;
  const handler = (e: BeforeUnloadEvent) => {
    e.preventDefault();
    e.returnValue = "";
  };
  window.addEventListener("beforeunload", handler);
  return () => window.removeEventListener("beforeunload", handler);
}, [dirty]);
```

---

### U20. dangerouslySetInnerHTML without HTML escaping audit

**Severity**: Medium
**Files**:
- `web/src/components/stack/StackDetail.tsx:331` (Dockerfile)
- `web/src/components/container/LogViewer.tsx:135`
- `web/src/components/docker/VolumesPage.tsx:75`
- `web/src/components/docker/NetworksPage.tsx:78`

While these use custom highlighters, the pattern is fragile. If any API response contains
`<script>` or HTML entities, it could be rendered.

**Fix**: Audit `highlightDockerfile`, `highlightLog`, and `highlightJSON` to ensure they call
an HTML-escape function before inserting markup:

```ts
function escapeHtml(str: string): string {
  return str.replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;");
}
```

---

### U21. No auto-refresh on Dashboard

**Severity**: Medium
**File**: `web/src/components/stack/DashboardOverview.tsx`

The dashboard loads once and never refreshes. The EventStream component streams events but
doesn't update dashboard state.

**Fix**: Add periodic polling (e.g., every 30s) or connect the EventStream events to trigger
dashboard re-fetches.

---

### U22. Keyboard shortcut shows macOS-only symbol

**Severity**: Medium
**File**: `web/src/components/layout/CommandPalette.tsx:57`

Shows `⌘K` but the listener handles both `metaKey` (Mac) and `ctrlKey` (Windows/Linux).

```tsx
<kbd className="...">⌘K</kbd>
```

**Fix**:

```tsx
const isMac = typeof navigator !== "undefined" && /Mac|iPod|iPhone|iPad/.test(navigator.userAgent);
// ...
<kbd className="...">{isMac ? "⌘K" : "Ctrl+K"}</kbd>
```

---

### U23. No client-side stack name validation

**Severity**: Medium
**Files**:
- `web/src/components/stack/RawComposeForm.tsx:45`
- `web/src/components/stack/GitCloneForm.tsx:66`
- `web/src/components/stack/TemplatePicker.tsx:48`

Stack names are only validated by the `required` HTML attribute. Invalid names cause a server
round-trip and a generic error.

**Fix**:

```tsx
<Input
  pattern="[a-zA-Z0-9][a-zA-Z0-9._-]*"
  title="Letters, numbers, hyphens, underscores, dots. Must start with alphanumeric."
  value={name} onChange={...} required
/>
```

---

### U24. GitCloneForm doesn't validate repository URL

**Severity**: Medium
**File**: `web/src/components/stack/GitCloneForm.tsx:70`

Accepts any text. A value like `not-a-url` will cause a server error.

**Fix**: Add `type="url"` for HTTPS repos, or pattern validation:

```tsx
<Input
  type="url"
  pattern="(https?://|git@).+"
  title="Repository URL (https:// or git@)"
  value={repoUrl} onChange={...} required
/>
```

---

### U25. Terminal resize not debounced

**Severity**: Medium
**File**: `web/src/components/terminal/Terminal.tsx:131`

`fit()` fires on every resize event, causing jank and sending many resize messages.

**Fix**: Debounce:

```tsx
const handleResize = useMemo(
  () => debounce(() => fitRef.current?.fit(), 150),
  []
);
```

---

### U26. Console command history not persisted

**Severity**: Low
**Files**:
- `web/src/components/stack/DockerConsole.tsx:16-17`
- `web/src/components/stack/StackConsole.tsx:21-22`

History is lost on component unmount (tab switch, navigation).

**Fix**: Persist in `localStorage`:

```tsx
const storageKey = `console-history-${stackName}`;
const [history, setHistory] = useState<string[]>(() => {
  try { return JSON.parse(localStorage.getItem(storageKey) || "[]"); }
  catch { return []; }
});
useEffect(() => { localStorage.setItem(storageKey, JSON.stringify(history)); }, [history]);
```

---

### U27. Native select dropdowns unreadable on dark theme

**Severity**: Low
**Files**: Multiple `<select>` elements with `bg-transparent`

On dark themes, native `<select>` dropdown options render with the OS default light background,
making text unreadable.

**Fix**: Use the shadcn/ui `Select` component (Radix-based) which respects the theme, or
add explicit option colors:

```css
select option {
  background-color: var(--color-cp-800);
  color: var(--color-foreground);
}
```

---

## Performance

### P1. Per-container SSE connection exhausts browser limits

**Severity**: Critical
**File**: `web/src/components/container/InlineStats.tsx:17`

Each running container opens its own `EventSource` for stats. With 20 containers, that's 20
SSE connections, exceeding the browser's 6-connection limit for HTTP/1.1. Excess connections
queue, stalling all other network activity including API calls and page navigation.

```tsx
const es = new EventSource(`/api/v1/sse/containers/${containerId}/stats`, { withCredentials: true });
```

**Fix**: Replace per-container SSE with a batch approach:

Option A -- Single SSE endpoint that multiplexes all container stats:
```
GET /api/v1/sse/containers/stats  (server sends events tagged with container ID)
```

Option B -- REST polling from the parent `ContainerListPage`:
```tsx
// In ContainerListPage, poll a batch stats endpoint every 3s
const { data } = await apiFetch("/api/v1/containers/stats");
// Pass stats to InlineStats via props
<InlineStats stats={statsMap[c.id]} />
```

---

### P2. Valkey cache exists but is never used for auth

**Severity**: Critical
**File**: `internal/api/middleware/auth.go:47-55`, `internal/app/auth_service.go:119-132`

The auth middleware calls `ValidateSession()` which goes directly to the database on every
request. The Valkey cache (`internal/infra/cache/valkey.go`) has fully implemented
`GetSession`/`SetSession`/`GetAPIKey`/`SetAPIKey` methods that are never called from the
validation path. Every authenticated request hits the database.

```go
// auth_service.go:119-132 -- goes straight to DB
func (s *AuthService) ValidateSession(ctx context.Context, sessionID string) (*auth.Session, error) {
    session, err := s.sessions.GetByID(ctx, sessionID)
    // ...
}
```

**Fix**: Add cache-aside lookup. The cache code is already written; it just needs plumbing:

```go
func (s *AuthService) ValidateSession(ctx context.Context, sessionID string) (*auth.Session, error) {
    // Check cache first
    if s.valkey != nil {
        if cached, err := s.valkey.GetSession(ctx, sessionID); err == nil && cached != nil {
            if cached.IsExpired() {
                _ = s.valkey.DeleteSession(ctx, sessionID)
                return nil, nil
            }
            return cached, nil
        }
    }

    // Cache miss -- query DB
    session, err := s.sessions.GetByID(ctx, sessionID)
    if err != nil {
        return nil, fmt.Errorf("looking up session: %w", err)
    }
    if session == nil {
        return nil, nil
    }
    if session.IsExpired() {
        _ = s.sessions.DeleteByID(ctx, sessionID)
        return nil, nil
    }

    // Populate cache
    if s.valkey != nil {
        _ = s.valkey.SetSession(ctx, session)
    }

    return session, nil
}
```

Do the same for `ValidateAPIKey`.

---

### P3. N+1 Docker API calls in stack listing

**Severity**: High
**File**: `internal/app/stack_service.go:194-198`

`List()` calls `docker.ListContainers(ctx, st.Name)` per stack in a sequential loop. With 50
stacks, that's 50 Docker API roundtrips.

```go
for _, st := range stacks {
    containers, err := s.docker.ListContainers(ctx, st.Name)
    if err == nil {
        st.Status = deriveStackStatus(containers)
    }
}
```

**Fix**: Fetch all containers once and group by Compose project label:

```go
func (s *StackService) List(ctx context.Context) ([]*stack.Stack, error) {
    stacks, err := s.stacks.List(ctx)
    if err != nil {
        return nil, err
    }

    // Single Docker API call for all containers
    allContainers, err := s.docker.ListContainers(ctx, "")
    if err == nil {
        byProject := make(map[string][]container.Container)
        for _, c := range allContainers {
            project := c.Labels["com.docker.compose.project"]
            if project != "" {
                byProject[project] = append(byProject[project], c)
            }
        }
        for _, st := range stacks {
            if containers, ok := byProject[st.Name]; ok {
                st.Status = deriveStackStatus(containers)
            }
        }
    }

    return stacks, nil
}
```

---

### P4. No database connection pool configuration

**Severity**: High
**File**: `internal/infra/store/db.go:56-60`

After `sql.Open()`, no pool parameters are set. Go defaults: unlimited open connections (can
exhaust PostgreSQL), only 2 idle connections (cold-connects on every burst).

```go
sqlDB, err := sql.Open(driverName, dsn)
if err != nil {
    return nil, fmt.Errorf("opening database: %w", err)
}
if err := sqlDB.PingContext(ctx); err != nil { ... }
```

**Fix**: Add after `sql.Open()`:

```go
sqlDB.SetMaxOpenConns(25)
sqlDB.SetMaxIdleConns(10)
sqlDB.SetConnMaxLifetime(5 * time.Minute)
sqlDB.SetConnMaxIdleTime(2 * time.Minute)
```

---

### P5. SSE onerror kills connections permanently

**Severity**: High
**Files**:
- `web/src/components/container/EventStream.tsx:67-69`
- `web/src/components/container/LogViewer.tsx:63-65`
- `web/src/components/container/ContainerStats.tsx:44-46`
- `web/src/components/container/InlineStats.tsx:28`

All SSE handlers call `es.close()` in `onerror`, defeating the browser's built-in
EventSource reconnection. Any transient error permanently kills all real-time streams.

```tsx
es.onerror = () => {
  setConnected(false);
  es.close();  // kills auto-reconnect
};
```

**Fix**: Remove `es.close()` to let the browser auto-reconnect:

```tsx
es.onerror = () => {
  setConnected(false);
  // Browser will auto-reconnect. EventSource handles this natively.
};
```

Or implement manual reconnect with exponential backoff if more control is needed.

---

### P6. LogViewer renders all lines without virtualization

**Severity**: High
**File**: `web/src/components/container/LogViewer.tsx:126-139`

Up to 1000 DOM nodes are rendered, each running `highlightLog()` which applies regex
replacements per line. Combined with SSE pushing new lines and `[...prev, newLine]` array
spreads on every event, this causes GC pressure and jank.

**Fix**:

1. Virtualize the log list (e.g., `@tanstack/react-virtual`)
2. Memoize highlighted output per line:
   ```tsx
   const highlightedLines = useMemo(
     () => lines.map(l => ({ ...l, html: highlightLog(l.message) })),
     [lines]
   );
   ```
3. Batch SSE events with `requestAnimationFrame` (see P21)

---

### P7. Font imports ship all subsets

**Severity**: High
**File**: `web/src/styles/globals.css:2-7`

`@fontsource/space-grotesk/400.css` etc. include all subsets (latin, latin-ext, cyrillic,
greek, vietnamese) by default. Only latin is needed.

```css
@import "@fontsource/space-grotesk/400.css";
@import "@fontsource/space-grotesk/500.css";
@import "@fontsource/space-grotesk/600.css";
@import "@fontsource/space-grotesk/700.css";
@import "@fontsource/jetbrains-mono/400.css";
@import "@fontsource/jetbrains-mono/700.css";
```

**Fix**: Import only the latin subset:

```css
@import "@fontsource/space-grotesk/latin-400.css";
@import "@fontsource/space-grotesk/latin-500.css";
@import "@fontsource/space-grotesk/latin-600.css";
@import "@fontsource/space-grotesk/latin-700.css";
@import "@fontsource/jetbrains-mono/latin-400.css";
@import "@fontsource/jetbrains-mono/latin-700.css";
```

Or switch to `@fontsource-variable/space-grotesk` for a single variable font file.

---

### P8. No font-display swap causes invisible text

**Severity**: High
**File**: `web/src/layouts/Layout.astro:26-31`, `web/src/styles/globals.css`

Fonts loaded via `@fontsource` default to `font-display: block`, causing Flash of Invisible
Text (FOIT). No `<link rel="preload">` hints exist for critical fonts.

**Fix**: Add `font-display: swap` and preload critical woff2 files:

```html
<link rel="preload" href="/_astro/space-grotesk-latin-400-normal.woff2"
      as="font" type="font/woff2" crossorigin />
```

---

### P9. No Vite manualChunks -- React invalidated every deploy

**Severity**: Medium
**File**: `web/astro.config.mjs`

The React runtime is bundled into a ~179KB chunk that invalidates on every deploy because the
content hash changes when any component changes.

**Fix**: Add manual chunks to separate vendor code:

```js
// astro.config.mjs
export default defineConfig({
  vite: {
    build: {
      rollupOptions: {
        output: {
          manualChunks: {
            react: ['react', 'react-dom'],
          }
        }
      }
    }
  }
});
```

---

### P10. codemirror meta-package included alongside individual packages

**Severity**: Medium
**File**: `web/package.json:30`

The `codemirror` meta-package re-exports modules that are already imported individually via
`@codemirror/*`. The ComposeEditor chunk is 362KB. Also, `@codemirror/lint` (line 19)
appears unused.

**Fix**: Remove `"codemirror"` and `"@codemirror/lint"` from `package.json`. All needed
`@codemirror/*` packages are already listed individually.

---

### P11. No request deduplication or caching in apiFetch

**Severity**: Medium
**File**: `web/src/lib/api/errors.ts:74-98`

`apiFetch` is a thin wrapper around `fetch` with zero caching or deduplication. Multiple
components on the same page can call the same endpoint simultaneously.

**Fix**: Adopt TanStack Query or implement a minimal dedup layer:

```tsx
const inflight = new Map<string, Promise<any>>();

export async function apiFetch<T>(url: string, opts?: RequestInit) {
  const key = `${opts?.method || "GET"}:${url}`;
  if (!opts?.body && inflight.has(key)) {
    return inflight.get(key);
  }
  const promise = doFetch<T>(url, opts).finally(() => inflight.delete(key));
  if (!opts?.body) inflight.set(key, promise);
  return promise;
}
```

---

### P12. resolveComposeFile re-queries DB redundantly

**Severity**: Medium
**File**: `internal/app/stack_service.go:621-645`

Called from `Deploy`, `Stop`, `Restart`, `Pull`, `Delete` etc., this method queries
`gitCfgs.GetByStackName` then `stacks.GetByName` -- even though callers already fetched
the stack. Two extra DB roundtrips on every stack operation.

**Fix**: Change the signature to accept the already-fetched stack data:

```go
func (s *StackService) resolveComposeFile(ctx context.Context, st *stack.Stack, gitCfg *stack.GitSource) (string, error) {
    // Use passed-in data instead of re-querying
}
```

---

### P13. No TTL/retention for audit_log and webhook_deliveries

**Severity**: Medium
**File**: `internal/infra/store/migrations/001_initial.sql:138-146,81-95`

Both tables grow unboundedly. The audit log is written on every mutating request.

**Fix**: Add a periodic cleanup job (similar to `SessionRepo.DeleteExpired`):

```go
func (r *AuditRepo) DeleteOlderThan(ctx context.Context, retention time.Duration) (int, error) {
    cutoff := time.Now().Add(-retention)
    result, err := r.db.ExecContext(ctx,
        "DELETE FROM audit_log WHERE created_at < $1", cutoff)
    if err != nil { return 0, err }
    n, _ := result.RowsAffected()
    return int(n), nil
}
```

Wire it into the startup cleanup loop with a configurable retention (default: 90 days).

---

### P14. Missing database indexes

**Severity**: Medium
**File**: `internal/infra/store/migrations/001_initial.sql`

Missing indexes that will cause performance issues at scale:

**Fix**: Add a new migration:

```sql
-- 003_add_indexes.sql
CREATE INDEX IF NOT EXISTS idx_audit_user ON audit_log(user_id);
CREATE INDEX IF NOT EXISTS idx_runs_active ON pipeline_runs(pipeline_id)
    WHERE status IN ('pending', 'running');
```

---

### P15. N+1 query in CronScheduler.checkSchedules

**Severity**: Medium
**File**: `internal/app/cron_scheduler.go:71-116`

For every pipeline with a cron trigger, `ListRuns()` is called to check if one is already
active.

**Fix**: Add a `HasActiveRun` repo method:

```go
func (r *PipelineRunRepo) HasActiveRun(ctx context.Context, pipelineID string) (bool, error) {
    var exists bool
    err := r.db.QueryRowContext(ctx,
        "SELECT EXISTS(SELECT 1 FROM pipeline_runs WHERE pipeline_id=$1 AND status IN ('pending','running'))",
        pipelineID,
    ).Scan(&exists)
    return exists, err
}
```

---

### P16. Compose.run buffers all stdout/stderr unbounded

**Severity**: Medium
**File**: `internal/infra/docker/compose.go:176-177`

`docker compose` output is fully buffered with no size limit. `docker compose pull` for many
large images can produce megabytes.

**Fix**: Use `io.LimitedReader` or truncate after a limit:

```go
var stdout, stderr bytes.Buffer
const maxOutput = 1 << 20 // 1MB
cmd.Stdout = &limitedWriter{buf: &stdout, max: maxOutput}
cmd.Stderr = &limitedWriter{buf: &stderr, max: maxOutput}
```

---

### P17. Webhook handler fires goroutine with no timeout or shutdown hook

**Severity**: Medium
**File**: `internal/api/handler/webhook.go:109-123`

The webhook receiver fires `SyncAndRedeploy` with `context.Background()`. No timeout, no
cancellation, no graceful shutdown integration.

**Fix**:

```go
ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
defer cancel()
go func() {
    defer cancel()
    gitSvc.SyncAndRedeploy(ctx, stackName, branch)
}()
```

Better: use a shutdown-aware context from the server lifecycle.

---

### P18. Docker event listener reconnect ignores context cancellation

**Severity**: Medium
**File**: `internal/infra/docker/events.go:42`

`time.Sleep(backoff)` blocks regardless of context cancellation. If `Stop()` is called during
a 60-second backoff, the goroutine won't exit until the sleep finishes.

```go
time.Sleep(backoff)
```

**Fix**:

```go
select {
case <-time.After(backoff):
case <-ctx.Done():
    return
}
```

---

### P19. Docker client initialization blocks indefinitely

**Severity**: Medium
**File**: `internal/infra/docker/client.go:51`

`cli.Info(context.Background())` has no timeout. If Docker is unresponsive, the entire app
hangs on startup.

**Fix**:

```go
infoCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
defer cancel()
info, err := cli.Info(infoCtx)
```

---

### P20. StreamStackLogs incorrect Docker multiplex parsing

**Severity**: Medium
**File**: `internal/api/handler/sse.go:407-418`

The stack log goroutine reads raw chunks and naively checks `data[0]` for the stream type. The
8-byte Docker multiplex header can be split across reads, producing corrupted output.

**Fix**: Use `bufio.Reader` with proper Docker multiplex header parsing (like
`StreamContainerLogs` at line 172 already does).

---

### P21. Log array spread on every SSE event

**Severity**: Medium
**Files**:
- `web/src/components/container/LogViewer.tsx:54-57`
- `web/src/components/container/EventStream.tsx:52-55`

Every SSE event creates a new array via `[...prev, line]`, causing GC pressure at high event
frequency.

**Fix**: Batch events using a ref and `requestAnimationFrame`:

```tsx
const pendingRef = useRef<LogLine[]>([]);
const rafRef = useRef<number>(0);

// In SSE handler:
pendingRef.current.push(line);
if (!rafRef.current) {
  rafRef.current = requestAnimationFrame(() => {
    setLines(prev => {
      const next = [...prev, ...pendingRef.current];
      pendingRef.current = [];
      rafRef.current = 0;
      return next.length > maxLines ? next.slice(-maxLines) : next;
    });
  });
}
```

---

### P22. No cache-control headers for static assets

**Severity**: Medium
**File**: `web/astro.config.mjs`, `internal/api/static.go`

Astro outputs content-hashed filenames (`ComposeEditor.DdyA-6ab.js`) ideal for long-term
caching, but no `Cache-Control` headers are configured in the Go file server.

**Fix**: In the static file middleware, set immutable caching for hashed assets:

```go
if strings.HasPrefix(r.URL.Path, "/_astro/") {
    w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
} else if strings.HasSuffix(r.URL.Path, ".html") || r.URL.Path == "/" {
    w.Header().Set("Cache-Control", "no-cache")
}
```
