# Bootstrap: Unattended First-Boot Provisioning

> **Design doc — not implemented yet.** Captures the design from the
> 2026-04-23 discussion. Target: v0.9.0 as a bundled feature with the
> self-upgrade work, or standalone if self-upgrade slips.

## Status

- [ ] Design review (this doc)
- [ ] Section 1: env-driven admin bootstrap
- [ ] Section 2: env-driven admin API key seeding
- [ ] Section 3: `COMPOSER_BOOTSTRAP_TOKEN` race mitigation
- [ ] Section 4: SOPS age key auto-generation (opt-in)
- [ ] Section 5: composer self-registration as a stack (ties into SELF_UPGRADE_PLAN)
- [ ] Tests
- [ ] Docs (`docs/configuration.md`, `docs/security.md`)

## Problem

First-boot provisioning of composer requires a human:

1. Deploy composer (Unraid template, `docker compose up`, whatever)
2. Open the web UI in a browser
3. POST `/api/v1/auth/bootstrap` via the UI form (email + password)
4. Log in
5. Create an API key via the UI
6. Paste the key into CI / IaC config

Fine for single-operator manual setups. Breaks down for:

- **IaC provisioning** (Terraform, Ansible, Pulumi): the tool needs to
  emit an API key that CI will use, without a browser in the loop.
- **Unattended fleet deploys**: if you spin up 10 composers (test
  environments, tenant isolation, etc.), step 2-5 doesn't scale.
- **Disaster recovery**: rebuilding from backups + git — the new
  composer's bootstrap endpoint is open to anyone who reaches it first.
  Race with attackers if the URL is discoverable.

## What composer already does

- **`POST /api/v1/auth/bootstrap`** (public until consumed, 409
  afterwards). Creates the first admin via JSON body. Frontend
  `LoginPage.tsx` auto-detects empty user table and shows a splash
  creation form.
- **Encryption key auto-generation** in `internal/infra/crypto/encrypt.go`:
  on first boot with no env/file, generates and persists
  `$COMPOSER_DATA_DIR/encryption.key`. Zero config.
- **SSH key encryption on first boot** (scoped to `/home/composer/.ssh`
  after v0.8.0 safety fix): any plaintext key mounted in gets encrypted
  once, transparently decrypted on use.

Missing pieces:

- No env-driven admin creation — humans only
- No pre-seeded API key — creates a second round-trip after admin
  bootstrap
