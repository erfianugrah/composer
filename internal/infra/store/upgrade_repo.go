package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// UpgradeRow mirrors the system_upgrade singleton table.
type UpgradeRow struct {
	ID             int       `json:"id"`
	Status         string    `json:"status"` // pending, helper_running, completed, failed
	HelperID       string    `json:"helper_id,omitempty"`
	StartedBy      string    `json:"started_by,omitempty"`
	FromVersion    string    `json:"from_version"`
	TargetImage    string    `json:"target_image"`
	DeploymentType string    `json:"deployment_type"` // compose, docker_run, unknown
	ErrorMessage   string    `json:"error_message,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

// UpgradeRepo implements persistence for the singleton system_upgrade table.
type UpgradeRepo struct {
	db DBTX
}

// NewUpgradeRepo creates an UpgradeRepo backed by the given DBTX (*sql.DB or *sql.Tx).
func NewUpgradeRepo(db DBTX) *UpgradeRepo {
	return &UpgradeRepo{db: db}
}

// ErrUpgradeInFlight is returned when an upgrade is already in progress.
var ErrUpgradeInFlight = errors.New("an upgrade is already in flight")

// Upsert inserts or updates the singleton upgrade row. Only succeeds if no
// upgrade is currently in flight (pending or helper_running).
func (r *UpgradeRepo) Upsert(ctx context.Context, row *UpgradeRow) error {
	now := time.Now().UTC()

	// Check for in-flight upgrade.
	var currentStatus sql.NullString
	err := r.db.QueryRowContext(ctx,
		`SELECT status FROM system_upgrade WHERE id = 1`,
	).Scan(&currentStatus)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if currentStatus.Valid {
		status := currentStatus.String
		if status == "pending" || status == "helper_running" {
			return ErrUpgradeInFlight
		}
	}

	// Upsert: INSERT OR REPLACE for SQLite compatibility, or ON CONFLICT for Postgres.
	_, err = r.db.ExecContext(ctx, `
		INSERT INTO system_upgrade (id, status, helper_id, started_by, from_version, target_image, deployment_type, created_at, updated_at)
		VALUES (1, $1, $2, $3, $4, $5, $6, $7, $8)
		ON CONFLICT (id) DO UPDATE SET
			status = EXCLUDED.status,
			helper_id = EXCLUDED.helper_id,
			started_by = EXCLUDED.started_by,
			from_version = EXCLUDED.from_version,
			target_image = EXCLUDED.target_image,
			deployment_type = EXCLUDED.deployment_type,
			error_message = NULL,
			updated_at = EXCLUDED.updated_at
	`, row.Status, row.HelperID, row.StartedBy, row.FromVersion, row.TargetImage, row.DeploymentType, now, now)
	return err
}

// Get returns the singleton upgrade row, or nil if none exists.
func (r *UpgradeRepo) Get(ctx context.Context) (*UpgradeRow, error) {
	row := &UpgradeRow{}
	var helperID, errorMsg sql.NullString
	err := r.db.QueryRowContext(ctx,
		`SELECT id, status, helper_id, started_by, from_version, target_image, deployment_type, error_message, created_at, updated_at
		 FROM system_upgrade WHERE id = 1`,
	).Scan(&row.ID, &row.Status, &helperID, &row.StartedBy, &row.FromVersion, &row.TargetImage, &row.DeploymentType, &errorMsg, &row.CreatedAt, &row.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	row.HelperID = helperID.String
	row.ErrorMessage = errorMsg.String
	return row, nil
}

// UpdateStatus transitions the upgrade status. It only succeeds if the current
// status matches currentStatus (optimistic concurrency check).
func (r *UpgradeRepo) UpdateStatus(ctx context.Context, currentStatus, newStatus string, helperID, errorMessage string) error {
	now := time.Now().UTC()
	result, err := r.db.ExecContext(ctx,
		`UPDATE system_upgrade SET status=$1, helper_id=$2, error_message=$3, updated_at=$4
		 WHERE id=1 AND status=$5`,
		newStatus, helperID, errorMessage, now, currentStatus,
	)
	if err != nil {
		return err
	}
	if n, _ := result.RowsAffected(); n == 0 {
		return ErrNotUpdated
	}
	return nil
}

// ReconcileStaleRuns marks any pipeline runs stuck in 'running' as 'interrupted'.
// Called at boot time.
func ReconcileStaleRuns(ctx context.Context, db *sql.DB) error {
	now := time.Now().UTC()
	_, err := db.ExecContext(ctx,
		`UPDATE pipeline_runs SET status='cancelled', finished_at=$1
		 WHERE status='running'`,
		now,
	)
	return err
}
