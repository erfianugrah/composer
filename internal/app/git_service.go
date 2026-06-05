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
	domreg "github.com/erfianugrah/composer/internal/domain/registry"
	"github.com/erfianugrah/composer/internal/domain/stack"
	"github.com/erfianugrah/composer/internal/infra/docker"
	"github.com/erfianugrah/composer/internal/infra/git"
	infreg "github.com/erfianugrah/composer/internal/infra/registry"
	"github.com/erfianugrah/composer/internal/infra/sops"
)

// GitService orchestrates git-backed stack operations and webhook processing.
type GitService struct {
	stacks       stack.StackRepository
	gitCfgs      stack.GitConfigRepository
	registryRepo domreg.Repository // optional; nil disables registry auth
	gitClient    *git.Client
	compose      *docker.Compose
	bus          domevent.Bus
	log          *zap.Logger
	stacksDir    string
	locks        *StackLocks // shared with StackService — prevents concurrent compose ops
}

// SetRegistryRepo wires an optional registry credentials repository.
// See StackService.SetRegistryRepo — same semantics.
func (s *GitService) SetRegistryRepo(r domreg.Repository) { s.registryRepo = r }

// withRegistryAuth materialises a DOCKER_CONFIG dir from global + per-stack
// credentials. Returns the wrapped ctx and a cleanup func.
func (s *GitService) withRegistryAuth(ctx context.Context, stackName string) (context.Context, func()) {
	noop := func() {}
	if s.registryRepo == nil {
		return ctx, noop
	}
	global, _ := s.registryRepo.ListGlobal(ctx)
	var perStack []*domreg.Credential
	if stackName != "" {
		perStack, _ = s.registryRepo.ListForStack(ctx, stackName)
	}
	merged := domreg.Resolve(global, perStack)
	if len(merged) == 0 {
		return ctx, noop
	}
	dir, cleanup, err := infreg.BuildConfigDir(merged)
	if err != nil || dir == "" {
		return ctx, noop
	}
	return docker.WithDockerConfigDir(ctx, dir), cleanup
}

func NewGitService(
	stacks stack.StackRepository,
	gitCfgs stack.GitConfigRepository,
	gitClient *git.Client,
	compose *docker.Compose,
	bus domevent.Bus,
	log *zap.Logger,
	stacksDir string,
	locks *StackLocks,
) *GitService {
	if log == nil {
		log = zap.NewNop()
	}
	return &GitService{
		stacks: stacks, gitCfgs: gitCfgs, gitClient: gitClient,
		compose: compose, bus: bus, log: log.Named("git"), stacksDir: stacksDir,
		locks: locks,
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
	s.locks.Lock(name)
	defer s.locks.Unlock(name)
	s.log.Info("auto-deploying cloned stack", zap.String("stack", name))
	if sops.IsAvailable() {
		var perStackAgeKey string
		if gitCfg.Credentials != nil {
			perStackAgeKey = gitCfg.Credentials.AgeKey
		}
		ageKey := sops.ResolveAgeKey(perStackAgeKey, s.stacksDir)
		sops.DecryptEnvFile(gitCfg.ResolveEnvPath(stackPath), ageKey)
		sops.DecryptComposeSecrets(filepath.Join(stackPath, gitCfg.ComposePath), ageKey)
	}
	deployCtx, regCleanup := s.withRegistryAuth(ctx, name)
	defer regCleanup()
	if _, err := s.compose.Up(deployCtx, stackPath, gitCfg.ComposePath); err != nil {
		s.log.Warn("auto-deploy failed (stack cloned but not running)", zap.String("stack", name), zap.Error(err))
	} else {
		sops.ReEncryptEnvFile(gitCfg.ResolveEnvPath(stackPath))
		sops.ReEncryptComposeSecrets(filepath.Join(stackPath, gitCfg.ComposePath))
		s.publishEvent(domevent.StackDeployed{Name: name, Timestamp: time.Now()})
	}

	return st, nil
}

// Sync pulls latest changes for a git-backed stack.
// Returns whether the compose file changed and the new commit SHA.
//
// Takes the per-stack lock so a Sync triggered via the API cannot race a
// Deploy / SyncAndRedeploy that has briefly decrypted SOPS-managed secrets
// to disk. Without the lock, the concurrent Pull would see the dirty
// worktree and (per go-git's silent partial-update path) advance HEAD
// without checking out the new tree -- the next clean Sync then becomes a
// no-op while compose.yaml on disk stays stale, and docker compose runs
// against the old file.
func (s *GitService) Sync(ctx context.Context, name string) (changed bool, newSHA string, err error) {
	s.locks.Lock(name)
	defer s.locks.Unlock(name)
	return s.syncLocked(ctx, name)
}

// syncLocked is the inner Sync body. Callers MUST already hold s.locks for
// `name` -- StackLocks uses a non-reentrant sync.Mutex, so the public Sync
// takes the lock and SyncAndRedeploy (which already holds it) calls this
// helper directly to avoid a self-deadlock.
func (s *GitService) syncLocked(ctx context.Context, name string) (changed bool, newSHA string, err error) {
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
		// Conflict / non-FF pulls still surface as errors and warrant the
		// dirty-state diagnostic. The bare "unstaged changes" path is now
		// handled inside git.Client.Pull (hard-reset to new HEAD), so it
		// reaches this branch only when HEAD did not advance.
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
	s.locks.Lock(name)
	defer s.locks.Unlock(name)
	s.log.Info("sync+redeploy", zap.String("stack", name))
	// Already holding the lock -- call syncLocked directly to avoid
	// re-entering the non-reentrant StackLocks mutex.
	_, _, err = s.syncLocked(ctx, name)
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
		envFile := cfg.ResolveEnvPath(st.Path)
		sops.DecryptEnvFile(envFile, ageKey)
		composePath := filepath.Join(st.Path, cfg.ComposePath)
		sops.DecryptComposeSecrets(composePath, ageKey)
		defer func() {
			sops.ReEncryptEnvFile(envFile)
			sops.ReEncryptComposeSecrets(composePath)
		}()
	}

	// Pull latest images before deploying — ensures mutable tags like :latest
	// are refreshed even when the compose file itself hasn't changed.
	deployCtx, regCleanup := s.withRegistryAuth(ctx, name)
	defer regCleanup()
	upCtx := deployCtx
	if _, pullErr := s.compose.Pull(deployCtx, st.Path, cfg.ComposePath); pullErr != nil {
		s.log.Warn("image pull failed, deploying with cached images",
			zap.String("stack", name), zap.Error(pullErr))
		// A slow registry can exhaust deployCtx's deadline during the pull
		// (context deadline exceeded). The subsequent Up would then fail
		// instantly on the dead context, defeating the cached-image fallback.
		// Detach from the expired deadline — preserving context values such as
		// the registry docker-config dir — and give Up its own budget so the
		// cached images can actually be deployed.
		if deployCtx.Err() != nil {
			freshCtx, cancel := context.WithTimeout(context.WithoutCancel(deployCtx), 5*time.Minute)
			defer cancel()
			upCtx = freshCtx
		}
	}

	_, err = s.compose.Up(upCtx, st.Path, cfg.ComposePath)
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
