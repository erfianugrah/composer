# Docker Hosts (Multi-Host) Implementation Plan

> **For agentic workers:** This plan is executed by the self-correcting loop (`.pi/harness.json` in this repo). Per-task commit steps are intentionally OMITTED - the loop owns git state. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let one composerd manage stacks across multiple docker daemons (local socket + remote TCP/mTLS endpoints), instead of one daemon per composerd process.

**Architecture:** A new `docker_hosts` table holds named remote endpoints (TCP + optional mTLS cert dir). `stacks.host_id` is a nullable FK - NULL means the default host (today's `COMPOSER_DOCKER_HOST`/socket auto-detection, fully back-compatible). A `docker.Factory` lazily builds and caches per-host SDK clients and `Compose` wrappers (per-host `DOCKER_HOST` + `DOCKER_TLS_VERIFY`/`DOCKER_CERT_PATH` env), and every consumer (stack service, pipeline executor, API handlers, event listeners) resolves its docker access through the factory. Self-upgrade stays pinned to the default host. API references hosts by **name**; the DB stores **id**.

**Tech Stack:** Go 1.x, database/sql (SQLite + Postgres via goose), docker SDK (`github.com/docker/docker/client`), Huma v2 (OpenAPI), Astro + React islands (bun), Playwright smoke tests.

**Safety constraints (repo AGENTS.md):** NEVER run `./composerd` or `go run ./cmd/composerd` (startup hook AES-encrypts `$HOME/.ssh`). Validate only via `go build` / `go vet` / `go test`. Unit tests only - no `-tags=integration`. Certs in tests are generated into `t.TempDir()` (never committed). Do not weaken or delete existing tests. This repo is PUBLIC: no real hostnames, IPs, deployment names, or proxy products anywhere in code, tests, or docs - generic examples only (`remote1`, `tcp://docker-remote.example:2376`).

---

## Design decisions (locked - do not relitigate mid-plan)

1. **Default host is implicit.** `host_id NULL` = the daemon composerd itself was configured with (`COMPOSER_DOCKER_HOST` env / socket detection). No `docker_hosts` row exists for it. Remote hosts are additive rows. Name used in API/UI for the default: `"local"`.
2. **One cert column, not three.** `docker_hosts.cert_dir TEXT NULL` - a directory containing `ca.pem`, `cert.pem`, `key.pem` (docker CLI naming convention). The SDK client reads the three files from it; the Compose wrapper exports it as `DOCKER_CERT_PATH` + `DOCKER_TLS_VERIFY=1`. NULL cert_dir = plain TCP or local socket.
3. **API by name, DB by id.** Create-stack payloads carry `host: "<name>"`; the service layer resolves name->id and stores `host_id`. Unknown name = 422. Empty/absent = default host.
4. **TLSConfig replaces env-only TLS.** `docker.NewClient(explicitHost string, tls *docker.TLSConfig)` - nil = today's env behaviour (regression-safe), non-nil = explicit `client.WithTLSClientConfig`.
5. **Self-upgrade pinned to default host.** Its helper hardcodes `/var/run/docker.sock`; out of scope to generalize. It keeps receiving the default client.
6. **Events fan-in.** One `EventListener` per host (default + each registered). Domain container events gain a `HostName` field (empty = default).
7. **UI by name.** Stack list/detail show a host badge (`local` when NULL). Host management is a Settings-page card (RegistryAuthSettings pattern), not a new nav page.

---

### Task 1: `docker_hosts` table + `domain/host` + `host_repo`

**Files:**
- Modify: `internal/infra/store/migrations.go` (add migration 008)
- Create: `internal/domain/host/host.go`
- Create: `internal/domain/host/repository.go`
- Create: `internal/infra/store/host_repo.go`
- Test: `internal/infra/store/host_repo_test.go`

- [ ] **Step 1: Write the failing domain test**

Create `internal/domain/host/host_test.go`:

```go
package host

import "testing"

func TestValidate(t *testing.T) {
	cases := []struct {
		name    string
		h       Host
		wantErr bool
	}{
		{"valid tcp+mTLS", Host{Name: "remote1", Endpoint: "tcp://docker-remote.example:2376", CertDir: "/certs"}, false},
		{"valid tcp plain", Host{Name: "edge", Endpoint: "tcp://10.0.0.2:2375"}, false},
		{"valid unix socket", Host{Name: "nas", Endpoint: "unix:///run/docker.sock"}, false},
		{"empty name", Host{Name: "", Endpoint: "tcp://x:2375"}, true},
		{"empty endpoint", Host{Name: "x", Endpoint: ""}, true},
		{"bad scheme", Host{Name: "x", Endpoint: "http://x"}, true},
		{"name with space", Host{Name: "my host", Endpoint: "tcp://x:2375"}, true},
		{"reserved name", Host{Name: "local", Endpoint: "tcp://x:2375"}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.h.Validate(); (err != nil) != tc.wantErr {
				t.Fatalf("Validate() err=%v wantErr=%v", err, tc.wantErr)
			}
		})
	}
}
```

Run: `go test ./internal/domain/host/ -v` - Expected: FAIL (package does not exist).

- [ ] **Step 2: Create the domain package**

`internal/domain/host/host.go`:

```go
// Package host defines the DockerHost aggregate: a named remote docker
// daemon endpoint composerd can manage stacks on. The DEFAULT host (the
// daemon composerd was configured with via COMPOSER_DOCKER_HOST / socket
// auto-detection) is implicit and has no row - stacks.host_id NULL means
// default. Rows in docker_hosts are ADDITIONAL remotes.
package host

import (
	"fmt"
	"regexp"
	"strings"
	"time"
)

// DefaultName is the display name for the implicit default host.
const DefaultName = "local"

type Host struct {
	ID        int64
	Name      string
	Endpoint  string // tcp://host:2376 | tcp://host:2375 | unix:///path.sock
	CertDir   string // dir holding ca.pem/cert.pem/key.pem; "" = no mTLS
	CreatedAt time.Time
	UpdatedAt time.Time
}

var nameRe = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,62}$`)

func (h *Host) Validate() error {
	if !nameRe.MatchString(h.Name) {
		return fmt.Errorf("host name %q: must be lowercase dns-label-ish (a-z0-9_-, max 63)", h.Name)
	}
	if h.Name == DefaultName {
		return fmt.Errorf("host name %q is reserved for the default host", DefaultName)
	}
	switch {
	case strings.HasPrefix(h.Endpoint, "tcp://"),
		strings.HasPrefix(h.Endpoint, "unix://"),
		strings.HasPrefix(h.Endpoint, "ssh://"):
	default:
		return fmt.Errorf("endpoint %q: scheme must be tcp://, unix:// or ssh://", h.Endpoint)
	}
	return nil
}
```

`internal/domain/host/repository.go`:

```go
package host

