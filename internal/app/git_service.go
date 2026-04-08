package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	domevent "github.com/erfianugrah/composer/internal/domain/event"
	"github.com/erfianugrah/composer/internal/domain/stack"
	"github.com/erfianugrah/composer/internal/infra/docker"
	"github.com/erfianugrah/composer/internal/infra/git"
)

// GitService orchestrates git-backed stack operations and webhook processing.
type GitService struct {
	stacks    stack.StackRepository
	gitCfgs   stack.GitConfigRepository
	gitClient *git.Client
	compose   *docker.Compose
	bus       domevent.Bus
	stacksDir string
}

func NewGitService(
	stacks stack.StackRepository,
	gitCfgs stack.GitConfigRepository,
	gitClient *git.Client,
	compose *docker.Compose,
	bus domevent.Bus,
	stacksDir string,
) *GitService {
	return &GitService{
		stacks: stacks, gitCfgs: gitCfgs, gitClient: gitClient,
		compose: compose, bus: bus, stacksDir: stacksDir,
	}
}

// CreateGitStack clones a git repo and creates a git-backed stack.
func (s *GitService) CreateGitStack(ctx context.Context, name string, gitCfg *stack.GitSource) (*stack.Stack, error) {
	stackPath := filepath.Join(s.stacksDir, name)

	// Clone the repo
	if err := s.gitClient.Clone(gitCfg.RepoURL, gitCfg.Branch, stackPath, gitCfg.Credentials); err != nil {
		return nil, fmt.Errorf("cloning repo: %w", err)
	}

	// Verify compose file exists
	composePath := filepath.Join(stackPath, gitCfg.ComposePath)
	if _, err := os.Stat(composePath); err != nil {
		os.RemoveAll(stackPath)
		return nil, fmt.Errorf("compose file %s not found in repo", gitCfg.ComposePath)
	}

	// Get HEAD SHA
	sha, err := s.gitClient.HeadSHA(stackPath)
	if err != nil {
		os.RemoveAll(stackPath)
		return nil, fmt.Errorf("getting HEAD: %w", err)
	}

	// Create stack in DB
	st, err := stack.NewGitStack(name, stackPath, gitCfg)
	if err != nil {
		os.RemoveAll(stackPath)
		return nil, err
	}

	if err := s.stacks.Create(ctx, st); err != nil {
		os.RemoveAll(stackPath)
		return nil, fmt.Errorf("persisting stack: %w", err)
	}

	// Save git config with current SHA
	gitCfg.LastCommitSHA = sha
	gitCfg.SyncStatus = stack.GitSynced
	now := time.Now()
	gitCfg.LastSyncAt = &now
	if err := s.gitCfgs.Upsert(ctx, name, gitCfg); err != nil {
		// Rollback: remove the stack record since git config failed
		s.stacks.Delete(ctx, name)
		os.RemoveAll(stackPath)
		return nil, fmt.Errorf("saving git config: %w", err)
	}

	s.publishEvent(domevent.StackCreated{Name: name, Timestamp: time.Now()})

	return st, nil
}

// Sync pulls latest changes for a git-backed stack.
// Returns whether the compose file changed and the new commit SHA.
func (s *GitService) Sync(ctx context.Context, name string) (changed bool, newSHA string, err error) {
	st, err := s.stacks.GetByName(ctx, name)
	if err != nil || st == nil {
		return false, "", ErrNotFound
	}
	if st.Source != stack.SourceGit {
		return false, "", fmt.Errorf("stack %s is not git-backed", name)
	}

	cfg, err := s.gitCfgs.GetByStackName(ctx, name)
	if err != nil || cfg == nil {
		return false, "", fmt.Errorf("git config not found for %s", name)
	}

	// Update sync status
	s.gitCfgs.UpdateSyncStatus(ctx, name, stack.GitSyncing, cfg.LastCommitSHA)

	changed, newSHA, err = s.gitClient.Pull(st.Path, cfg.ComposePath, cfg.Credentials)
	if err != nil {
		s.gitCfgs.UpdateSyncStatus(ctx, name, stack.GitSyncErr, cfg.LastCommitSHA)
		return false, "", fmt.Errorf("pulling: %w", err)
	}

	s.gitCfgs.UpdateSyncStatus(ctx, name, stack.GitSynced, newSHA)

	return changed, newSHA, nil
}

// SyncAndRedeploy syncs a git-backed stack and redeploys if the compose file changed.
// This is the core GitOps flow triggered by webhooks.
func (s *GitService) SyncAndRedeploy(ctx context.Context, name string) (action string, err error) {
	changed, _, err := s.Sync(ctx, name)
	if err != nil {
		return "error", err
	}

	if !changed {
		return "synced", nil
	}

	// Get git config to check auto-redeploy setting
	cfg, _ := s.gitCfgs.GetByStackName(ctx, name)
	if cfg != nil && !cfg.AutoSync {
		return "synced_pending_manual", nil
	}

	// Redeploy
	st, _ := s.stacks.GetByName(ctx, name)
	if st == nil {
		return "error", ErrNotFound
	}

	_, err = s.compose.Up(ctx, st.Path)
	if err != nil {
		s.publishEvent(domevent.StackError{Name: name, Error: err.Error(), Timestamp: time.Now()})
		return "error", fmt.Errorf("redeploying: %w", err)
	}

	s.publishEvent(domevent.StackDeployed{Name: name, Timestamp: time.Now()})

	return "redeployed", nil
}

// GitLog returns recent commits for a git-backed stack.
func (s *GitService) GitLog(ctx context.Context, name string, limit int) ([]git.CommitInfo, error) {
	st, err := s.stacks.GetByName(ctx, name)
	if err != nil || st == nil {
		return nil, ErrNotFound
	}

	cfg, err := s.gitCfgs.GetByStackName(ctx, name)
	if err != nil || cfg == nil {
		return nil, fmt.Errorf("git config not found")
	}

	return s.gitClient.Log(st.Path, cfg.ComposePath, limit)
}

// GitStatus returns the current sync status for a git-backed stack.
func (s *GitService) GitStatus(ctx context.Context, name string) (*stack.GitSource, error) {
	cfg, err := s.gitCfgs.GetByStackName(ctx, name)
	if err != nil {
		return nil, err
	}
	if cfg == nil {
		return nil, ErrNotFound
	}
	return cfg, nil
}

// Rollback checks out a specific commit in a git-backed stack.
func (s *GitService) Rollback(ctx context.Context, name, commitSHA string) error {
	st, err := s.stacks.GetByName(ctx, name)
	if err != nil || st == nil {
		return ErrNotFound
	}
	if st.Source != stack.SourceGit {
		return fmt.Errorf("stack %s is not git-backed", name)
	}

	if err := s.gitClient.Checkout(st.Path, commitSHA); err != nil {
		return fmt.Errorf("checkout %s: %w", commitSHA, err)
	}

	// Update sync status
	s.gitCfgs.UpdateSyncStatus(ctx, name, stack.GitSynced, commitSHA)

	return nil
}

func (s *GitService) publishEvent(evt domevent.Event) {
	if s.bus != nil {
		s.bus.Publish(evt)
	}
}
