package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/erfianugrah/composer/internal/domain/auth"
)

// APIKeyRepo implements auth.APIKeyRepository using Postgres.
type APIKeyRepo struct {
	pool *pgxpool.Pool
}

func NewAPIKeyRepo(pool *pgxpool.Pool) *APIKeyRepo {
	return &APIKeyRepo{pool: pool}
}

func (r *APIKeyRepo) Create(ctx context.Context, k *auth.APIKey) error {
	_, err := r.pool.Exec(ctx,
		`INSERT INTO api_keys (id, name, hashed_key, role, created_by, last_used_at, expires_at, created_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		k.ID, k.Name, k.HashedKey, string(k.Role), k.CreatedBy, k.LastUsedAt, k.ExpiresAt, k.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("inserting api key: %w", err)
	}
	return nil
}

func (r *APIKeyRepo) GetByHashedKey(ctx context.Context, hashedKey string) (*auth.APIKey, error) {
	k := &auth.APIKey{}
	var role string
	err := r.pool.QueryRow(ctx,
		`SELECT id, name, hashed_key, role, created_by, last_used_at, expires_at, created_at
		 FROM api_keys WHERE hashed_key = $1`, hashedKey,
	).Scan(&k.ID, &k.Name, &k.HashedKey, &role, &k.CreatedBy, &k.LastUsedAt, &k.ExpiresAt, &k.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("getting api key: %w", err)
	}
	k.Role = auth.Role(role)
	return k, nil
}

func (r *APIKeyRepo) List(ctx context.Context) ([]*auth.APIKey, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT id, name, hashed_key, role, created_by, last_used_at, expires_at, created_at
		 FROM api_keys ORDER BY created_at ASC`)
	if err != nil {
		return nil, fmt.Errorf("listing api keys: %w", err)
	}
	defer rows.Close()

	var keys []*auth.APIKey
	for rows.Next() {
		k := &auth.APIKey{}
		var role string
		if err := rows.Scan(&k.ID, &k.Name, &k.HashedKey, &role, &k.CreatedBy, &k.LastUsedAt, &k.ExpiresAt, &k.CreatedAt); err != nil {
			return nil, fmt.Errorf("scanning api key row: %w", err)
		}
		k.Role = auth.Role(role)
		keys = append(keys, k)
	}
	return keys, rows.Err()
}

func (r *APIKeyRepo) Delete(ctx context.Context, id string) error {
	_, err := r.pool.Exec(ctx, `DELETE FROM api_keys WHERE id = $1`, id)
	return err
}

func (r *APIKeyRepo) UpdateLastUsed(ctx context.Context, id string) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE api_keys SET last_used_at = $2 WHERE id = $1`,
		id, time.Now().UTC())
	return err
}