import "context"

type Repository interface {
	Create(ctx context.Context, h *Host) error
	GetByID(ctx context.Context, id int64) (*Host, error)
	GetByName(ctx context.Context, name string) (*Host, error)
	List(ctx context.Context) ([]*Host, error)
	Update(ctx context.Context, h *Host) error
	Delete(ctx context.Context, id int64) error
	// CountStacks returns how many stacks reference this host (delete guard).
	CountStacks(ctx context.Context, id int64) (int, error)
}
```

Run: `go test ./internal/domain/host/ -v` - Expected: PASS.

- [ ] **Step 3: Write the failing repo test**

`internal/infra/store/host_repo_test.go` - follow the existing sqlite repo test idiom in this package (see how `stack_repo_test.go` / `registry_repo_test.go` open a test DB - MATCH THE EXISTING HELPER, do not invent a new one). Tests:

```go
// TestHostRepoCRUD: Create -> GetByID/GetByName round-trip all fields,
// List returns both, Update changes endpoint, Delete removes,
// GetByName on missing returns nil, nil (not error).
// TestHostRepoDuplicateName: second Create with same name fails.
// TestHostRepoCountStacks: insert host, insert stack row with host_id set,
// CountStacks == 1; after deleting the stack row, == 0.
```

Run: `go test ./internal/infra/store/ -run TestHostRepo -v` - Expected: FAIL (`host_repo.go` does not exist, `docker_hosts` table does not exist).

- [ ] **Step 4: Migration 008 (Go migration, both dialects)**

In `internal/infra/store/migrations.go`, add to the slice returned by `goMigrations`:

```go
		// 008: docker_hosts + stacks.host_id (multi-host support).
		goose.NewGoMigration(
			8,
			&goose.GoFunc{RunTx: func(ctx context.Context, tx *sql.Tx) error {
				return execAll(ctx, tx, dockerHostsUp(dbType))
			}},
			&goose.GoFunc{RunTx: func(ctx context.Context, tx *sql.Tx) error {
				return execAll(ctx, tx, dockerHostsDown())
			}},
		),
```

and the statement builders:

```go
func dockerHostsUp(dbType DBType) []string {
	var idCol string
	switch dbType {
	case DBTypeSQLite:
		idCol = "id INTEGER PRIMARY KEY AUTOINCREMENT"
	default: // Postgres
		idCol = "id BIGSERIAL PRIMARY KEY"
	}
	return []string{
		`CREATE TABLE IF NOT EXISTS docker_hosts (
		    ` + idCol + `,
		    name       TEXT NOT NULL UNIQUE,
		    endpoint   TEXT NOT NULL,
		    cert_dir   TEXT NOT NULL DEFAULT '',
		    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
		    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`,
		// NULL = default host (the daemon composerd was configured with).
		`ALTER TABLE stacks ADD COLUMN host_id INTEGER NULL
		    REFERENCES docker_hosts(id)`,
		`CREATE INDEX IF NOT EXISTS idx_stacks_host_id ON stacks(host_id)`,
	}
}

func dockerHostsDown() []string {
	return []string{
		`DROP INDEX IF EXISTS idx_stacks_host_id`,
		`ALTER TABLE stacks DROP COLUMN host_id`,
		`DROP TABLE IF EXISTS docker_hosts`,
	}
}
```

Note: `ALTER TABLE ... ADD COLUMN ... REFERENCES` and `DROP COLUMN` work on both modern SQLite (>= 3.35, which modernc.org/sqlite ships) and Postgres. Migration numbering: 007 is the latest SQL file, 005 is Go - goose tracks them in one sequence, so 008 is next regardless of Go-vs-SQL.

- [ ] **Step 5: `internal/infra/store/host_repo.go`**

```go
package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/erfianugrah/composer/internal/domain/host"
)

type HostRepo struct{ db *sql.DB }

func NewHostRepo(db *sql.DB) *HostRepo { return &HostRepo{db: db} }

func (r *HostRepo) Create(ctx context.Context, h *host.Host) error {
	res, err := r.db.ExecContext(ctx,
		`INSERT INTO docker_hosts (name, endpoint, cert_dir, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5)`,
		h.Name, h.Endpoint, h.CertDir, h.CreatedAt, h.UpdatedAt)
	if err != nil {
		return fmt.Errorf("inserting docker host: %w", err)
	}
	h.ID, _ = res.LastInsertId()
	return nil
}

func (r *HostRepo) scan(row *sql.Row) (*host.Host, error) {
	h := &host.Host{}
	err := row.Scan(&h.ID, &h.Name, &h.Endpoint, &h.CertDir, &h.CreatedAt, &h.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return h, nil
}

func (r *HostRepo) GetByID(ctx context.Context, id int64) (*host.Host, error) {
	return r.scan(r.db.QueryRowContext(ctx,
		`SELECT id, name, endpoint, cert_dir, created_at, updated_at
		 FROM docker_hosts WHERE id = $1`, id))
}

func (r *HostRepo) GetByName(ctx context.Context, name string) (*host.Host, error) {
	return r.scan(r.db.QueryRowContext(ctx,
		`SELECT id, name, endpoint, cert_dir, created_at, updated_at
		 FROM docker_hosts WHERE name = $1`, name))
}

func (r *HostRepo) List(ctx context.Context) ([]*host.Host, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, name, endpoint, cert_dir, created_at, updated_at
		 FROM docker_hosts ORDER BY name ASC LIMIT 200`)
	if err != nil {
		return nil, fmt.Errorf("listing docker hosts: %w", err)
	}
	defer rows.Close()
	var out []*host.Host
	for rows.Next() {
		h := &host.Host{}
		if err := rows.Scan(&h.ID, &h.Name, &h.Endpoint, &h.CertDir, &h.CreatedAt, &h.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scanning docker host: %w", err)
		}
		out = append(out, h)
	}
	return out, rows.Err()
}

func (r *HostRepo) Update(ctx context.Context, h *host.Host) error {
	res, err := r.db.ExecContext(ctx,
		`UPDATE docker_hosts SET name=$2, endpoint=$3, cert_dir=$4, updated_at=$5
		 WHERE id=$1`,
		h.ID, h.Name, h.Endpoint, h.CertDir, h.UpdatedAt)
	if err != nil {
		return fmt.Errorf("updating docker host: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotUpdated
	}
	return nil
}

func (r *HostRepo) Delete(ctx context.Context, id int64) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM docker_hosts WHERE id=$1`, id)
	return err
}

