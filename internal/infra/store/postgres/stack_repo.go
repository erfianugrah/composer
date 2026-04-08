package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/erfianugrah/composer/internal/domain/stack"
)

// StackRepo implements stack.StackRepository using Postgres.
type StackRepo struct {
	pool *pgxpool.Pool
}

func NewStackRepo(pool *pgxpool.Pool) *StackRepo {
	return &StackRepo{pool: pool}
}

func (r *StackRepo) Create(ctx context.Context, s *stack.Stack) error {
	_, err := r.pool.Exec(ctx,
		`INSERT INTO stacks (name, path, source, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5)`,
		s.Name, s.Path, string(s.Source), s.CreatedAt, s.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("inserting stack: %w", err)
	}
	return nil
}

func (r *StackRepo) GetByName(ctx context.Context, name string) (*stack.Stack, error) {
	s := &stack.Stack{}
	var source string
	err := r.pool.QueryRow(ctx,
		`SELECT name, path, source, created_at, updated_at
		 FROM stacks WHERE name = $1`, name,
	).Scan(&s.Name, &s.Path, &source, &s.CreatedAt, &s.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("getting stack: %w", err)
	}
	s.Source = stack.Source(source)
	s.Status = stack.StatusUnknown
	return s, nil
}

func (r *StackRepo) List(ctx context.Context) ([]*stack.Stack, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT name, path, source, created_at, updated_at
		 FROM stacks ORDER BY name ASC`)
	if err != nil {
		return nil, fmt.Errorf("listing stacks: %w", err)
	}
	defer rows.Close()

	var stacks []*stack.Stack
	for rows.Next() {
		s := &stack.Stack{}
		var source string
		if err := rows.Scan(&s.Name, &s.Path, &source, &s.CreatedAt, &s.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scanning stack row: %w", err)
		}
		s.Source = stack.Source(source)
		s.Status = stack.StatusUnknown
		stacks = append(stacks, s)
	}
	return stacks, rows.Err()
}

func (r *StackRepo) Update(ctx context.Context, s *stack.Stack) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE stacks SET path=$2, source=$3, updated_at=$4 WHERE name=$1`,
		s.Name, s.Path, string(s.Source), s.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("updating stack: %w", err)
	}
	return nil
}

func (r *StackRepo) Delete(ctx context.Context, name string) error {
	_, err := r.pool.Exec(ctx, `DELETE FROM stacks WHERE name=$1`, name)
	if err != nil {
		return fmt.Errorf("deleting stack: %w", err)
	}
	return nil
}

// GitConfigRepo implements stack.GitConfigRepository using Postgres.
type GitConfigRepo struct {
	pool *pgxpool.Pool
}

func NewGitConfigRepo(pool *pgxpool.Pool) *GitConfigRepo {
	return &GitConfigRepo{pool: pool}
}

func (r *GitConfigRepo) Upsert(ctx context.Context, stackName string, cfg *stack.GitSource) error {
	_, err := r.pool.Exec(ctx,
		`INSERT INTO stack_git_configs (stack_name, repo_url, branch, compose_path, auto_sync, auth_method, credentials, last_sync_at, last_commit, sync_status)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		 ON CONFLICT (stack_name) DO UPDATE SET
		   repo_url=$2, branch=$3, compose_path=$4, auto_sync=$5, auth_method=$6,
		   credentials=$7, last_sync_at=$8, last_commit=$9, sync_status=$10`,
		stackName, cfg.RepoURL, cfg.Branch, cfg.ComposePath, cfg.AutoSync,
		string(cfg.AuthMethod), nil, cfg.LastSyncAt, cfg.LastCommitSHA, string(cfg.SyncStatus),
	)
	if err != nil {
		return fmt.Errorf("upserting git config: %w", err)
	}
	return nil
}

func (r *GitConfigRepo) GetByStackName(ctx context.Context, stackName string) (*stack.GitSource, error) {
	cfg := &stack.GitSource{}
	var authMethod, syncStatus string
	err := r.pool.QueryRow(ctx,
		`SELECT repo_url, branch, compose_path, auto_sync, auth_method, last_sync_at, last_commit, sync_status
		 FROM stack_git_configs WHERE stack_name = $1`, stackName,
	).Scan(&cfg.RepoURL, &cfg.Branch, &cfg.ComposePath, &cfg.AutoSync,
		&authMethod, &cfg.LastSyncAt, &cfg.LastCommitSHA, &syncStatus)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("getting git config: %w", err)
	}
	cfg.AuthMethod = stack.GitAuthMethod(authMethod)
	cfg.SyncStatus = stack.GitSyncStatus(syncStatus)
	return cfg, nil
}

func (r *GitConfigRepo) Delete(ctx context.Context, stackName string) error {
	_, err := r.pool.Exec(ctx, `DELETE FROM stack_git_configs WHERE stack_name=$1`, stackName)
	return err
}

func (r *GitConfigRepo) UpdateSyncStatus(ctx context.Context, stackName string, status stack.GitSyncStatus, commitSHA string) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE stack_git_configs SET sync_status=$2, last_commit=$3, last_sync_at=NOW() WHERE stack_name=$1`,
		stackName, string(status), commitSHA,
	)
	return err
}
