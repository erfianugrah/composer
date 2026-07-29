package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/erfianugrah/composer/internal/domain/host"
)

// HostRepo implements host.Repository using database/sql.
type HostRepo struct{ db *sql.DB }

// NewHostRepo creates a HostRepo.
func NewHostRepo(db *sql.DB) *HostRepo { return &HostRepo{db: db} }

// Create inserts a new docker host. Sets h.ID on success (SQLite only;
// Postgres callers should use RETURNING or fall back to GetByName).
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

// GetByID returns the host with the given ID, or nil,nil if not found.
func (r *HostRepo) GetByID(ctx context.Context, id int64) (*host.Host, error) {
	return r.scan(r.db.QueryRowContext(ctx,
		`SELECT id, name, endpoint, cert_dir, created_at, updated_at
		 FROM docker_hosts WHERE id = $1`, id))
}

// GetByName returns the host with the given name, or nil,nil if not found.
func (r *HostRepo) GetByName(ctx context.Context, name string) (*host.Host, error) {
	return r.scan(r.db.QueryRowContext(ctx,
		`SELECT id, name, endpoint, cert_dir, created_at, updated_at
		 FROM docker_hosts WHERE name = $1`, name))
}

// List returns all docker hosts ordered by name.
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

// Update modifies endpoint, cert_dir, and name for an existing host.
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

// Delete removes a docker host by ID.
func (r *HostRepo) Delete(ctx context.Context, id int64) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM docker_hosts WHERE id=$1`, id)
	return err
}

// CountStacks returns the number of stacks referencing the given host ID.
func (r *HostRepo) CountStacks(ctx context.Context, id int64) (int, error) {
	var n int
	err := r.db.QueryRowContext(ctx,
		`SELECT count(*) FROM stacks WHERE host_id = $1`, id).Scan(&n)
	return n, err
}
