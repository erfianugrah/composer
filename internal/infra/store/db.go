package store

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib" // registers "pgx" driver
	_ "modernc.org/sqlite"             // registers "sqlite" driver

	"github.com/pressly/goose/v3"
)

// DBType represents the database backend.
type DBType string

const (
	DBTypePostgres DBType = "postgres"
	DBTypeSQLite   DBType = "sqlite"
)

// DB wraps a *sql.DB with metadata about the backend type.
type DB struct {
	SQL  *sql.DB
	Type DBType
}

// New creates a database connection based on the URL.
// Empty URL or "sqlite://..." uses SQLite. "postgres://..." uses PostgreSQL.
// For SQLite with no URL, the DB file is created at dataDir/composer.db.
func New(ctx context.Context, dbURL, dataDir string) (*DB, error) {
	dbType, dsn := parseDBURL(dbURL, dataDir)

	var driverName string
	switch dbType {
	case DBTypePostgres:
		driverName = "pgx"
	case DBTypeSQLite:
		driverName = "sqlite"
		// Ensure parent directory exists
		dir := filepath.Dir(dsn)
		if dir != "" && dir != "." {
			if err := os.MkdirAll(dir, 0700); err != nil {
				return nil, fmt.Errorf("creating data directory %s: %w", dir, err)
			}
		}
		// SQLite pragmas for performance
		dsn = dsn + "?_pragma=journal_mode(wal)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(on)"
	default:
		return nil, fmt.Errorf("unsupported database type: %s", dbType)
	}

	sqlDB, err := sql.Open(driverName, dsn)
	if err != nil {
		return nil, fmt.Errorf("opening database: %w", err)
	}

	if err := sqlDB.PingContext(ctx); err != nil {
		sqlDB.Close()
		return nil, fmt.Errorf("pinging database: %w", err)
	}

	// P4: Connection pool configuration
	if dbType == DBTypeSQLite {
		sqlDB.SetMaxOpenConns(1) // SQLite serializes writes; 1 conn avoids BUSY errors
		sqlDB.SetMaxIdleConns(1)
	} else {
		sqlDB.SetMaxOpenConns(25)
		sqlDB.SetMaxIdleConns(5)
	}
	sqlDB.SetConnMaxLifetime(5 * time.Minute)

	// Run migrations
	dialect := goose.DialectPostgres
	if dbType == DBTypeSQLite {
		dialect = goose.DialectSQLite3
	}

	provider, err := goose.NewProvider(dialect, sqlDB, Migrations)
	if err != nil {
		sqlDB.Close()
		return nil, fmt.Errorf("creating goose provider: %w", err)
	}

	if _, err := provider.Up(ctx); err != nil {
		sqlDB.Close()
		return nil, fmt.Errorf("running migrations: %w", err)
	}

	return &DB{SQL: sqlDB, Type: dbType}, nil
}

// Close closes the database connection.
func (db *DB) Close() error {
	return db.SQL.Close()
}

// Tx executes fn within a database transaction.
// If fn returns an error, the transaction is rolled back.
// Otherwise, it is committed.
func (db *DB) Tx(ctx context.Context, fn func(tx *sql.Tx) error) error {
	tx, err := db.SQL.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("beginning transaction: %w", err)
	}
	if err := fn(tx); err != nil {
		tx.Rollback()
		return err
	}
	return tx.Commit()
}

// ErrNotUpdated is returned when an update/delete affected zero rows.
var ErrNotUpdated = fmt.Errorf("entity not found or not modified")

// IsPostgres returns true if using PostgreSQL.
func (db *DB) IsPostgres() bool {
	return db.Type == DBTypePostgres
}

// IsSQLite returns true if using SQLite.
func (db *DB) IsSQLite() bool {
	return db.Type == DBTypeSQLite
}

// parseDBURL determines the database type and DSN from the URL.
func parseDBURL(url, dataDir string) (DBType, string) {
	url = strings.TrimSpace(url)

	if url == "" {
		// No URL = SQLite in data directory
		return DBTypeSQLite, filepath.Join(dataDir, "composer.db")
	}

	if strings.HasPrefix(url, "sqlite://") || strings.HasPrefix(url, "sqlite:") {
		path := strings.TrimPrefix(url, "sqlite://")
		path = strings.TrimPrefix(path, "sqlite:")
		if path == "" {
			path = filepath.Join(dataDir, "composer.db")
		}
		return DBTypeSQLite, path
	}

	if strings.HasPrefix(url, "postgres://") || strings.HasPrefix(url, "postgresql://") {
		return DBTypePostgres, url
	}

	// Default: treat as Postgres DSN
	return DBTypePostgres, url
}