func (r *HostRepo) CountStacks(ctx context.Context, id int64) (int, error) {
	var n int
	err := r.db.QueryRowContext(ctx,
		`SELECT count(*) FROM stacks WHERE host_id = $1`, id).Scan(&n)
	return n, err
}
```

- [ ] **Step 6: Run the repo tests**

Run: `go test ./internal/infra/store/ -run TestHostRepo -v`
Expected: PASS. If `CountStacks` fails because the test inserted a stack without the new column - the migration adds `host_id` nullable, so plain old INSERTs still work; the test sets it explicitly via `INSERT INTO stacks (name, path, source, host_id) VALUES (...)`.

Also run the whole store package: `go test ./internal/infra/store/ -count=1` - all pre-existing tests must stay green (the migration runs on every test DB open; a syntax error breaks everything).

---

### Task 2: `stacks.host_id` through the stack domain + `stack_repo`

**Files:**
- Modify: `internal/domain/stack/aggregate.go` (Stack struct + constructors)
- Modify: `internal/infra/store/stack_repo.go` (4 SQL sites + 3 Scan sites)
- Test: `internal/infra/store/stack_repo_test.go` (extend)

- [ ] **Step 1: Write the failing test**

Add to `internal/infra/store/stack_repo_test.go`:

```go
// TestStackRepoHostIDRoundTrip: create a docker_hosts row, create a stack
// with HostID set, GetByName/List return the same HostID; a stack created
// without HostID comes back with HostID == nil.
```

Run: `go test ./internal/infra/store/ -run TestStackRepoHostID -v` - Expected: FAIL (`stack.Stack` has no `HostID` field).

- [ ] **Step 2: Domain field**

In `internal/domain/stack/aggregate.go`, add to `Stack`:

```go
	// HostID selects which docker daemon this stack deploys to.
	// nil = default host (host.DefaultName). Non-nil = docker_hosts.id.
	HostID *int64
```

`NewStack`/`NewGitStack` keep their signatures (HostID stays nil by default); callers set `st.HostID = &id` explicitly. This avoids touching every existing constructor call site.

- [ ] **Step 3: Repo SQL + scans**

In `internal/infra/store/stack_repo.go`:

- `Create`: `INSERT INTO stacks (name, path, source, host_id, created_at, updated_at) VALUES ($1,$2,$3,$4,$5,$6)` with `s.HostID` (database/sql maps `*int64` nil -> NULL automatically).
- `GetByName` + `List`: add `host_id` to the SELECT column list (position after `source`) and `&s.HostID` to the Scan destinations.
- `Update`: `UPDATE stacks SET path=$2, source=$3, host_id=$4, updated_at=$5 WHERE name=$1`.

- [ ] **Step 4: Run tests**

Run: `go test ./internal/infra/store/ ./internal/domain/... -count=1`
Expected: PASS (new + all pre-existing).

---

### Task 3: `app.HostService`

**Files:**
- Create: `internal/app/host_service.go`
- Test: `internal/app/host_service_test.go`

- [ ] **Step 1: Write the failing test**

`internal/app/host_service_test.go` - use a hand-rolled fake `host.Repository` (map-backed, same idiom as existing app-layer tests in this repo). Cases:

```go
// TestHostServiceCreateValidates: invalid name/endpoint -> error, repo not called.
// TestHostServiceDeleteGuard: CountStacks > 0 -> Delete returns error naming
// the count; CountStacks == 0 -> Delete proceeds.
// TestHostServiceResolve: ResolveHostID("remote1") returns &id for a known
// host; ResolveHostID("") and ResolveHostID("local") return nil, nil (default);
// ResolveHostID("nope") returns a "unknown docker host" error.
```

Run: `go test ./internal/app/ -run TestHostService -v` - Expected: FAIL (undefined `HostService`).

- [ ] **Step 2: Implement**

`internal/app/host_service.go`:

```go
package app

import (
	"context"
	"fmt"
	"time"

	"github.com/erfianugrah/composer/internal/domain/host"
	"go.uber.org/zap"
)

type HostService struct {
	repo host.Repository
	log  *zap.Logger
}

func NewHostService(repo host.Repository, log *zap.Logger) *HostService {
	return &HostService{repo: repo, log: log}
}

func (s *HostService) List(ctx context.Context) ([]*host.Host, error) {
	return s.repo.List(ctx)
}

func (s *HostService) Get(ctx context.Context, id int64) (*host.Host, error) {
	return s.repo.GetByID(ctx, id)
}

func (s *HostService) Create(ctx context.Context, h *host.Host) error {
	if err := h.Validate(); err != nil {
		return err
	}
	now := time.Now().UTC()
	h.CreatedAt, h.UpdatedAt = now, now
	return s.repo.Create(ctx, h)
}

func (s *HostService) Update(ctx context.Context, h *host.Host) error {
	if err := h.Validate(); err != nil {
		return err
	}
	h.UpdatedAt = time.Now().UTC()
	return s.repo.Update(ctx, h)
}

func (s *HostService) Delete(ctx context.Context, id int64) error {
	n, err := s.repo.CountStacks(ctx, id)
	if err != nil {
		return fmt.Errorf("checking stack references: %w", err)
	}
	if n > 0 {
		return fmt.Errorf("host still has %d stack(s) assigned; reassign them first", n)
	}
	return s.repo.Delete(ctx, id)
}

