package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/erfianugrah/composer/internal/domain/auth"
)

// UserRepo implements auth.UserRepository using database/sql.
type UserRepo struct {
	db *sql.DB
}

// NewUserRepo creates a new UserRepo.
func NewUserRepo(db *sql.DB) *UserRepo {
	return &UserRepo{db: db}
}

func (r *UserRepo) Create(ctx context.Context, u *auth.User) error {
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO users (id, email, password_hash, role, auth_provider, created_at, updated_at, last_login_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		u.ID, u.Email, u.PasswordHash, string(u.Role), u.AuthProvider, u.CreatedAt, u.UpdatedAt, u.LastLoginAt,
	)
	if err != nil {
		return fmt.Errorf("inserting user: %w", err)
	}
	return nil
}

func (r *UserRepo) GetByID(ctx context.Context, id string) (*auth.User, error) {
	return scanUser(r.db.QueryRowContext(ctx,
		`SELECT id, email, password_hash, role, auth_provider, created_at, updated_at, last_login_at
		 FROM users WHERE id = $1`, id))
}

func (r *UserRepo) GetByEmail(ctx context.Context, email string) (*auth.User, error) {
	return scanUser(r.db.QueryRowContext(ctx,
		`SELECT id, email, password_hash, role, auth_provider, created_at, updated_at, last_login_at
		 FROM users WHERE email = $1`, email))
}

func (r *UserRepo) List(ctx context.Context) ([]*auth.User, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, email, password_hash, role, auth_provider, created_at, updated_at, last_login_at
		 FROM users ORDER BY created_at ASC LIMIT 500`)
	if err != nil {
		return nil, fmt.Errorf("listing users: %w", err)
	}
	defer rows.Close()

	var users []*auth.User
	for rows.Next() {
		u := &auth.User{}
		var role string
		err := rows.Scan(&u.ID, &u.Email, &u.PasswordHash, &role, &u.AuthProvider, &u.CreatedAt, &u.UpdatedAt, &u.LastLoginAt)
		if err != nil {
			return nil, fmt.Errorf("scanning user row: %w", err)
		}
		u.Role = auth.Role(role)
		users = append(users, u)
	}
	return users, rows.Err()
}

func (r *UserRepo) Update(ctx context.Context, u *auth.User) error {
	result, err := r.db.ExecContext(ctx,
		`UPDATE users SET email=$2, password_hash=$3, role=$4, updated_at=$5, last_login_at=$6
		 WHERE id=$1`,
		u.ID, u.Email, u.PasswordHash, string(u.Role), u.UpdatedAt, u.LastLoginAt,
	)
	if err != nil {
		return fmt.Errorf("updating user: %w", err)
	}
	if n, _ := result.RowsAffected(); n == 0 {
		return ErrNotUpdated
	}
	return nil
}

func (r *UserRepo) Delete(ctx context.Context, id string) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM users WHERE id=$1`, id)
	if err != nil {
		return fmt.Errorf("deleting user: %w", err)
	}
	return nil
}

func (r *UserRepo) Count(ctx context.Context) (int, error) {
	var count int
	err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM users`).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("counting users: %w", err)
	}
	return count, nil
}

// scanUser reads a single user from a QueryRow result.
func scanUser(row *sql.Row) (*auth.User, error) {
	u := &auth.User{}
	var role string
	err := row.Scan(&u.ID, &u.Email, &u.PasswordHash, &role, &u.AuthProvider, &u.CreatedAt, &u.UpdatedAt, &u.LastLoginAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil // not found
	}
	if err != nil {
		return nil, fmt.Errorf("scanning user: %w", err)
	}
	u.Role = auth.Role(role)
	return u, nil
}
