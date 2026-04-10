package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/erfianugrah/composer/internal/infra/crypto"
)

// Webhook represents a stored webhook configuration.
type Webhook struct {
	ID           string
	StackName    string
	Provider     string
	Secret       string
	BranchFilter string
	AutoRedeploy bool
	CreatedBy    string
}

// WebhookRepo implements webhook persistence using database/sql.
type WebhookRepo struct {
	db *sql.DB
}

func NewWebhookRepo(db *sql.DB) *WebhookRepo {
	return &WebhookRepo{db: db}
}

func (r *WebhookRepo) Create(ctx context.Context, w *Webhook) error {
	// Encrypt the webhook secret before storage (S17)
	encSecret, err := crypto.Encrypt(w.Secret)
	if err != nil {
		return fmt.Errorf("encrypting webhook secret: %w", err)
	}
	_, err = r.db.ExecContext(ctx,
		`INSERT INTO webhooks (id, stack_name, provider, secret, branch_filter, auto_redeploy, created_by)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		w.ID, w.StackName, w.Provider, encSecret, w.BranchFilter, w.AutoRedeploy, w.CreatedBy,
	)
	return err
}

func (r *WebhookRepo) GetByID(ctx context.Context, id string) (*Webhook, error) {
	w := &Webhook{}
	err := r.db.QueryRowContext(ctx,
		`SELECT id, stack_name, provider, secret, branch_filter, auto_redeploy, created_by
		 FROM webhooks WHERE id = $1`, id,
	).Scan(&w.ID, &w.StackName, &w.Provider, &w.Secret, &w.BranchFilter, &w.AutoRedeploy, &w.CreatedBy)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("getting webhook: %w", err)
	}
	// Decrypt the secret (backwards compatible with unencrypted data)
	w.Secret, _ = crypto.Decrypt(w.Secret)
	return w, nil
}

func (r *WebhookRepo) ListByStack(ctx context.Context, stackName string) ([]*Webhook, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, stack_name, provider, secret, branch_filter, auto_redeploy, created_by
		 FROM webhooks WHERE stack_name = $1 ORDER BY created_at ASC`, stackName,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var webhooks []*Webhook
	for rows.Next() {
		w := &Webhook{}
		if err := rows.Scan(&w.ID, &w.StackName, &w.Provider, &w.Secret, &w.BranchFilter, &w.AutoRedeploy, &w.CreatedBy); err != nil {
			return nil, err
		}
		webhooks = append(webhooks, w)
	}
	return webhooks, rows.Err()
}

func (r *WebhookRepo) ListAll(ctx context.Context) ([]*Webhook, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, stack_name, provider, secret, branch_filter, auto_redeploy, created_by
		 FROM webhooks ORDER BY created_at ASC LIMIT 500`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var webhooks []*Webhook
	for rows.Next() {
		w := &Webhook{}
		if err := rows.Scan(&w.ID, &w.StackName, &w.Provider, &w.Secret, &w.BranchFilter, &w.AutoRedeploy, &w.CreatedBy); err != nil {
			return nil, err
		}
		webhooks = append(webhooks, w)
	}
	return webhooks, rows.Err()
}

func (r *WebhookRepo) Update(ctx context.Context, w *Webhook) error {
	result, err := r.db.ExecContext(ctx,
		`UPDATE webhooks SET branch_filter=$2, auto_redeploy=$3, provider=$4 WHERE id=$1`,
		w.ID, w.BranchFilter, w.AutoRedeploy, w.Provider,
	)
	if err != nil {
		return err
	}
	if n, _ := result.RowsAffected(); n == 0 {
		return ErrNotUpdated
	}
	return nil
}

func (r *WebhookRepo) CreateDelivery(ctx context.Context, d *WebhookDelivery) error {
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO webhook_deliveries (id, webhook_id, event, branch, commit_sha, status, action, error)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		d.ID, d.WebhookID, d.Event, d.Branch, d.CommitSHA, d.Status, d.Action, d.Error,
	)
	return err
}

func (r *WebhookRepo) UpdateDeliveryStatus(ctx context.Context, id, status, action, errMsg string) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE webhook_deliveries SET status=$2, action=$3, error=$4 WHERE id=$1`,
		id, status, action, errMsg,
	)
	return err
}

func (r *WebhookRepo) Delete(ctx context.Context, id string) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM webhooks WHERE id = $1`, id)
	return err
}

// WebhookDelivery represents a webhook delivery record.
type WebhookDelivery struct {
	ID        string
	WebhookID string
	Event     string
	Branch    string
	CommitSHA string
	Status    string
	Action    string
	Error     string
	CreatedAt string
}

func (r *WebhookRepo) ListDeliveries(ctx context.Context, webhookID string, limit int) ([]WebhookDelivery, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, webhook_id, event, branch, commit_sha, status, action, error, created_at
		 FROM webhook_deliveries WHERE webhook_id = $1 ORDER BY created_at DESC LIMIT $2`, webhookID, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var deliveries []WebhookDelivery
	for rows.Next() {
		d := WebhookDelivery{}
		if err := rows.Scan(&d.ID, &d.WebhookID, &d.Event, &d.Branch, &d.CommitSHA, &d.Status, &d.Action, &d.Error, &d.CreatedAt); err != nil {
			return nil, err
		}
		deliveries = append(deliveries, d)
	}
	return deliveries, rows.Err()
}

// CleanupDeliveriesOlderThan removes webhook deliveries older than the given duration.
func (r *WebhookRepo) CleanupDeliveriesOlderThan(ctx context.Context, maxAge time.Duration) (int, error) {
	cutoff := time.Now().UTC().Add(-maxAge)
	result, err := r.db.ExecContext(ctx, `DELETE FROM webhook_deliveries WHERE created_at < $1`, cutoff)
	if err != nil {
		return 0, err
	}
	n, _ := result.RowsAffected()
	return int(n), nil
}
