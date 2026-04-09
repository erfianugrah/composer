package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/erfianugrah/composer/internal/domain/auth"
)

// SessionRepo implements auth.SessionRepository using database/sql.
type SessionRepo struct {
	db *sql.DB
}

func NewSessionRepo(db *sql.DB) *SessionRepo {
	return &SessionRepo{db: db}
}

func (r *SessionRepo) Create(ctx context.Context, s *auth.Session) error {
	// Store SHA-256 hash of token, not the token itself.
	// The plain token is in s.ID (returned to client as cookie).
	hashedID := auth.HashSessionToken(s.ID)
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO sessions (id, user_id, role, created_at, expires_at)
		 VALUES ($1, $2, $3, $4, $5)`,
		hashedID, s.UserID, string(s.Role), s.CreatedAt, s.ExpiresAt,
	)
	if err != nil {
		return fmt.Errorf("inserting session: %w", err)
	}
	return nil
}

func (r *SessionRepo) GetByID(ctx context.Context, id string) (*auth.Session, error) {
	// Hash the incoming cookie token to match against stored hash
	hashedID := auth.HashSessionToken(id)
	s := &auth.Session{}
	var role string
	err := r.db.QueryRowContext(ctx,
		`SELECT id, user_id, role, created_at, expires_at
		 FROM sessions WHERE id = $1`, hashedID,
	).Scan(&s.ID, &s.UserID, &role, &s.CreatedAt, &s.ExpiresAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("getting session: %w", err)
	}
	s.Role = auth.Role(role)
	return s, nil
}

func (r *SessionRepo) DeleteByID(ctx context.Context, id string) error {
	hashedID := auth.HashSessionToken(id)
	_, err := r.db.ExecContext(ctx, `DELETE FROM sessions WHERE id = $1`, hashedID)
	return err
}

func (r *SessionRepo) DeleteByUserID(ctx context.Context, userID string) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM sessions WHERE user_id = $1`, userID)
	return err
}

func (r *SessionRepo) DeleteExpired(ctx context.Context) (int, error) {
	result, err := r.db.ExecContext(ctx, `DELETE FROM sessions WHERE expires_at < $1`, time.Now().UTC())
	if err != nil {
		return 0, fmt.Errorf("deleting expired sessions: %w", err)
	}
	n, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("getting rows affected: %w", err)
	}
	return int(n), nil
}
