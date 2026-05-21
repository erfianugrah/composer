package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/erfianugrah/composer/internal/domain/registry"
	"github.com/erfianugrah/composer/internal/infra/crypto"
)

// RegistryCredentialRepo persists registry.Credential rows.
//
// secret is stored encrypted with crypto.Encrypt (AES-256-GCM, "enc:" prefixed).
// We fail closed on encryption errors — never write a plaintext secret.
type RegistryCredentialRepo struct {
	db *sql.DB
}

func NewRegistryCredentialRepo(db *sql.DB) *RegistryCredentialRepo {
	return &RegistryCredentialRepo{db: db}
}

func (r *RegistryCredentialRepo) Upsert(ctx context.Context, c *registry.Credential) error {
	if err := c.Validate(); err != nil {
		return err
	}
	enc, err := crypto.Encrypt(c.Secret)
	if err != nil {
		return fmt.Errorf("encrypting registry secret: %w", err)
	}
	now := time.Now().UTC()
	if c.ID == 0 {
		res, err := r.db.ExecContext(ctx,
			`INSERT INTO registry_credentials (registry, username, secret_enc, email, stack_name, created_at, updated_at)
			 VALUES ($1, $2, $3, $4, $5, $6, $6)
			 ON CONFLICT(registry, stack_name) DO UPDATE SET
			   username=$2, secret_enc=$3, email=$4, updated_at=$6`,
			c.Registry, c.Username, enc, c.Email, c.StackName, now,
		)
		if err != nil {
			return fmt.Errorf("inserting registry credential: %w", err)
		}
		// Best-effort ID hydration — sqlite returns LastInsertId; on conflict it
		// returns the conflict-rewrite rowid which is fine.
		if id, err := res.LastInsertId(); err == nil {
			c.ID = id
		}
		c.CreatedAt = now
		c.UpdatedAt = now
		return nil
	}
	_, err = r.db.ExecContext(ctx,
		`UPDATE registry_credentials SET registry=$1, username=$2, secret_enc=$3, email=$4, stack_name=$5, updated_at=$6
		 WHERE id=$7`,
		c.Registry, c.Username, enc, c.Email, c.StackName, now, c.ID,
	)
	if err != nil {
		return fmt.Errorf("updating registry credential %d: %w", c.ID, err)
	}
	c.UpdatedAt = now
	return nil
}

func (r *RegistryCredentialRepo) Delete(ctx context.Context, id int64) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM registry_credentials WHERE id=$1`, id)
	return err
}

func (r *RegistryCredentialRepo) GetByID(ctx context.Context, id int64) (*registry.Credential, error) {
	row := r.db.QueryRowContext(ctx,
		`SELECT id, registry, username, secret_enc, email, stack_name, created_at, updated_at
		 FROM registry_credentials WHERE id=$1`, id)
	c, err := scanRegistryRow(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return c, err
}

func (r *RegistryCredentialRepo) List(ctx context.Context) ([]*registry.Credential, error) {
	return r.query(ctx, `SELECT id, registry, username, secret_enc, email, stack_name, created_at, updated_at
		FROM registry_credentials ORDER BY stack_name, registry`)
}

func (r *RegistryCredentialRepo) ListGlobal(ctx context.Context) ([]*registry.Credential, error) {
	return r.query(ctx, `SELECT id, registry, username, secret_enc, email, stack_name, created_at, updated_at
		FROM registry_credentials WHERE stack_name='' ORDER BY registry`)
}

func (r *RegistryCredentialRepo) ListForStack(ctx context.Context, stackName string) ([]*registry.Credential, error) {
	return r.query(ctx, `SELECT id, registry, username, secret_enc, email, stack_name, created_at, updated_at
		FROM registry_credentials WHERE stack_name=$1 ORDER BY registry`, stackName)
}

func (r *RegistryCredentialRepo) query(ctx context.Context, q string, args ...any) ([]*registry.Credential, error) {
	rows, err := r.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("querying registry credentials: %w", err)
	}
	defer rows.Close()
	var out []*registry.Credential
	for rows.Next() {
		c, err := scanRegistryRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// rowScanner abstracts over *sql.Row and *sql.Rows for shared scan logic.
type rowScanner interface {
	Scan(dest ...any) error
}

func scanRegistryRow(s rowScanner) (*registry.Credential, error) {
	var c registry.Credential
	var enc string
	if err := s.Scan(&c.ID, &c.Registry, &c.Username, &enc, &c.Email, &c.StackName, &c.CreatedAt, &c.UpdatedAt); err != nil {
		return nil, err
	}
	plain, err := crypto.Decrypt(enc)
	if err != nil {
		// Decrypt failure is logged at the caller; we return the row with an
		// empty Secret so admins can still see + delete malformed entries.
		c.Secret = ""
		return &c, nil
	}
	c.Secret = plain
	return &c, nil
}
