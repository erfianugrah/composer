package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
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

// WebhookRepo implements webhook persistence.
type WebhookRepo struct {
	pool *pgxpool.Pool
}

func NewWebhookRepo(pool *pgxpool.Pool) *WebhookRepo {
	return &WebhookRepo{pool: pool}
}

func (r *WebhookRepo) Create(ctx context.Context, w *Webhook) error {
	_, err := r.pool.Exec(ctx,
		`INSERT INTO webhooks (id, stack_name, provider, secret, branch_filter, auto_redeploy, created_by)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		w.ID, w.StackName, w.Provider, w.Secret, w.BranchFilter, w.AutoRedeploy, w.CreatedBy,
	)
	return err
}

func (r *WebhookRepo) GetByID(ctx context.Context, id string) (*Webhook, error) {
	w := &Webhook{}
	err := r.pool.QueryRow(ctx,
		`SELECT id, stack_name, provider, secret, branch_filter, auto_redeploy, created_by
		 FROM webhooks WHERE id = $1`, id,
	).Scan(&w.ID, &w.StackName, &w.Provider, &w.Secret, &w.BranchFilter, &w.AutoRedeploy, &w.CreatedBy)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("getting webhook: %w", err)
	}
	return w, nil
}

func (r *WebhookRepo) ListByStack(ctx context.Context, stackName string) ([]*Webhook, error) {
	rows, err := r.pool.Query(ctx,
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
	rows, err := r.pool.Query(ctx,
		`SELECT id, stack_name, provider, secret, branch_filter, auto_redeploy, created_by
		 FROM webhooks ORDER BY created_at ASC`,
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

func (r *WebhookRepo) Delete(ctx context.Context, id string) error {
	_, err := r.pool.Exec(ctx, `DELETE FROM webhooks WHERE id = $1`, id)
	return err
}
