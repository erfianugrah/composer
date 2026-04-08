package stack

import "context"

// StackRepository persists and retrieves stack metadata.
// The compose file content itself lives on the filesystem.
type StackRepository interface {
	Create(ctx context.Context, s *Stack) error
	GetByName(ctx context.Context, name string) (*Stack, error)
	List(ctx context.Context) ([]*Stack, error)
	Update(ctx context.Context, s *Stack) error
	Delete(ctx context.Context, name string) error
}

// GitConfigRepository persists git configuration for git-backed stacks.
type GitConfigRepository interface {
	Upsert(ctx context.Context, stackName string, cfg *GitSource) error
	GetByStackName(ctx context.Context, stackName string) (*GitSource, error)
	Delete(ctx context.Context, stackName string) error
	UpdateSyncStatus(ctx context.Context, stackName string, status GitSyncStatus, commitSHA string) error
}
