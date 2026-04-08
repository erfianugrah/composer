package postgres

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/jackc/pgx/v5/stdlib" // registers "pgx" driver for database/sql
	"github.com/pressly/goose/v3"

	"github.com/erfianugrah/composer/internal/infra/store"
)

// DB holds a pgxpool for application queries.
type DB struct {
	Pool *pgxpool.Pool
}

// New creates a new Postgres connection pool and runs migrations.
func New(ctx context.Context, connURL string) (*DB, error) {
	// pgxpool for the application
	pool, err := pgxpool.New(ctx, connURL)
	if err != nil {
		return nil, fmt.Errorf("creating pgx pool: %w", err)
	}

	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("pinging postgres: %w", err)
	}

	// Run migrations via database/sql (goose requires *sql.DB)
	if err := runMigrations(connURL); err != nil {
		pool.Close()
		return nil, fmt.Errorf("running migrations: %w", err)
	}

	return &DB{Pool: pool}, nil
}

// Close closes the connection pool.
func (db *DB) Close() {
	db.Pool.Close()
}

// runMigrations applies all pending goose migrations.
func runMigrations(connURL string) error {
	sqlDB, err := sql.Open("pgx", connURL)
	if err != nil {
		return fmt.Errorf("opening sql.DB for migrations: %w", err)
	}
	defer sqlDB.Close()

	provider, err := goose.NewProvider(
		goose.DialectPostgres,
		sqlDB,
		store.Migrations,
	)
	if err != nil {
		return fmt.Errorf("creating goose provider: %w", err)
	}

	results, err := provider.Up(context.Background())
	if err != nil {
		return fmt.Errorf("applying migrations: %w", err)
	}

	for _, r := range results {
		if r.Error != nil {
			return fmt.Errorf("migration %s failed: %w", r.Source.Path, r.Error)
		}
	}

	return nil
}
