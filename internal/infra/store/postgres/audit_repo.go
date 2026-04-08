package postgres

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// AuditEntry represents a single audit log entry.
type AuditEntry struct {
	ID        string
	UserID    string
	Action    string
	Resource  string
	Detail    map[string]any
	IPAddress string
	CreatedAt time.Time
}

// AuditRepo persists audit log entries.
type AuditRepo struct {
	pool *pgxpool.Pool
}

func NewAuditRepo(pool *pgxpool.Pool) *AuditRepo {
	return &AuditRepo{pool: pool}
}

// Log writes an audit entry. Fire-and-forget -- errors are swallowed.
func (r *AuditRepo) Log(ctx context.Context, entry AuditEntry) {
	r.pool.Exec(ctx,
		`INSERT INTO audit_log (id, user_id, action, resource, detail, ip_address, created_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		entry.ID, entry.UserID, entry.Action, entry.Resource, entry.Detail, entry.IPAddress, entry.CreatedAt,
	)
}

// Recent returns the most recent audit entries.
func (r *AuditRepo) Recent(ctx context.Context, limit int) ([]AuditEntry, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}

	rows, err := r.pool.Query(ctx,
		`SELECT id, user_id, action, resource, detail, ip_address, created_at
		 FROM audit_log ORDER BY created_at DESC LIMIT $1`, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var entries []AuditEntry
	for rows.Next() {
		e := AuditEntry{}
		if err := rows.Scan(&e.ID, &e.UserID, &e.Action, &e.Resource, &e.Detail, &e.IPAddress, &e.CreatedAt); err != nil {
			return nil, err
		}
		entries = append(entries, e)
	}
	return entries, rows.Err()
}
