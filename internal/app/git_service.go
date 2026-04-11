package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"go.uber.org/zap"

	domevent "github.com/erfianugrah/composer/internal/domain/event"
	"github.com/erfianugrah/composer/internal/domain/stack"
	"github.com/erfianugrah/composer/internal/infra/docker"
	"github.com/erfianugrah/composer/internal/infra/git"
	"github.com/erfianugrah/composer/internal/infra/sops"
)

// GitService orchestrates git-backed stack operations and webhook processing.
type GitService struct {
	stacks    stack.StackRepository
	gitCfgs   stack.GitConfigRepository
	gitClient *git.Client
	compose   *docker.Compose
	bus       domevent.Bus
	log       *zap.Logger
	stacksDir string
}

func NewGitService(
	stacks stack.StackRepository,
	gitCfgs stack.GitConfigRepository,
	gitClient *git.Client,
	compose *docker.Compose,
	bus domevent.Bus,
	log *zap.Logger,
	stacksDir string,
) *GitService {
	if log == nil {
		log = zap.NewNop()
	}
	return &GitService{
		stacks: stacks, gitCfgs: gitCfgs, gitClient: gitClient,
		compose: compose, bus: bus, log: log.Named("git"), stacksDir: stacksDir,
	}
}

// CreateGitStack clones a git repo and creates a git-backed stack.
func (s *GitService) CreateGitStack(ctx context.Context, name string, gitCfg *stack.GitSource) (*stack.Stack, error) {
	stackPath := filepath.Join(s.stacksDir, name)
	s.log.Info("cloning git stack", zap.String("stack", name), zap.String("repo", gitCfg.RepoURL), zap.String("branch", gitCfg.Branch))

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

	// Auto-deploy after clone
	s.log.Info("auto-deploying cloned stack", zap.String("stack", name))
	if sops.IsAvailable() {
		var perStackAgeKey string
		if gitCfg.Credentials != nil {
			perStackAgeKey = gitCfg.Credentials.AgeKey
		}
		ageKey := sops.ResolveAgeKey(perStackAgeKey, s.stacksDir)
		sops.DecryptEnvFile(stackPath, ageKey)
		sops.DecryptComposeSecrets(filepath.Join(stackPath, gitCfg.ComposePath), ageKey)
	}
	if _, err := s.compose.Up(ctx, stackPath, gitCfg.ComposePath); err != nil {
		s.log.Warn("auto-deploy failed (stack cloned but not running)", zap.String("stack", name), zap.Error(err))
	} else {
		sops.ReEncryptEnvFile(stackPath)
		sops.ReEncryptComposeSecrets(filepath.Join(stackPath, gitCfg.ComposePath))
		s.publishEvent(domevent.StackDeployed{Name: name, Timestamp: time.Now()})
	}

	return st, nil
}

// Sync pulls latest changes for a git-backed stack.
// Returns whether the compose file changed and the new commit SHA.
func (s *GitService) Sync(ctx context.Context, name string) (changed bool, newSHA string, err error) {
	s.log.Info("syncing git stack", zap.String("stack", name))
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
		errMsg := err.Error()
		// If dirty working tree causes pull failure, give a clear message
		if strings.Contains(errMsg, "worktree contains unstaged changes") ||
			strings.Contains(errMsg, "uncommitted changes") ||
			strings.Contains(errMsg, "conflict") {
			s.gitCfgs.UpdateSyncStatus(ctx, name, stack.GitDirty, cfg.LastCommitSHA)
			return false, "", fmt.Errorf("sync failed: local changes conflict with remote -- discard local edits or commit them first: %w", err)
		}
		s.gitCfgs.UpdateSyncStatus(ctx, name, stack.GitSyncErr, cfg.LastCommitSHA)
		return false, "", fmt.Errorf("pulling: %w", err)
	}

	s.gitCfgs.UpdateSyncStatus(ctx, name, stack.GitSynced, newSHA)

	return changed, newSHA, nil
}

// SyncAndRedeploy syncs a git-backed stack and redeploys.
// Always pulls images and redeploys when triggered by a webhook, even if the
// compose file hasn't changed — the webhook itself signals a new image is available.
func (s *GitService) SyncAndRedeploy(ctx context.Context, name string) (action string, err error) {
	s.log.Info("sync+redeploy", zap.String("stack", name))
	_, _, err = s.Sync(ctx, name)
	if err != nil {
		s.log.Error("sync failed", zap.String("stack", name), zap.Error(err))
		return "error", err
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

	// Decrypt SOPS-encrypted secrets after sync, before deploy.
	// Re-encrypt after deploy so secrets are never left decrypted at rest.
	if sops.IsAvailable() {
		var perStackAgeKey string
		if cfg != nil && cfg.Credentials != nil {
			perStackAgeKey = cfg.Credentials.AgeKey
		}
		ageKey := sops.ResolveAgeKey(perStackAgeKey, s.stacksDir)
		sops.DecryptEnvFile(st.Path, ageKey)
		composePath := filepath.Join(st.Path, cfg.ComposePath)
		sops.DecryptComposeSecrets(composePath, ageKey)
		defer func() {
			sops.ReEncryptEnvFile(st.Path)
			sops.ReEncryptComposeSecrets(filepath.Join(st.Path, cfg.ComposePath))
		}()
	}

	// Pull latest images before deploying — ensures mutable tags like :latest
	// are refreshed even when the compose file itself hasn't changed.
	if _, pullErr := s.compose.Pull(ctx, st.Path, cfg.ComposePath); pullErr != nil {
		s.log.Warn("image pull failed, deploying with cached images",
			zap.String("stack", name), zap.Error(pullErr))
	}

	_, err = s.compose.Up(ctx, st.Path, cfg.ComposePath)
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

// WorkingDiff returns the diff between the committed and working tree version of the compose file.
func (s *GitService) WorkingDiff(ctx context.Context, name string) ([]git.DiffLine, bool, error) {
	st, err := s.stacks.GetByName(ctx, name)
	if err != nil || st == nil {
		return nil, false, ErrNotFound
	}
	if st.Source != stack.SourceGit {
		return nil, false, fmt.Errorf("stack %s is not git-backed", name)
	}

	cfg, err := s.gitCfgs.GetByStackName(ctx, name)
	if err != nil || cfg == nil {
		return nil, false, fmt.Errorf("git config not found for %s", name)
	}

	return s.gitClient.WorkingDiff(st.Path, cfg.ComposePath)
}

func (s *GitService) publishEvent(evt domevent.Event) {
	if s.bus != nil {
		s.bus.Publish(evt)
	}
}