- No race mitigation on exposed endpoints
- No SOPS age key auto-generation (encryption.key got this treatment;
  SOPS didn't)
- No self-registration as a stack (ties into SELF_UPGRADE_PLAN gap —
  composer doesn't know what stack it is)

## Design

Five env vars + one sentinel file:

```
# Bootstrap admin (required for auto-bootstrap)
COMPOSER_BOOTSTRAP_EMAIL=admin@example.com
COMPOSER_BOOTSTRAP_PASSWORD=plaintext     # OR
COMPOSER_BOOTSTRAP_PASSWORD_FILE=/run/secrets/composer_admin_pw

# Bootstrap API key (optional)
COMPOSER_BOOTSTRAP_API_KEY_NAME=ci-key    # if set, also create admin API key

# Race mitigation (optional, alternative to auto-bootstrap)
COMPOSER_BOOTSTRAP_TOKEN=xyz123           # required in /auth/bootstrap body until consumed

# SOPS age key auto-generation (opt-in)
COMPOSER_AUTO_GENERATE_AGE_KEY=true
```

### Startup logic

```go
func bootstrap(ctx context.Context) error {
    empty, err := authSvc.IsUserTableEmpty(ctx)
    if err != nil { return err }
    if !empty { return nil }  // users exist, nothing to do

    email := os.Getenv("COMPOSER_BOOTSTRAP_EMAIL")
    if email == "" {
        // No auto-bootstrap — fall through to normal /auth/bootstrap flow
        return nil
    }

    pw := resolveBootstrapPassword()
    if pw == "" {
        return fmt.Errorf("COMPOSER_BOOTSTRAP_EMAIL is set but no password source found")
    }

    user, err := authSvc.Bootstrap(ctx, email, pw)
    if err != nil { return err }

    logger.Info("✓ bootstrap complete", "admin", email, "user_id", user.ID)

    if keyName := os.Getenv("COMPOSER_BOOTSTRAP_API_KEY_NAME"); keyName != "" {
        result, err := authSvc.CreateAPIKey(ctx, keyName, auth.RoleAdmin, user.ID, nil)
        if err != nil { return err }
        path := filepath.Join(dataDir, "bootstrap-api-key.txt")
        if err := os.WriteFile(path, []byte(result.PlaintextKey), 0600); err != nil {
            return err
        }
        logger.Warn("⚠ API key written to "+path+" — retrieve it once and delete the file",
            "key_name", keyName, "key_prefix", result.PlaintextKey[:12])
    }

    return nil
}

func resolveBootstrapPassword() string {
    if pw := os.Getenv("COMPOSER_BOOTSTRAP_PASSWORD"); pw != "" {
        return pw
    }
    if path := os.Getenv("COMPOSER_BOOTSTRAP_PASSWORD_FILE"); path != "" {
        b, err := os.ReadFile(path)
        if err != nil {
            logger.Warn("COMPOSER_BOOTSTRAP_PASSWORD_FILE unreadable", "path", path, "error", err)
            return ""
        }
        return strings.TrimSpace(string(b))
    }
    return ""
}
```

### Race mitigation

Two modes, one-of:

**Mode A — auto-bootstrap** (above): env vars set, admin seeded
automatically on first boot. Bootstrap endpoint is closed (409) after
seeding. No race window.

**Mode B — token-gated bootstrap**: `COMPOSER_BOOTSTRAP_TOKEN` set, no
`COMPOSER_BOOTSTRAP_EMAIL`. The `/api/v1/auth/bootstrap` POST handler
requires a `token` field in the request body that matches the env var
until the admin is created. Prevents drive-by bootstrap claims on
exposed endpoints.

```go
// handler/auth.go Bootstrap:
func (h *AuthHandler) Bootstrap(ctx context.Context, input *dto.BootstrapInput) (*dto.BootstrapOutput, error) {
    if token := os.Getenv("COMPOSER_BOOTSTRAP_TOKEN"); token != "" {
        if subtle.ConstantTimeCompare([]byte(input.Body.Token), []byte(token)) != 1 {
            return nil, huma.Error403Forbidden("bootstrap token required or incorrect")
        }
    }
    // ... rest of existing flow
}
```

DTO gains an optional `token` field on `BootstrapInput`:

```go
type BootstrapInput struct {
    Body struct {
        Email    string `json:"email" format:"email" minLength:"3" maxLength:"320"`
        Password string `json:"password" minLength:"8" maxLength:"72"`
        Token    string `json:"token,omitempty" maxLength:"256" doc:"Required when COMPOSER_BOOTSTRAP_TOKEN env is set. One-time, discarded after admin creation."`
    }
}
```

### SOPS age key auto-generation

Currently `sops.LoadGlobalAgeKey` checks env then file, returns empty
string if neither found. Extend with an auto-gen path gated by
`COMPOSER_AUTO_GENERATE_AGE_KEY=true`:

```go
// on first boot, after other bootstrap:
if ageKey := sops.LoadGlobalAgeKey(dataDir); ageKey == "" {
    if os.Getenv("COMPOSER_AUTO_GENERATE_AGE_KEY") == "true" {
        priv, pub, err := sops.GenerateAgeKey()
        if err != nil {
            logger.Warn("SOPS age auto-generation failed", "error", err)
        } else {
            sops.SaveAgeKey(dataDir, priv, pub)
            logger.Info("✓ SOPS age key generated",
                "public_key", pub,
                "file", filepath.Join(dataDir, "age.key"))
            logger.Warn("Set recipient in your .sops.yaml:",
                "recipient", pub)
        }
    }
}
```

Gated by env var because existing deployments that rely on
`LoadGlobalAgeKey == ""` (SOPS disabled) shouldn't suddenly get a key.

### Self-registration as a stack

Ties into SELF_UPGRADE_PLAN.md. On startup, if composer detects it's
running under `docker compose` (via `com.docker.compose.project` label
on its own container) OR has `COMPOSER_SELF_STACK_NAME` env set,
auto-register a stack row in the DB:

```go
stackName := os.Getenv("COMPOSER_SELF_STACK_NAME")
if stackName == "" {
    // Try compose label
    stackName, _ = discoverComposeProjectLabel()  // returns "" if not compose
}
if stackName != "" {
    _, err := stackRepo.GetByName(ctx, stackName)
    if errors.Is(err, stack.ErrNotFound) {
        selfStack := &stack.Stack{
            Name:   stackName,
            Source: stack.SourceLocal,  // or Git if we detect a .git dir
            Path:   "<known-or-env-path>",
            Self:   true,  // new boolean field: this is composer itself
        }
        stackRepo.Create(ctx, selfStack)
        logger.Info("registered composer as a stack", "name", stackName)
    }
}
```

Implications:
- `Stack.Self bool` is a new field. Migration + DTO update.
- UI can show a special badge on the composer-self stack; most operations
  (delete, stop) should be forbidden on it (safety).
- `/api/v1/system/upgrade` uses this stack row to locate the compose
  file / run spec.

## API surface changes

### Modified

- `POST /api/v1/auth/bootstrap` gains optional `token` body field; behavior
  unchanged when `COMPOSER_BOOTSTRAP_TOKEN` env is empty.

### New (stretch goal)

- `GET /api/v1/system/bootstrap-status` (public) — returns whether auto-bootstrap
  was used and whether a bootstrap API key file exists. Lets IaC tools know
  where to find the seeded key.

```json
{
  "user_exists": true,
  "auto_bootstrapped": true,
  "bootstrap_api_key_file": "/opt/composer/bootstrap-api-key.txt",
  "bootstrap_api_key_file_exists": true
}
```

## Security considerations

- **Env vars with secrets are not great**: exposed via `ps auxe`,
  `/proc/<pid>/environ`, container inspect, host-side logging. Prefer
  `_FILE` variants where possible. Document this clearly.
- **`bootstrap-api-key.txt` file lifecycle**: 0600 permissions, written
  once. Operator retrieves it and either deletes manually or we add a
  `COMPOSER_BOOTSTRAP_API_KEY_DELETE_AFTER=24h` to auto-delete. Don't
  auto-delete by default — operators might retrieve it days later.
- **`COMPOSER_BOOTSTRAP_PASSWORD` in plaintext env**: bad practice. The
  `_FILE` variant is preferred. Document with explicit guidance in
  `docs/security.md`.
- **Bootstrap token is single-use in practice** (admin is seeded
  exactly once). Don't re-check it after the admin exists; the
  bootstrap endpoint returns 409 regardless of token value once
  consumed.
- **Constant-time comparison**: use `subtle.ConstantTimeCompare` on
  token check to avoid timing side-channel.
- **`_FILE` variant**: standard 12-factor convention (e.g. Postgres,
  Docker secrets). Read on startup only; file deletion after read is
  left to the operator / orchestrator.

## Testing strategy

### Unit

- `resolveBootstrapPassword()`: env var set / file path set / both set
  (env wins) / neither set
- `BootstrapHandler` with token env set: correct token, wrong token,
  missing token, after first bootstrap (token check disabled)
- `autoBootstrap()`: empty user table + env vars → creates admin + key;
  already-populated user table → no-op; missing password → error;
  create API key fails → returns error with cleanup
- SOPS auto-gen: env set + no existing key → generates; existing env key
  → skips; existing file key → skips

### Integration

- Full lifecycle: container starts with auto-bootstrap env, composer
  boots, admin + API key are created, API key file exists with 0600
  perms, subsequent boot doesn't re-create (idempotent).

### Manual / E2E

- Unraid template with bootstrap env vars set → fresh install → browser
  loads login page (not splash) → operator pastes API key from
  `/opt/composer/bootstrap-api-key.txt`
- Terraform / Ansible flow: provisioning sets env vars, retrieves key
  from file via SSH or volume mount, populates CI secret

## Migration path

No data migration needed. Fresh deployments opt into auto-bootstrap by
setting env vars. Existing deployments that rely on the UI bootstrap
flow are unchanged — env vars aren't required.

Token mitigation is opt-in; if no one sets `COMPOSER_BOOTSTRAP_TOKEN`,
the endpoint behaves exactly as today. Existing deployments that
already bootstrapped don't care either way (user table is non-empty,
so the endpoint 409s regardless).

## Out of scope

- **SSO / OIDC at bootstrap time**. Auto-bootstrap creates a
  password-authenticated admin. If the operator wants OIDC-only, they
  delete the password after first login or edit the user record via
  `/api/v1/users/{id}`. Bootstrap via OIDC group claims is a much
  bigger design.
- **Secrets rotation orchestration**. This plan seeds secrets on first
  boot; rotating them is a separate concern. See issue: "rotate
  COMPOSER_ENCRYPTION_KEY without losing access to encrypted DB rows"
  (needs an in-place re-encrypt migration).
- **Multi-tenant bootstrap**. If composer ever supports multiple
  tenant admins at bootstrap, env vars scale badly. This doc assumes
  single-admin-seed.

## Related plans

- `SELF_UPGRADE_PLAN.md` — Section 5 here (self-registration as a
  stack) feeds the self-upgrade endpoint. Implement in that order:
  bootstrap → self-register → self-upgrade.
