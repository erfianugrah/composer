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

// SessionRepo implements auth.SessionRepository using Postgres.
type SessionRepo struct {
	pool *pgxpool.Pool
}

func NewSessionRepo(pool *pgxpool.Pool) *SessionRepo {
	return &SessionRepo{pool: pool}
}

func (r *SessionRepo) Create(ctx context.Context, s *auth.Session) error {
	_, err := r.pool.Exec(ctx,
		`INSERT INTO sessions (id, user_id, role, created_at, expires_at)
		 VALUES ($1, $2, $3, $4, $5)`,
		s.ID, s.UserID, string(s.Role), s.CreatedAt, s.ExpiresAt,
	)
	if err != nil {
		return fmt.Errorf("inserting session: %w", err)
	}
	return nil
}

func (r *SessionRepo) GetByID(ctx context.Context, id string) (*auth.Session, error) {
	s := &auth.Session{}
	var role string
	err := r.pool.QueryRow(ctx,
		`SELECT id, user_id, role, created_at, expires_at
		 FROM sessions WHERE id = $1`, id,
	).Scan(&s.ID, &s.UserID, &role, &s.CreatedAt, &s.ExpiresAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("getting session: %w", err)
	}
	s.Role = auth.Role(role)
	return s, nil
}

func (r *SessionRepo) DeleteByID(ctx context.Context, id string) error {
	_, err := r.pool.Exec(ctx, `DELETE FROM sessions WHERE id = $1`, id)
	return err
}

func (r *SessionRepo) DeleteByUserID(ctx context.Context, userID string) error {
	_, err := r.pool.Exec(ctx, `DELETE FROM sessions WHERE user_id = $1`, userID)
	return err
}

func (r *SessionRepo) DeleteExpired(ctx context.Context) (int, error) {
	tag, err := r.pool.Exec(ctx, `DELETE FROM sessions WHERE expires_at < $1`, time.Now().UTC())
	if err != nil {
		return 0, fmt.Errorf("deleting expired sessions: %w", err)
	}
	return int(tag.RowsAffected()), nil
}
