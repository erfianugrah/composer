package store

import (
	"context"
	"database/sql"

	"github.com/pressly/goose/v3"
)

// goMigrations returns Go-coded migrations that need dialect-specific SQL.
//
// Pure-SQL migrations live in migrations/*.sql and are loaded via the embedded
// FS. Use a Go migration here only when one SQL statement isn't portable
// across SQLite + Postgres (e.g. autoincrement primary keys: SQLite uses
// INTEGER PRIMARY KEY AUTOINCREMENT, Postgres uses BIGSERIAL / IDENTITY).
func goMigrations(dbType DBType) []*goose.Migration {
	return []*goose.Migration{
		// 005: registry_credentials (per-stack + global Docker auth).
		goose.NewGoMigration(
			5,
			&goose.GoFunc{RunTx: func(ctx context.Context, tx *sql.Tx) error {
				return execAll(ctx, tx, registryCredentialsUp(dbType))
			}},
			&goose.GoFunc{RunTx: func(ctx context.Context, tx *sql.Tx) error {
				return execAll(ctx, tx, registryCredentialsDown())
			}},
		),
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
		// 009: docker_host_certs (per-host mTLS material, encrypted at rest).
		goose.NewGoMigration(
			9,
			&goose.GoFunc{RunTx: func(ctx context.Context, tx *sql.Tx) error {
				return execAll(ctx, tx, dockerHostCertsUp())
			}},
			&goose.GoFunc{RunTx: func(ctx context.Context, tx *sql.Tx) error {
				return execAll(ctx, tx, dockerHostCertsDown())
			}},
		),
	}
}

func execAll(ctx context.Context, tx *sql.Tx, stmts []string) error {
	for _, s := range stmts {
		if _, err := tx.ExecContext(ctx, s); err != nil {
			return err
		}
	}
	return nil
}

func registryCredentialsUp(dbType DBType) []string {
	// stack_name == '' marks a GLOBAL credential, applied to every stack.
	// stack_name != '' marks a per-stack override: per-stack creds win over
	// globals for the same registry. secret_enc is AES-256-GCM encrypted.
	var idCol string
	switch dbType {
	case DBTypeSQLite:
		idCol = "id INTEGER PRIMARY KEY AUTOINCREMENT"
	default: // Postgres
		idCol = "id BIGSERIAL PRIMARY KEY"
	}
	return []string{
		`CREATE TABLE IF NOT EXISTS registry_credentials (
		    ` + idCol + `,
		    registry   TEXT NOT NULL,
		    username   TEXT NOT NULL,
		    secret_enc TEXT NOT NULL,
		    email      TEXT NOT NULL DEFAULT '',
		    stack_name TEXT NOT NULL DEFAULT '',
		    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
		    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
		    UNIQUE(registry, stack_name)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_registry_credentials_stack_name
		    ON registry_credentials(stack_name)`,
	}
}

func registryCredentialsDown() []string {
	return []string{
		`DROP INDEX IF EXISTS idx_registry_credentials_stack_name`,
		`DROP TABLE IF EXISTS registry_credentials`,
	}
}

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

func dockerHostCertsUp() []string {
	// One row per host. All three PEMs are AES-256-GCM encrypted via
	// crypto.Encrypt ("enc:" prefixed); plaintext never touches the table.
	// cert_fingerprint is the sha256 hex of the client cert DER; cert_not_after
	// is RFC3339. PK is host_id so upserts are a single-statement conflict on
	// the primary key (portable SQLite/Postgres).
	return []string{
		`CREATE TABLE IF NOT EXISTS docker_host_certs (
		    host_id          INTEGER PRIMARY KEY REFERENCES docker_hosts(id) ON DELETE CASCADE,
		    ca_cert_enc      TEXT NOT NULL DEFAULT '',
		    cert_enc         TEXT NOT NULL DEFAULT '',
		    key_enc          TEXT NOT NULL DEFAULT '',
		    cert_fingerprint TEXT NOT NULL DEFAULT '',
		    cert_not_after   TEXT NOT NULL DEFAULT '',
		    updated_at       TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`,
	}
}

func dockerHostCertsDown() []string {
	return []string{
		`DROP TABLE IF EXISTS docker_host_certs`,
	}
}
