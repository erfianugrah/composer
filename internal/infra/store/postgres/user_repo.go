package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/erfianugrah/composer/internal/domain/auth"
)

// UserRepo implements auth.UserRepository using Postgres.
type UserRepo struct {
	pool *pgxpool.Pool
}

// NewUserRepo creates a new UserRepo.
func NewUserRepo(pool *pgxpool.Pool) *UserRepo {
	return &UserRepo{pool: pool}
}

func (r *UserRepo) Create(ctx context.Context, u *auth.User) error {
	_, err := r.pool.Exec(ctx,
		`INSERT INTO users (id, email, password_hash, role, created_at, updated_at, last_login_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		u.ID, u.Email, u.PasswordHash, string(u.Role), u.CreatedAt, u.UpdatedAt, u.LastLoginAt,
	)
	if err != nil {
		return fmt.Errorf("inserting user: %w", err)
	}
	return nil
}

func (r *UserRepo) GetByID(ctx context.Context, id string) (*auth.User, error) {
	return r.scanUser(r.pool.QueryRow(ctx,
		`SELECT id, email, password_hash, role, created_at, updated_at, last_login_at
		 FROM users WHERE id = $1`, id))
}

func (r *UserRepo) GetByEmail(ctx context.Context, email string) (*auth.User, error) {
	return r.scanUser(r.pool.QueryRow(ctx,
		`SELECT id, email, password_hash, role, created_at, updated_at, last_login_at
		 FROM users WHERE email = $1`, email))
}

func (r *UserRepo) List(ctx context.Context) ([]*auth.User, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT id, email, password_hash, role, created_at, updated_at, last_login_at
		 FROM users ORDER BY created_at ASC`)
	if err != nil {
		return nil, fmt.Errorf("listing users: %w", err)
	}
	defer rows.Close()

	var users []*auth.User
	for rows.Next() {
		u, err := r.scanUserFromRows(rows)
		if err != nil {
			return nil, err
		}
		users = append(users, u)
	}
	return users, rows.Err()
}

func (r *UserRepo) Update(ctx context.Context, u *auth.User) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE users SET email=$2, password_hash=$3, role=$4, updated_at=$5, last_login_at=$6
		 WHERE id=$1`,
		u.ID, u.Email, u.PasswordHash, string(u.Role), u.UpdatedAt, u.LastLoginAt,
	)
	if err != nil {
		return fmt.Errorf("updating user: %w", err)
	}
	return nil
}

func (r *UserRepo) Delete(ctx context.Context, id string) error {
	_, err := r.pool.Exec(ctx, `DELETE FROM users WHERE id=$1`, id)
	if err != nil {
		return fmt.Errorf("deleting user: %w", err)
	}
	return nil
}

func (r *UserRepo) Count(ctx context.Context) (int, error) {
	var count int
	err := r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM users`).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("counting users: %w", err)
	}
	return count, nil
}

// scanUser reads a single user from a QueryRow result.
func (r *UserRepo) scanUser(row pgx.Row) (*auth.User, error) {
	u := &auth.User{}
	var role string
	err := row.Scan(&u.ID, &u.Email, &u.PasswordHash, &role, &u.CreatedAt, &u.UpdatedAt, &u.LastLoginAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil // not found
	}
	if err != nil {
		return nil, fmt.Errorf("scanning user: %w", err)
	}
	u.Role = auth.Role(role)
	return u, nil
}

// scanUserFromRows reads a user from a Rows iterator.
func (r *UserRepo) scanUserFromRows(rows pgx.Rows) (*auth.User, error) {
	u := &auth.User{}
	var role string
	err := rows.Scan(&u.ID, &u.Email, &u.PasswordHash, &role, &u.CreatedAt, &u.UpdatedAt, &u.LastLoginAt)
	if err != nil {
		return nil, fmt.Errorf("scanning user row: %w", err)
	}
	u.Role = auth.Role(role)
	return u, nil
}