// ResolveHostID maps an API-facing host name to a docker_hosts.id.
// "" and host.DefaultName both mean the default host -> nil, nil.
func (s *HostService) ResolveHostID(ctx context.Context, name string) (*int64, error) {
	if name == "" || name == host.DefaultName {
		return nil, nil
	}
	h, err := s.repo.GetByName(ctx, name)
	if err != nil {
		return nil, fmt.Errorf("resolving docker host: %w", err)
	}
	if h == nil {
		return nil, fmt.Errorf("unknown docker host %q", name)
	}
	return &h.ID, nil
}
```

- [ ] **Step 3: Run**

Run: `go test ./internal/app/ -run TestHostService -v` - Expected: PASS.

---

### Task 4: per-host TLS in `docker.NewClient`

**Files:**
- Modify: `internal/infra/docker/client.go` (`NewClient` signature + TLS branch)
- Modify: `cmd/composerd/main.go:135` (call-site update)
- Test: `internal/infra/docker/client_tls_test.go` (new)
- Modify: any existing callers of `NewClient` in tests (grep `NewClient(` across the repo; update to the new signature with `nil`)

- [ ] **Step 1: Write the failing test**

`internal/infra/docker/client_tls_test.go` (no integration tag):

```go
package docker

import (
	"os"
	"path/filepath"
	"testing"
)

// NewClient with a TLSConfig must NOT depend on DOCKER_TLS_VERIFY /
// DOCKER_CERT_PATH env. We can't dial a real daemon in unit tests, so assert
// construction-time behaviour: missing cert files -> error naming the file;
// complete cert dir -> client constructs (no dial happens at construction).
func TestNewClientTLSMissingCerts(t *testing.T) {
	dir := t.TempDir() // empty: no ca.pem/cert.pem/key.pem
	_, err := NewClient("tcp://docker-remote.example:2376", &TLSConfig{CertDir: dir})
	if err == nil {
		t.Fatal("expected error for missing cert files")
	}
}

func TestNewClientTLSNilKeepsEnvBehaviour(t *testing.T) {
	// nil TLSConfig = legacy env path; a tcp host constructs fine with no env set.
	c, err := NewClient("tcp://docker-remote.example:2376", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer c.Close()
	if c.Host() != "tcp://docker-remote.example:2376" {
		t.Fatalf("Host() = %q", c.Host())
	}
}

// writeTestCerts generates a self-signed CA+client cert triplet into dir
// (ca.pem/cert.pem/key.pem). Reuse an existing helper if one exists in this
// package's tests; otherwise generate with crypto/x509 (see
// client_fromenv_test.go for the package's white-box idiom).
func TestNewClientTLSWithCerts(t *testing.T) {
	dir := t.TempDir()
	writeTestCerts(t, dir)
	for _, f := range []string{"ca.pem", "cert.pem", "key.pem"} {
		if _, err := os.Stat(filepath.Join(dir, f)); err != nil {
			t.Fatalf("fixture missing %s: %v", f, err)
		}
	}
	c, err := NewClient("tcp://docker-remote.example:2376", &TLSConfig{CertDir: dir})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer c.Close()
}
```

Run: `go test ./internal/infra/docker/ -run TestNewClientTLS -v` - Expected: FAIL (`TLSConfig` undefined, `NewClient` arity).

- [ ] **Step 2: Implement**

In `internal/infra/docker/client.go`:

```go
// TLSConfig pins explicit mTLS material for a remote host. CertDir must
// contain ca.pem, cert.pem, key.pem (docker CLI naming). When nil, NewClient
// falls back to the legacy env behaviour (FromEnv: DOCKER_TLS_VERIFY /
// DOCKER_CERT_PATH) - which is correct for the DEFAULT host only.
type TLSConfig struct{ CertDir string }
```

Change the signature to `func NewClient(explicitHost string, tls *TLSConfig) (*Client, error)` and build opts:

```go
	opts := []dockerclient.Opt{
		dockerclient.WithHost(host),
		dockerclient.WithAPIVersionNegotiation(),
	}
	if tls != nil {
		if tls.CertDir == "" {
			return nil, fmt.Errorf("TLS config requires CertDir")
		}
		ca := filepath.Join(tls.CertDir, "ca.pem")
		cert := filepath.Join(tls.CertDir, "cert.pem")
		key := filepath.Join(tls.CertDir, "key.pem")
		for _, f := range []string{ca, cert, key} {
			if _, err := os.Stat(f); err != nil {
				return nil, fmt.Errorf("TLS material: %w", err)
			}
		}
		opts = append(opts, dockerclient.WithTLSClientConfig(ca, cert, key))
	} else {
		// FromEnv BEFORE WithHost is no longer possible with this opt order -
		// keep the existing legacy path EXACTLY as it was: build the env-based
		// opt list first, then append WithHost (see client_fromenv_test.go:
		// "Explicit host must always win over DOCKER_HOST").
		opts = []dockerclient.Opt{
			dockerclient.FromEnv,
			dockerclient.WithHost(host),
			dockerclient.WithAPIVersionNegotiation(),
		}
	}
```

(Keep the structure tidy: compute `host` from `explicitHost`/detectSocket as today, then branch on `tls != nil` for the opt list. The nil branch must stay byte-for-byte equivalent in behaviour to the current code - `client_fromenv_test.go` pins it.)

Update `cmd/composerd/main.go:135` to `docker.NewClient(cfg.DockerHost, nil)` and every test caller to add the `nil` second arg (`rg -n 'NewClient\(' --type go` to find them).

- [ ] **Step 3: Run**

Run: `go test ./internal/infra/docker/ -count=1`
Expected: PASS - new TLS tests plus ALL pre-existing (`client_fromenv_test.go` must stay green untouched; it pins the legacy opt order).

---

### Task 5: per-host TLS env in the Compose wrapper

**Files:**
- Modify: `internal/infra/docker/compose.go` (struct + `applyExtraEnv` + `RunPTY`)
- Test: `internal/infra/docker/compose_env_test.go` (extend - this file exists from the mTLS hardening work)

- [ ] **Step 1: Write the failing test**

Add to `internal/infra/docker/compose_env_test.go`:

```go
// With a Compose built via NewComposeTLS(host, &TLSConfig{CertDir: "/x"}, log):
// applyExtraEnv output contains DOCKER_HOST=host, DOCKER_TLS_VERIFY=1,
// DOCKER_CERT_PATH=/x. The RunPTY env construction contains the same three.
// With plain NewCompose(host, log) none of the TLS vars are added (the legacy
// env-inheritance behaviour from the mTLS pin tests stays intact).
```

Run: `go test ./internal/infra/docker/ -run TestComposeTLS -v` - Expected: FAIL (`NewComposeTLS` undefined).

- [ ] **Step 2: Implement**

In `internal/infra/docker/compose.go`:

```go
type Compose struct {
	dockerHost string // passed as DOCKER_HOST env var
	certDir    string // when non-empty: DOCKER_TLS_VERIFY=1 + DOCKER_CERT_PATH
	log        *zap.Logger
}

func NewCompose(dockerHost string, log *zap.Logger) *Compose {
	return &Compose{dockerHost: dockerHost, log: log}
}

// NewComposeTLS is the per-remote-host variant: the docker CLI child gets
// explicit mTLS env instead of relying on composerd's process env (which is
// the default host's material - wrong for any other host).
func NewComposeTLS(dockerHost string, tls *TLSConfig, log *zap.Logger) *Compose {
	c := NewCompose(dockerHost, log)
	if tls != nil {
		c.certDir = tls.CertDir
	}
	return c
}
```

In `applyExtraEnv` (after the `DOCKER_HOST` append) and in the inline env construction inside `RunPTY` (compose.go:~336), add the identical block:

```go
	if c.certDir != "" {
		env = append(env, "DOCKER_TLS_VERIFY=1", "DOCKER_CERT_PATH="+c.certDir)
	}
```

Factor it into a small helper (e.g. `func (c *Compose) dockerEnv() []string`) so both sites share it - do NOT let the two sites drift.

- [ ] **Step 3: Run**

Run: `go test ./internal/infra/docker/ -count=1` - Expected: PASS.

---

### Task 6: `docker.Factory` (per-host client + compose cache)

**Files:**
- Create: `internal/infra/docker/factory.go`
- Test: `internal/infra/docker/factory_test.go`

- [ ] **Step 1: Write the failing test**

`internal/infra/docker/factory_test.go` (white-box, no daemon needed):

```go
// fakeHostStore implements HostStore with a map.
//
// TestFactoryDefault: ClientFor(nil) / ComposeFor(nil) return the DEFAULT
// client/compose (same pointer on repeated calls) without touching the store.
// TestFactoryRemoteConstruction: register host remote1=tcp://docker-remote.example:2376
// (no certs) -> ClientFor(&id) constructs, Host() matches; repeated call hits
// the cache (store.GetByID called once - count calls in the fake).
// TestFactoryUnknownID: ClientFor(&unknownID) -> error "unknown docker host".
// TestFactoryMissingCerts: host with CertDir pointing at a nonexistent dir ->
// error naming TLS material.
// TestFactoryClientForName: "" and "local" -> default; "remote1" -> remote.
```

Run: `go test ./internal/infra/docker/ -run TestFactory -v` - Expected: FAIL (`factory.go` absent).

- [ ] **Step 2: Implement**

`internal/infra/docker/factory.go`:

```go
package docker

import (
	"context"
	"fmt"
	"sync"

	"github.com/erfianugrah/composer/internal/domain/host"
	"go.uber.org/zap"
)

// HostStore is the slice of host.Repository the Factory needs.
type HostStore interface {
	GetByID(ctx context.Context, id int64) (*host.Host, error)
	GetByName(ctx context.Context, name string) (*host.Host, error)
}

// Factory resolves per-host docker access. The default host's client/compose
// are built once at boot and shared; remote hosts are constructed lazily on
// first use and cached by docker_hosts.id. Thread-safe.
type Factory struct {
	defClient  *Client
	defCompose *Compose
	store      HostStore
	log        *zap.Logger

	mu       sync.Mutex
	clients  map[int64]*Client
	composes map[int64]*Compose
}

func NewFactory(defClient *Client, defCompose *Compose, store HostStore, log *zap.Logger) *Factory {
	return &Factory{
		defClient: defClient, defCompose: defCompose, store: store, log: log,
		clients: map[int64]*Client{}, composes: map[int64]*Compose{},
	}
}

// DefaultClient / DefaultCompose are for consumers pinned to the default host
// (self-upgrade, system info).
func (f *Factory) DefaultClient() *Client   { return f.defClient }
func (f *Factory) DefaultCompose() *Compose { return f.defCompose }

func (f *Factory) ClientFor(ctx context.Context, hostID *int64) (*Client, error) {
	if hostID == nil {
		return f.defClient, nil
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if c, ok := f.clients[*hostID]; ok {
		return c, nil
	}
	h, err := f.store.GetByID(ctx, *hostID)
	if err != nil {
		return nil, fmt.Errorf("loading docker host %d: %w", *hostID, err)
	}
	if h == nil {
		return nil, fmt.Errorf("unknown docker host id %d", *hostID)
	}
	c, err := clientForHost(h)
	if err != nil {
		return nil, err
	}
	f.clients[*hostID] = c
	return c, nil
}

func (f *Factory) ComposeFor(ctx context.Context, hostID *int64) (*Compose, error) {
	if hostID == nil {
		return f.defCompose, nil
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if c, ok := f.composes[*hostID]; ok {
		return c, nil
	}
	h, err := f.store.GetByID(ctx, *hostID)
	if err != nil {
		return nil, fmt.Errorf("loading docker host %d: %w", *hostID, err)
	}
	if h == nil {
		return nil, fmt.Errorf("unknown docker host id %d", *hostID)
	}
	if _, err := clientForHost(h); err != nil { // validate TLS material eagerly
		return nil, err
	}
	c := NewComposeTLS(h.Endpoint, tlsForHost(h), f.log)
	f.composes[*hostID] = c
	return c, nil
}

// ClientForName resolves API-facing names ("" / "local" = default).
func (f *Factory) ClientForName(ctx context.Context, name string) (*Client, error) {
	if name == "" || name == host.DefaultName {
		return f.defClient, nil
	}
	h, err := f.store.GetByName(ctx, name)
	if err != nil {
		return nil, fmt.Errorf("resolving docker host: %w", err)
	}
	if h == nil {
		return nil, fmt.Errorf("unknown docker host %q", name)
	}
	return f.ClientFor(ctx, &h.ID)
}

func tlsForHost(h *host.Host) *TLSConfig {
	if h.CertDir == "" {
		return nil
	}
	return &TLSConfig{CertDir: h.CertDir}
}

func clientForHost(h *host.Host) (*Client, error) {
	c, err := NewClient(h.Endpoint, tlsForHost(h))
	if err != nil {
		return nil, fmt.Errorf("docker host %q: %w", h.Name, err)
	}
	return c, nil
}

// Close shuts the default client and every cached remote client.
func (f *Factory) Close() {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, c := range f.clients {
		_ = c.Close()
	}
	if f.defClient != nil {
		_ = f.defClient.Close()
	}
}
```

Note for the test: the factory calls `NewClient`, which performs a 10s `Info` dial for runtime detection - that would make unit tests slow/flaky. Handle this the clean way: extract runtime detection so construction is lazy (probe on first use), OR make `clientForHost` injectable via a package-level var the test can stub:

```go
// newClientForHost is a var so unit tests can stub construction.
var newClientForHost = func(endpoint string, tls *TLSConfig) (*Client, error) {
	return NewClient(endpoint, tls)
}
```

and `clientForHost` calls `newClientForHost`. Prefer this stub-var; do NOT restructure runtime detection in this plan.

- [ ] **Step 3: Run**

Run: `go test ./internal/infra/docker/ -count=1` - Expected: PASS.

---

### Task 7: hosts REST API (`/api/v1/hosts`)

**Files:**
- Create: `internal/api/dto/host.go`
- Create: `internal/api/handler/host.go`
- Modify: `internal/api/openapi.go` (tag + registration)
- Modify: `internal/api/server.go` (Deps fields)
- Modify: `cmd/composerd/main.go` (build repo/service, pass into Deps)
- Test: `internal/api/handler/host_test.go`

Follow the registry_credentials pattern EXACTLY (domain + repo + service + dto + handler + tag + `register(deps.X != nil, ...)`). Read `internal/api/handler/registry.go`, `internal/api/dto/registry.go`, and the registration block at `internal/api/openapi.go:148-150` before writing.

- [ ] **Step 1: Write the failing handler test**

`internal/api/handler/host_test.go` - mirror the existing handler test setup (see `stack_test.go` / registry handler test for how they build a huma test API + fake service). Cases:

```go
// POST /api/v1/hosts as admin with valid body -> 200/201, body has id.
// POST with bad endpoint scheme -> 422.
// DELETE with stacks still assigned -> 409 (or 422) with the guard message.
// GET /api/v1/hosts as viewer -> 200 list.
// POST as viewer -> 403.
```

RBAC: list/get = viewer, create/update/delete = admin (same as registry).

- [ ] **Step 2: DTOs**

`internal/api/dto/host.go`:

```go
package dto

type DockerHostBody struct {
	Name     string `json:"name" minLength:"1" maxLength:"63" doc:"Unique host name (lowercase, dns-label-ish)"`
	Endpoint string `json:"endpoint" minLength:"1" doc:"tcp://host:2376 | tcp://host:2375 | unix:///path.sock"`
	CertDir  string `json:"cert_dir,omitempty" doc:"Directory holding ca.pem/cert.pem/key.pem for mTLS; empty = no TLS"`
}

type CreateHostInput struct {
	Body DockerHostBody
}

type UpdateHostInput struct {
	ID   int64 `path:"id"`
	Body DockerHostBody
}

type HostIDInput struct {
	ID int64 `path:"id"`
}

type DockerHostOutput struct {
	ID        int64  `json:"id"`
	Name      string `json:"name"`
	Endpoint  string `json:"endpoint"`
	CertDir   string `json:"cert_dir,omitempty"`
	TLS       bool   `json:"tls" doc:"true when cert_dir is set"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

type HostOutputBody struct {
	Host *DockerHostOutput `json:"host"`
}
type ListHostsOutputBody struct {
	Hosts []*DockerHostOutput `json:"hosts"`
}
type CreateHostOutput struct{ Body HostOutputBody }
type ListHostsOutput  struct{ Body ListHostsOutputBody }
type GetHostOutput    struct{ Body HostOutputBody }
```

(Adjust wrapper naming to match how the registry DTO wraps Body - copy its exact convention.)

- [ ] **Step 3: Handler**

`internal/api/handler/host.go`: `HostHandler{hosts *app.HostService}`, `NewHostHandler`, `Register(api huma.API)` with five `huma.Register` calls:

| OperationID | Method | Path | Role |
|---|---|---|---|
| `listHosts` | GET | `/api/v1/hosts` | viewer |
| `createHost` | POST | `/api/v1/hosts` | admin |
| `getHost` | GET | `/api/v1/hosts/{id}` | viewer |
| `updateHost` | PUT | `/api/v1/hosts/{id}` | admin |
| `deleteHost` | DELETE | `/api/v1/hosts/{id}` | admin |

Use `errsViewer` / `errsViewerNotFound` / `errsAdminMutation` from `internal/api/handler/common.go` and `serverError(ctx, err)` from `errors.go`, same as registry.go. Validation errors from `host.Validate` / unknown-host / delete-guard -> `huma.Error422UnprocessableEntity(err.Error())`.

- [ ] **Step 4: Register + wire Deps**

- `internal/api/openapi.go`: add tag `{Name: "hosts", Description: "Remote docker host management"}` next to the registries tag, and in `RegisterHumaHandlers`:

```go
	register(deps.HostService != nil, func() {
		handler.NewHostHandler(deps.HostService).Register(api)
	})
```

- `internal/api/server.go`: add `HostService *app.HostService` and `DockerFactory *docker.Factory` fields to `Deps` (the factory is used by later tasks; add it now).
- `cmd/composerd/main.go`: after the store setup, `hostRepo := store.NewHostRepo(db)` + `hostSvc := app.NewHostService(hostRepo, logger)`; pass both into `api.Deps{...}`.

- [ ] **Step 5: Run**

Run: `go test ./internal/api/... -run TestHost -v` then `go build ./... && go vet ./...`
Expected: PASS. Also `make generate` must now show the hosts endpoints in `web/src/lib/api/openapi.json` (leave the regenerated files in the worktree).

---

### Task 8: stack create flows accept `host` + summary/detail expose it

**Files:**
- Modify: `internal/api/dto/git.go` (`CreateGitStackInput` body: add `Host string`)
- Modify: `internal/api/dto/stack.go` (`CreateStackInput` body + `StackSummary` + detail output: add `Host`)
- Modify: `internal/app/git_service.go` (`CreateGitStack`: resolve host, persist, deploy via factory)
- Modify: `internal/app/stack_service.go` (same for local create; ListContainers via factory; summary mapping)
- Modify: constructor wiring in `cmd/composerd/main.go`
- Test: `internal/app/git_service_test.go` + `stack_service_test.go` (extend), `internal/api/handler/git_test.go` if present

- [ ] **Step 1: Failing tests**

```go
// git_service: CreateGitStack with host:"remote1" (fake host repo knows it)
// persists stack with HostID == remote1's id; host:"nope" -> error
// "unknown docker host"; host:"" -> HostID nil.
// stack_service List: a stack whose HostID maps to "remote1" surfaces
// Host == "remote1" in the summary mapping; nil -> "local".
```

- [ ] **Step 2: Service changes**

`git_service.go` gains two fields: `hosts *HostService`-equivalent (use the `host.Repository` interface directly to avoid an app->app dependency) and `factory *docker.Factory` (replacing the single `compose *docker.Compose` field). Constructor `NewGitService(...)` signature grows - update the one caller in main.go.

In `CreateGitStack`, after building `gitCfg` and before `s.stacks.Create`:

```go
	hostID, err := app.ResolveHostIDVia(ctx, s.hostRepo, hostName) // shared helper, see below
	if err != nil {
		return nil, err
	}
	st.HostID = hostID
```

Put the resolve helper in `internal/app/host_service.go` so git_service, stack_service and pipeline_executor share it:

```go
// ResolveHostIDVia is the free-function form of HostService.ResolveHostID
// for services that hold host.Repository directly.
func ResolveHostIDVia(ctx context.Context, repo host.Repository, name string) (*int64, error) { ... }
```

The auto-deploy block (`git_service.go:133-158`) switches from `s.compose.Up(...)` to:

```go
	compose, err := s.factory.ComposeFor(ctx, st.HostID)
	if err != nil {
		s.log.Warn("host compose resolution failed; skipping auto-deploy", ...)
	} else {
		... compose.Up(deployCtx, stackPath, gitCfg.ComposePath) ...
	}
```

`stack_service.go`: same field swap (`docker *docker.Client` + `compose *docker.Compose` -> `factory *docker.Factory`). Its three `ListContainers(ctx, name)` call sites (lines ~205, ~222, ~1057) become:

```go
	st, err := s.stacks.GetByName(ctx, name)   // already loaded in most paths - reuse
	cli, err := s.factory.ClientFor(ctx, st.HostID)
	... cli.ListContainers(ctx, name) ...
```

Summary mapping: wherever `StackSummary`-shaped output is built (handler or service - find it via `rg 'StackSummary' internal/`), resolve host names in bulk: `hostRepo.List(ctx)` once -> `map[int64]string`, then `Host = name or "local"`.

- [ ] **Step 3: DTO + handler changes**

- `dto.CreateGitStackInput` body: `Host string `json:"host,omitempty" doc:"Docker host name; empty = local default"``.
- `dto.CreateStackInput` body (local create): same field.
- `StackSummary` + stack detail output structs: `Host string `json:"host"``.
- `handler/git.go` `CreateGitStack` (line ~246): pass `input.Body.Host` through to the service call (signature change).
- `handler/stack.go` `Create` (line ~309): same.

- [ ] **Step 4: Run**

Run: `go test ./internal/app/ ./internal/api/... -count=1` + `go build ./...`
Expected: PASS, and `make generate` diff shows `host` on the stack schemas.

---

### Task 9: consumer re-plumb - pipelines, API docker handlers, events, main wiring

**Files:**
- Modify: `internal/app/pipeline_executor.go`
- Modify: `internal/api/handler/container.go`, `resources.go`, `sse.go`, `docker_exec.go`, `ws/terminal.go`
- Modify: `internal/domain/event/` container events (add `HostName`)
- Modify: `internal/infra/docker/events.go` (listener takes host name)
- Modify: `cmd/composerd/main.go` (factory construction, per-host listeners, Deps)
- Test: `internal/app/pipeline_executor_test.go` (extend), `internal/infra/docker/events_test.go` if present

- [ ] **Step 1: Pipeline executor**

Swap `compose *docker.Compose` + `docker *docker.Client` fields for `factory *docker.Factory` (+ `hostRepo` for name resolution; it already has `stacks` repo).

- `executeComposeStep` (~line 279): after resolving the stack via `e.stacks.GetByName`, get `compose, err := e.factory.ComposeFor(ctx, st.HostID)` and use it for all compose subcommands in that step.
- `executeDockerExec` (~line 354): step config gains an optional `host` key (name). Resolution order: `config["host"]` if set -> `factory.ClientForName(ctx, name)`; else default host (today's behaviour). Document the key in the comment block above the function (lines ~337-352): `{"container": "web", "cmd": [...], "host": "remote1"}`.

Tests: fake factory (build a real `docker.Factory` with stubbed `newClientForHost`, or introduce a small `DockerResolver` interface in app - PREFER the interface if the executor's tests already use fakes; match existing test style).

- [ ] **Step 2: API docker handlers get `?host=`**

All five handlers below switch their constructor from `*docker.Client` / `*docker.Compose` to `*docker.Factory`, and each method resolves at the top:

```go
	cli, err := h.factory.ClientForName(ctx, hostQueryParam) // "" = default
	if err != nil {
		return nil, huma.Error422UnprocessableEntity(err.Error())
	}
```

Sites (full worked pattern above; apply identically):

1. `internal/api/handler/container.go` - add `Host string `query:"host"`` to the input structs of List/Inspect/Start/Stop/Restart/Pause/Unpause/Logs.
2. `internal/api/handler/resources.go` - same for networks/volumes/images/events/prune inputs.
3. `internal/api/handler/sse.go` - logs/stats/stack-logs: read the `host` query param inside the SSE setup.
4. `internal/api/handler/docker_exec.go` - the allowlisted `/api/v1/docker/exec`: add `host` to input, resolve compose via `factory.ComposeForName` (add that method to Factory mirroring `ClientForName`).
5. `internal/api/ws/terminal.go` - terminal target container: `host` query param on the WS URL.

`handler/system.go` stays on the DEFAULT client (via `factory.DefaultClient()`) - system info describes the daemon composerd runs on.

- [ ] **Step 3: Events carry host identity**

- In the domain event package (`rg -n 'ContainerStateChanged' internal/domain/`): add `HostName string` to the container state + health event structs (empty = default).
- `internal/infra/docker/events.go`: `NewEventListener(client, bus)` -> `NewEventListener(client, bus, hostName string)`; `handleEvent` stamps `HostName` on every emitted event.
- SSE/websocket consumers that filter by container id now key on (host, id) where trivially possible; otherwise accept the union (documented limitation: container short-ids can collide across hosts).

- [ ] **Step 4: main.go wiring**

In `cmd/composerd/main.go`:

```go
	dockerClient, err = docker.NewClient(cfg.DockerHost, nil)   // Task 4
	...
	compose = docker.NewCompose(dockerClient.Host(), logger)
	hostRepo := store.NewHostRepo(db)
	hostSvc := app.NewHostService(hostRepo, logger)
	factory := docker.NewFactory(dockerClient, compose, hostRepo, logger)
```

- Replace the single `eventListener := docker.NewEventListener(dockerClient, bus)` with one per host:

```go
	eventListener := docker.NewEventListener(dockerClient, bus, "")
	eventListener.Start()
	defer eventListener.Stop()
	if hosts, err := hostRepo.List(ctx); err == nil {
		for _, h := range hosts {
			cli, err := factory.ClientFor(ctx, &h.ID)
			if err != nil {
				logger.Warn("skipping event listener for host", zap.String("host", h.Name), zap.Error(err))
				continue
			}
			l := docker.NewEventListener(cli, bus, h.Name)
			l.Start()
			defer l.Stop()
		}
	}
```

- `NewStackService` / `NewGitService` / `NewPipelineExecutor` get the factory (+ hostRepo where the plan says) instead of the raw client/compose.
- `selfupgrade.NewUpgradeService(upgradeRepo, factory.DefaultClient(), ...)` (all three construction sites: main.go:291, openapi.go:187-189, server.go:113-118).
- `api.Deps{...}`: `DockerClient: dockerClient` -> `DockerFactory: factory`, `Compose: compose` -> factory-based, `HostService: hostSvc`.
- Shutdown: `dockerClient.Close()` -> `factory.Close()`.

- [ ] **Step 5: Run the full gate**

Run: `go build ./... && go vet ./... && go test -race -count=1 ./internal/... ./cmd/... && go test -count=1 ./internal/conformance/...`
Expected: ALL PASS, including every pre-existing test. This is the big-bang task - if a pre-existing test referenced the old constructor signatures, update the test setup to the new signatures WITHOUT weakening assertions.

---

### Task 10: frontend - hosts card, host picker, host badge

**Files:**
- Create: `web/src/components/layout/DockerHosts.tsx`
- Modify: `web/src/pages/settings/index.astro` (mount the card)
- Modify: `web/src/components/stack/GitCloneForm.tsx` (host `<select>`)
- Modify: `web/src/components/stack/RawComposeForm.tsx` (host `<select>`)
- Modify: `web/src/components/stack/DashboardOverview.tsx` (host badge column)
- Modify: `web/e2e/smoke.spec.ts` (hosts describe block)
- Run: `make generate` (regenerate + leave `web/src/lib/api/{openapi.json,openapi.yaml,types.ts}` in the worktree - CI diffs them)

- [ ] **Step 1: regenerate API types first**

Run: `make generate`
Expected: `web/src/lib/api/openapi.json` now contains `/api/v1/hosts` endpoints and `host` on stack schemas. This is a CI gate (`git diff --exit-code` on the three generated files), so the regenerated artifacts MUST be part of the final state.

- [ ] **Step 2: `DockerHosts.tsx` settings card**

Copy the `RegistryAuthSettings.tsx` anatomy exactly (hand-written interface, `apiFetch` + refetch-after-mutation, controlled `useState` form with create/edit dual mode, `ConfirmButton` delete, `data-table` primitives, `data-testid` on every interactive element, wrapped in `ErrorBoundary`). Interface:

```tsx
interface DockerHost {
  id: number;
  name: string;
  endpoint: string;
  cert_dir?: string;
  tls: boolean;
  created_at: string;
  updated_at: string;
}
```

Form fields: Name, Endpoint (placeholder `tcp://docker-remote.example:2376`), Cert Dir (placeholder: path with ca.pem/cert.pem/key.pem, optional). Table columns: Name, Endpoint, TLS badge (`mTLS` / `plain`), Created, actions (edit/delete). Delete errors (guard: stacks still assigned) surface via the existing `error` state - apiFetch returns the huma `detail` string. testids: `hosts-form`, `hosts-submit`, `hosts-edit-${h.id}`, `hosts-delete-${h.id}`, `hosts-row-${h.id}`.

Mount in `web/src/pages/settings/index.astro` directly under `<RegistryAuthSettings client:load />`:

```astro
<DockerHosts client:load />
```

- [ ] **Step 3: host picker on create forms**

In `GitCloneForm.tsx`: add `const [hosts, setHosts] = useState<DockerHost[]>([])` + `const [host, setHost] = useState("")`; fetch `apiFetch<{ hosts: DockerHost[] }>("/api/v1/hosts")` in a `useEffect` on mount. Add a `<select>` cloned from the existing Auth Method select's markup/classes (new grid row if the 4-col row is full), default option `<option value="">local (default)</option>`, then one option per host (`value={h.name}`). testid `git-host`. Submit body gains `...(host && { host })`.

`RawComposeForm.tsx`: identical treatment (testid `raw-host`).

- [ ] **Step 4: host badge in the stack list**

`DashboardOverview.tsx`: extend `StackSummary` with `host: string`; add `"host"` to the sort `SortKey` union + accessors (`host: (s) => s.host ?? "local"`); add a `<SortHeader>` "Host" next to Source (use the same `hideOnNarrow` class as Source); body cell renders `<Badge>` with the host name, styled via the `statusColor`-adjacent convention (default host `local` gets the muted style, remotes get `bg-cp-blue/20 text-cp-blue border-cp-blue/30`).

- [ ] **Step 5: Playwright smoke additions**

In `web/e2e/smoke.spec.ts`, add a `test.describe("Docker hosts", ...)` that stubs `page.route("**/api/v1/hosts*", ...)` (list empty + list with one host) and asserts: the settings page shows the `hosts-form`; submitting with the stubbed POST shows the new row; the badge column header exists on the dashboard (dashboard stack list stub already exists in this file - extend its fulfilled JSON with `host: "local"`). Match the file's existing route-stub idiom; do not add new fixtures files.

- [ ] **Step 6: Run frontend gates**

Run: `cd web && bun run build && bun run check`
Expected: PASS. (Playwright itself runs in CI; if chromium is available locally, `bun run test` is a bonus, not a gate for this plan.)

---

### Task 11: docs

**Files:**
- Modify: `README.md`

- [ ] **Step 1: README section**

Add a "Multiple docker hosts" section: `docker_hosts` manages named remote daemons (`/api/v1/hosts` or Settings -> Docker Hosts); each has an endpoint (`tcp://...:2376` with mTLS cert dir, or plain `:2375`); stacks pin to a host at creation (`host` field; empty = the local default host composerd is configured with); `COMPOSER_DOCKER_HOST` still configures the default host; pipeline `docker_exec` steps accept an optional `host` key; self-upgrade only ever acts on the default host. Keep it GENERIC (public repo): no real hostnames/IPs/deployments.

- [ ] **Step 2: Final gate**

Run the whole loop sensor suite by hand once:
`go build ./... && go vet ./... && test -z "$(gofmt -l internal/ cmd/)" && go test -race -count=1 ./internal/... ./cmd/... && go test -count=1 ./internal/conformance/... && make generate && git diff --exit-code --quiet -- web/src/lib/api/ && cd web && bun run build`

Expected: everything green. Release mechanics (version bump, tag, image) are OUT of scope for this plan - they happen after human review.

---

## Self-review notes (plan author)

- **Spec coverage:** table+FK (T1-2), service+delete guard (T3), TLS client (T4), compose env (T5), factory (T6), REST CRUD (T7), create-flow + summary host field (T8), pipelines/API/events/main replumb (T9), UI (T10), docs (T11). Bootstrap/migration of existing stacks = automatic (host_id NULL = local). Out of scope (deliberate): multi-host-aware self-upgrade, host health dashboard, per-host container-id collision handling in SSE filters.
- **Type consistency:** `TLSConfig{CertDir}` used by T4/T5/T6; `Factory.ClientFor/ComposeFor/ClientForName/ComposeForName/DefaultClient/DefaultCompose` - ComposeForName is defined in T9 step 2 note; `ResolveHostIDVia` free function defined in T8 step 2 and used by T8/T9. Executor/API handlers referencing the factory get it via constructor/Deps as listed in T9.
- **Known mechanical-repetition areas (intentional):** T9 step 2 lists all five handler sites with one worked pattern; T10 steps 3-4 give exact insertion points rather than full file dumps - the files are large and the anchors (Auth Method select, Source column) are unambiguous.
