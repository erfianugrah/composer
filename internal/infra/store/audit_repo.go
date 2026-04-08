package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"
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

// AuditRepo persists audit log entries using database/sql.
type AuditRepo struct {
	db *sql.DB
}

func NewAuditRepo(db *sql.DB) *AuditRepo {
	return &AuditRepo{db: db}
}

// Log writes an audit entry. Fire-and-forget -- errors are swallowed.
func (r *AuditRepo) Log(ctx context.Context, entry AuditEntry) {
	// Serialize detail map to JSON string for database/sql compatibility.
	var detailStr *string
	if entry.Detail != nil {
		b, err := json.Marshal(entry.Detail)
		if err == nil {
			s := string(b)
			detailStr = &s
		}
	}

	r.db.ExecContext(ctx,
		`INSERT INTO audit_log (id, user_id, action, resource, detail, ip_address, created_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		entry.ID, entry.UserID, entry.Action, entry.Resource, detailStr, entry.IPAddress, entry.CreatedAt,
	)
}

// Recent returns the most recent audit entries.
func (r *AuditRepo) Recent(ctx context.Context, limit int) ([]AuditEntry, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}

	rows, err := r.db.QueryContext(ctx,
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
		var detailStr sql.NullString
		if err := rows.Scan(&e.ID, &e.UserID, &e.Action, &e.Resource, &detailStr, &e.IPAddress, &e.CreatedAt); err != nil {
			return nil, err
		}
		if detailStr.Valid && detailStr.String != "" {
			_ = json.Unmarshal([]byte(detailStr.String), &e.Detail) // best-effort; corrupt JSON returns nil Detail
		}
		entries = append(entries, e)
	}
	return entries, rows.Err()
}
