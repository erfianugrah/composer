package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"time"

	"go.uber.org/zap"

	domcontainer "github.com/erfianugrah/composer/internal/domain/container"
	"github.com/erfianugrah/composer/internal/domain/event"
	"github.com/erfianugrah/composer/internal/domain/host"
	domreg "github.com/erfianugrah/composer/internal/domain/registry"
	"github.com/erfianugrah/composer/internal/domain/stack"
	"github.com/erfianugrah/composer/internal/infra/docker"
	infreg "github.com/erfianugrah/composer/internal/infra/registry"
	"github.com/erfianugrah/composer/internal/infra/sops"
	"gopkg.in/yaml.v3"
)

// StackService orchestrates stack management operations.
type StackService struct {
	stacks       stack.StackRepository
	gitCfgs      stack.GitConfigRepository
	registryRepo domreg.Repository // optional; nil disables registry auth
	hostRepo     host.Repository   // optional; nil disables host resolution
	factory      *docker.Factory   // optional per-host compose resolver
	docker       *docker.Client
	compose      *docker.Compose
	bus          event.Bus
	log          *zap.Logger
	stacksDir    string
	dataDir      string
	locks        *StackLocks // per-stack mutex to prevent concurrent compose operations (shared)
}

// composeFor resolves the Compose wrapper for a stack's host.
// nil HostID returns the default compose; host-pinned stacks MUST resolve
// or the function returns a hard error -- a silent local fallback would
// deploy to the wrong daemon.
func (s *StackService) composeFor(ctx context.Context, st *stack.Stack) (*docker.Compose, error) {
	if st.HostID == nil {
		return s.compose, nil
	}
	if s.factory == nil {
		return nil, fmt.Errorf("stack is pinned to docker host %d but no host factory is configured", *st.HostID)
	}
	c, err := s.factory.ComposeFor(ctx, st.HostID)
	if err != nil {
		return nil, fmt.Errorf("resolving compose for docker host %d: %w", *st.HostID, err)
	}
	return c, nil
}

// clientFor resolves the Docker client for a stack's host.
// nil HostID returns the default client; host-pinned stacks MUST resolve.
func (s *StackService) clientFor(ctx context.Context, st *stack.Stack) (*docker.Client, error) {
	if st.HostID == nil {
		if s.docker == nil {
			return nil, fmt.Errorf("docker client not available on default host")
		}
		return s.docker, nil
	}
	if s.factory == nil {
		return nil, fmt.Errorf("stack is pinned to docker host %d but no host factory is configured", *st.HostID)
	}
	c, err := s.factory.ClientFor(ctx, st.HostID)
	if err != nil {
		return nil, fmt.Errorf("resolving client for docker host %d: %w", *st.HostID, err)
	}
	return c, nil
}

// SetRegistryRepo wires an optional registry credentials repository. When set,
// docker compose pull/up operations get a DOCKER_CONFIG with auths resolved
// from global + per-stack rows. Pass nil to disable.
func (s *StackService) SetRegistryRepo(r domreg.Repository) { s.registryRepo = r }

// withRegistryAuth resolves registry credentials for stackName, materialises
// a tempdir DOCKER_CONFIG, and returns a child context plus a cleanup func.
// Safe to call with no registryRepo configured (returns ctx + no-op cleanup).
func (s *StackService) withRegistryAuth(ctx context.Context, stackName string) (context.Context, func()) {
	noop := func() {}
	if s.registryRepo == nil {
		return ctx, noop
	}
	global, err := s.registryRepo.ListGlobal(ctx)
	if err != nil {
		s.log.Warn("registry: list global creds failed", zap.Error(err))
	}
	var perStack []*domreg.Credential
	if stackName != "" {
		if rows, err := s.registryRepo.ListForStack(ctx, stackName); err != nil {
			s.log.Warn("registry: list per-stack creds failed", zap.String("stack", stackName), zap.Error(err))
		} else {
			perStack = rows
		}
	}
	merged := domreg.Resolve(global, perStack)
	if len(merged) == 0 {
		return ctx, noop
	}
	dir, cleanup, err := infreg.BuildConfigDir(merged)
	if err != nil {
		s.log.Error("registry: build DOCKER_CONFIG failed", zap.Error(err))
		return ctx, noop
	}
	if dir == "" {
		return ctx, cleanup
	}
	s.log.Debug("registry: DOCKER_CONFIG materialised",
		zap.String("stack", stackName),
		zap.Int("registries", len(merged)),
	)
	return docker.WithDockerConfigDir(ctx, dir), cleanup
}

// NewStackService creates a new StackService.
func NewStackService(
	stacks stack.StackRepository,
	gitCfgs stack.GitConfigRepository,
	dockerClient *docker.Client,
	compose *docker.Compose,
	bus event.Bus,
	log *zap.Logger,
	stacksDir string,
	dataDir string,
	locks *StackLocks,
	hostRepo host.Repository,
	factory *docker.Factory,
) *StackService {
	if log == nil {
		log = zap.NewNop()
	}
	return &StackService{
		stacks:    stacks,
		gitCfgs:   gitCfgs,
		docker:    dockerClient,
		compose:   compose,
		bus:       bus,
		log:       log.Named("stacks"),
		stacksDir: stacksDir,
		dataDir:   dataDir,
		locks:     locks,
		hostRepo:  hostRepo,
		factory:   factory,
	}
}

// Create creates a new local stack with the given compose content.
func (s *StackService) Create(ctx context.Context, name, composeContent string, hostID *int64) (*stack.Stack, error) {
	// Resolve HostID: nil = default (local), non-nil = remote docker_hosts row.
	s.locks.Lock(name)
	defer s.locks.Unlock(name)

	stackPath := filepath.Join(s.stacksDir, name)

	st, err := stack.NewStackWithHost(name, stackPath, stack.SourceLocal, hostID)
	if err != nil {
		return nil, err
	}
	st.ComposeContent = composeContent

	if err := os.MkdirAll(stackPath, 0755); err != nil {
		return nil, fmt.Errorf("creating stack directory: %w", err)
	}
	composePath := filepath.Join(stackPath, "compose.yaml")
	if err := os.WriteFile(composePath, []byte(composeContent), 0600); err != nil {
		return nil, fmt.Errorf("writing compose file: %w", err)
	}

	// Validate compose syntax before persisting to DB
	if s.compose != nil {
		cf, cErr := s.composeFor(ctx, st)
		if cErr != nil {
			os.RemoveAll(stackPath)
			return nil, cErr
		}
		if _, err := cf.Validate(ctx, stackPath); err != nil {
			os.RemoveAll(stackPath)
			return nil, fmt.Errorf("invalid compose file: %w", err)
		}
	}

	if err := s.stacks.Create(ctx, st); err != nil {
		os.RemoveAll(stackPath)
		return nil, fmt.Errorf("persisting stack: %w", err)
	}

	s.publishEvent(event.StackCreated{Name: name, Timestamp: time.Now()})

	// Auto-deploy after create
	s.log.Info("auto-deploying new stack", zap.String("stack", name))
	cf := s.resolveComposeFile(ctx, name)
	// Re-encrypt on ANY return path (success OR deploy failure) so secrets are
	// never left decrypted at rest. No-op when nothing was decrypted.
	defer s.reEncryptSopsSecretsCtx(ctx, name, st.Path)
	if err := s.decryptSopsSecrets(ctx, name, st.Path); err != nil {
		// A deploy here would run compose against ciphertext: abort the whole
		// create and roll back the persisted row.
		os.RemoveAll(stackPath)
		s.stacks.Delete(ctx, name)
		return nil, fmt.Errorf("decrypting SOPS secrets: %w", err)
	}
	deployCtx, regCleanup := s.withRegistryAuth(ctx, name)
	defer regCleanup()
	composeWrapper, cErr := s.composeFor(ctx, st)
	if cErr != nil {
		s.log.Warn("auto-deploy failed (stack created but not running)", zap.String("stack", name), zap.Error(cErr))
	} else if _, err := composeWrapper.Up(deployCtx, st.Path, cf); err != nil {
		s.log.Warn("auto-deploy failed (stack created but not running)", zap.String("stack", name), zap.Error(err))
		// Don't fail the create -- stack is saved, user can deploy manually
	} else {
		s.publishEvent(event.StackDeployed{Name: name, Timestamp: time.Now()})
	}

	return st, nil
}

// Get retrieves a stack by name with containers and compose content.
func (s *StackService) Get(ctx context.Context, name string) (*stack.Stack, error) {
	st, err := s.stacks.GetByName(ctx, name)
	if err != nil {
		return nil, err
	}
	if st == nil {
		return nil, ErrNotFound
	}

	// Read compose content -- try git config's compose path first, then common names
	composeContent := s.composeContentFor(ctx, st)
	st.ComposeContent = composeContent

	if st.Source == stack.SourceGit && st.GitConfig == nil {
		cfg, err := s.gitCfgs.GetByStackName(ctx, name)
		if err == nil && cfg != nil {
			st.GitConfig = cfg
		}
	}

	cl, err := s.clientFor(ctx, st)
	if err != nil {
		st.Status = stack.StatusUnknown
	} else {
		containers, cErr := cl.ListContainers(ctx, name)
		if cErr != nil {
			st.Status = stack.StatusUnknown
		} else {
			st.Status = deriveStackStatus(containers, composeOneShotServices(composeContent))
		}
	}

	return st, nil
}

// List returns all stacks with runtime status.
// P3: Uses a single Docker API call to list ALL containers, then groups by stack.
func (s *StackService) List(ctx context.Context) ([]*stack.Stack, error) {
	// DB rows only. Live container status is maintained by the background
	// StatusRefresher; deriving it here put a docker fan-out back on the
	// request path (the dead-host stall this method used to cause).
	stacks, err := s.stacks.List(ctx)
	if err != nil {
		return nil, err
	}
	return stacks, nil
}

// Update updates compose content. Writes to disk + DB.
// For git-backed stacks, this creates local changes that diverge from HEAD.
// The sync status is marked "dirty" to warn the user that the next git sync
// will overwrite these local edits unless they are committed and pushed.
func (s *StackService) Update(ctx context.Context, name, composeContent string) (*stack.Stack, error) {
	s.locks.Lock(name)
	defer s.locks.Unlock(name)

	st, err := s.stacks.GetByName(ctx, name)
	if err != nil {
		return nil, err
	}
	if st == nil {
		return nil, ErrNotFound
	}

	st.UpdateCompose(composeContent)

	composePath := filepath.Join(st.Path, "compose.yaml")
	// Save old content for rollback in case DB update fails
	oldContent, _ := os.ReadFile(composePath)

	if err := os.WriteFile(composePath, []byte(composeContent), 0600); err != nil {
		return nil, fmt.Errorf("writing compose file: %w", err)
	}

	if err := s.stacks.Update(ctx, st); err != nil {
		// Rollback: restore old file content
		if oldContent != nil {
			os.WriteFile(composePath, oldContent, 0600)
		}
		return nil, err
	}

	// If git-backed, mark as dirty so the UI can warn the user
	if st.Source == stack.SourceGit {
		cfg, err := s.gitCfgs.GetByStackName(ctx, name)
		if err == nil && cfg != nil {
			s.gitCfgs.UpdateSyncStatus(ctx, name, stack.GitDirty, cfg.LastCommitSHA)
		}
	}

	s.publishEvent(event.StackUpdated{Name: name, Timestamp: time.Now()})

	return st, nil
}

// Delete removes a stack.
func (s *StackService) Delete(ctx context.Context, name string, removeVolumes bool) error {
	s.locks.Lock(name)
	defer s.locks.Unlock(name)

	st, err := s.stacks.GetByName(ctx, name)
	if err != nil {
		return err
	}
	if st == nil {
		return ErrNotFound
	}

	// Stop containers first (best effort)
	cf := s.resolveComposeFile(ctx, name)
	if cw, cErr := s.composeFor(ctx, st); cErr == nil {
		cw.Down(ctx, st.Path, cf, removeVolumes)
	}

	if err := s.stacks.Delete(ctx, name); err != nil {
		return err
	}

	os.RemoveAll(st.Path)

	s.publishEvent(event.StackDeleted{Name: name, Timestamp: time.Now()})

	// Clean up the per-stack lock to prevent unbounded growth
	s.locks.Delete(name)

	return nil
}

// Deploy runs docker compose up.
func (s *StackService) Deploy(ctx context.Context, name string) (*docker.ComposeResult, error) {
	s.locks.Lock(name)
	defer s.locks.Unlock(name)

	st, err := s.stacks.GetByName(ctx, name)
	if err != nil {
		return nil, err
	}
	if st == nil {
		return nil, ErrNotFound
	}

	cf := s.resolveComposeFile(ctx, name)
	s.log.Info("deploying stack", zap.String("stack", name), zap.String("path", st.Path), zap.String("compose_file", cf))
	// Re-encrypt before the failure return too, so a partial decrypt is not
	// left plaintext at rest.
	defer s.reEncryptSopsSecretsCtx(ctx, name, st.Path)
	if err := s.decryptSopsSecrets(ctx, name, st.Path); err != nil {
		s.log.Error("deploy aborted", zap.String("stack", name), zap.Error(err))
		s.publishEvent(event.StackError{Name: name, Error: err.Error(), Timestamp: time.Now()})
		return nil, err
	}
	deployCtx, regCleanup := s.withRegistryAuth(ctx, name)
	defer regCleanup()

	cw, cErr := s.composeFor(ctx, st)
	if cErr != nil {
		s.log.Error("deploy failed", zap.String("stack", name), zap.Error(cErr))
		s.publishEvent(event.StackError{Name: name, Error: cErr.Error(), Timestamp: time.Now()})
		return nil, cErr
	}
	result, err := cw.Up(deployCtx, st.Path, cf)
	if err != nil {
		s.log.Error("deploy failed", zap.String("stack", name), zap.Error(err))
		s.publishEvent(event.StackError{Name: name, Error: err.Error(), Timestamp: time.Now()})
		return result, err
	}

	s.log.Info("deploy completed", zap.String("stack", name))
	s.publishEvent(event.StackDeployed{Name: name, Timestamp: time.Now()})
	return result, nil
}

// BuildAndDeploy runs docker compose up --build (builds Dockerfiles then starts).
func (s *StackService) BuildAndDeploy(ctx context.Context, name string) (*docker.ComposeResult, error) {
	s.locks.Lock(name)
	defer s.locks.Unlock(name)

	st, err := s.stacks.GetByName(ctx, name)
	if err != nil {
		return nil, err
	}
	if st == nil {
		return nil, ErrNotFound
	}

	cf := s.resolveComposeFile(ctx, name)
	s.log.Info("build+deploy stack", zap.String("stack", name), zap.String("path", st.Path), zap.String("compose_file", cf))
	// Re-encrypt before the failure return too, so a partial decrypt is not
	// left plaintext at rest.
	defer s.reEncryptSopsSecretsCtx(ctx, name, st.Path)
	if err := s.decryptSopsSecrets(ctx, name, st.Path); err != nil {
		s.log.Error("build+deploy aborted", zap.String("stack", name), zap.Error(err))
		s.publishEvent(event.StackError{Name: name, Error: err.Error(), Timestamp: time.Now()})
		return nil, err
	}
	deployCtx, regCleanup := s.withRegistryAuth(ctx, name)
	defer regCleanup()

	cw, cErr := s.composeFor(ctx, st)
	if cErr != nil {
		s.publishEvent(event.StackError{Name: name, Error: cErr.Error(), Timestamp: time.Now()})
		return nil, cErr
	}
	result, err := cw.BuildAndUp(deployCtx, st.Path, cf)
	if err != nil {
		s.publishEvent(event.StackError{Name: name, Error: err.Error(), Timestamp: time.Now()})
		return result, err
	}

	s.publishEvent(event.StackDeployed{Name: name, Timestamp: time.Now()})
	return result, nil
}

// Stop runs docker compose down.
func (s *StackService) Stop(ctx context.Context, name string) (*docker.ComposeResult, error) {
	s.locks.Lock(name)
	defer s.locks.Unlock(name)

	st, err := s.stacks.GetByName(ctx, name)
	if err != nil {
		return nil, err
	}
	if st == nil {
		return nil, ErrNotFound
	}

	cf := s.resolveComposeFile(ctx, name)
	s.log.Info("stopping stack", zap.String("stack", name))
	cw, cErr := s.composeFor(ctx, st)
	if cErr != nil {
		s.log.Error("stop failed", zap.String("stack", name), zap.Error(cErr))
		return nil, cErr
	}
	result, err := cw.Down(ctx, st.Path, cf, false)
	if err != nil {
		s.log.Error("stop failed", zap.String("stack", name), zap.Error(err))
		return result, err
	}

	s.log.Info("stop completed", zap.String("stack", name))
	s.publishEvent(event.StackStopped{Name: name, Timestamp: time.Now()})
	return result, nil
}

// Restart runs docker compose restart.
func (s *StackService) Restart(ctx context.Context, name string) (*docker.ComposeResult, error) {
	s.locks.Lock(name)
	defer s.locks.Unlock(name)

	st, err := s.stacks.GetByName(ctx, name)
	if err != nil {
		return nil, err
	}
	if st == nil {
		return nil, ErrNotFound
	}

	cf := s.resolveComposeFile(ctx, name)
	s.log.Info("restarting stack", zap.String("stack", name))
	cw, cErr := s.composeFor(ctx, st)
	if cErr != nil {
		s.log.Error("restart failed", zap.String("stack", name), zap.Error(cErr))
		return nil, cErr
	}
	result, err := cw.Restart(ctx, st.Path, cf)
	if err == nil {
		s.log.Info("restart completed", zap.String("stack", name))
		s.publishEvent(event.StackDeployed{Name: name, Timestamp: time.Now()})
	} else {
		s.log.Error("restart failed", zap.String("stack", name), zap.Error(err))
	}
	return result, err
}

// Pull runs docker compose pull.
func (s *StackService) Pull(ctx context.Context, name string) (*docker.ComposeResult, error) {
	s.locks.Lock(name)
	defer s.locks.Unlock(name)

	st, err := s.stacks.GetByName(ctx, name)
	if err != nil {
		return nil, err
	}
	if st == nil {
		return nil, ErrNotFound
	}

	cf := s.resolveComposeFile(ctx, name)
	s.log.Info("pulling images", zap.String("stack", name))
	// Decrypt SOPS-managed .env/compose before pull so ${VAR} image-tag
	// references (e.g. image: repo:${TAG}) interpolate to plaintext instead of
	// the literal ENC[...] ciphertext. Re-encrypt on exit. Mirrors Deploy.
	defer s.reEncryptSopsSecretsCtx(ctx, name, st.Path)
	if err := s.decryptSopsSecrets(ctx, name, st.Path); err != nil {
		s.log.Error("pull aborted", zap.String("stack", name), zap.Error(err))
		s.publishEvent(event.StackError{Name: name, Error: err.Error(), Timestamp: time.Now()})
		return nil, err
	}
	pullCtx, regCleanup := s.withRegistryAuth(ctx, name)
	defer regCleanup()
	cw, cErr := s.composeFor(ctx, st)
	if cErr != nil {
		s.log.Error("pull failed", zap.String("stack", name), zap.Error(cErr))
		return nil, cErr
	}
	result, err := cw.Pull(pullCtx, st.Path, cf)
	if err == nil {
		s.log.Info("pull completed", zap.String("stack", name))
		s.publishEvent(event.StackUpdated{Name: name, Timestamp: time.Now()})
	} else {
		s.log.Error("pull failed", zap.String("stack", name), zap.Error(err))
	}
	return result, err
}

// Config runs `docker compose config --no-interpolate` and returns the
// structurally normalized YAML with ${VAR} references intact. See
// docker.Compose.Config -- the --no-interpolate flag prevents the /diff
// endpoint from leaking plaintext .env values to viewers.
func (s *StackService) Config(ctx context.Context, name string) (*docker.ComposeResult, error) {
	st, err := s.stacks.GetByName(ctx, name)
	if err != nil {
		return nil, err
	}
	if st == nil {
		return nil, ErrNotFound
	}
	cw, cErr := s.composeFor(ctx, st)
	if cErr != nil {
		return nil, cErr
	}
	return cw.Config(ctx, st.Path)
}

// Validate runs docker compose config to validate the compose syntax.
func (s *StackService) Validate(ctx context.Context, name string) (*docker.ComposeResult, error) {
	st, err := s.stacks.GetByName(ctx, name)
	if err != nil {
		return nil, err
	}
	if st == nil {
		return nil, ErrNotFound
	}
	cw, cErr := s.composeFor(ctx, st)
	if cErr != nil {
		return nil, cErr
	}
	return cw.Validate(ctx, st.Path)
}

// ImportResult holds the outcome of an import operation.
type ImportResult struct {
	Imported []string `json:"imported"`
	Skipped  []string `json:"skipped"`
	Errors   []string `json:"errors"`
}

// ImportFromDir scans a source directory for compose stacks and imports them.
// Each subdirectory containing a compose.yaml or docker-compose.yml is treated as a stack.
// Files are copied to Composer's stacks directory and registered in the DB.
// Already-existing stacks (by name) are skipped.
func (s *StackService) ImportFromDir(ctx context.Context, sourceDir string) (*ImportResult, error) {
	// Validate import path (S12) -- block sensitive system directories
	absDir, err := filepath.Abs(sourceDir)
	if err != nil {
		return nil, fmt.Errorf("resolving import path: %w", err)
	}
	// Resolve symlinks to prevent bypassing the blocklist via symlink → /etc
	resolved, err := filepath.EvalSymlinks(absDir)
	if err != nil {
		return nil, fmt.Errorf("resolving symlinks in import path: %w", err)
	}
	for _, blocked := range []string{"/etc", "/var/run", "/proc", "/sys", "/dev", "/root", "/boot"} {
		if strings.HasPrefix(resolved, blocked) || strings.HasPrefix(absDir, blocked) {
			return nil, fmt.Errorf("import from %s is not permitted", blocked)
		}
	}

	entries, err := os.ReadDir(sourceDir)
	if err != nil {
		return nil, fmt.Errorf("reading source directory: %w", err)
	}

	result := &ImportResult{}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()

		// Find compose file
		composeFile := ""
		for _, candidate := range []string{"compose.yaml", "compose.yml", "docker-compose.yaml", "docker-compose.yml"} {
			path := filepath.Join(sourceDir, name, candidate)
			if _, err := os.Stat(path); err == nil {
				composeFile = path
				break
			}
		}
		if composeFile == "" {
			continue // not a stack directory
		}

		// Check if already exists
		existing, _ := s.stacks.GetByName(ctx, name)
		if existing != nil {
			result.Skipped = append(result.Skipped, name)
			continue
		}

		// Read compose content
		content, err := os.ReadFile(composeFile)
		if err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("%s: %v", name, err))
			continue
		}

		// Copy entire stack directory to Composer's stacks dir
		destDir := filepath.Join(s.stacksDir, name)
		if err := copyDir(filepath.Join(sourceDir, name), destDir); err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("%s: copy failed: %v", name, err))
			continue
		}

		// Register in DB
		st, err := stack.NewStack(name, destDir, stack.SourceLocal)
		if err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("%s: %v", name, err))
			continue
		}
		st.ComposeContent = string(content)
		if err := s.stacks.Create(ctx, st); err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("%s: db error: %v", name, err))
			continue
		}

		result.Imported = append(result.Imported, name)
	}

	return result, nil
}

// ConvertToGit converts a local stack to a git-backed stack by initializing
// a git repo, committing the compose file, and optionally pushing to a remote.
func (s *StackService) ConvertToGit(ctx context.Context, name string, repoURL, branch, composePath, envPath string, creds *stack.GitCredentials) error {
	s.locks.Lock(name)
	defer s.locks.Unlock(name)

	st, err := s.stacks.GetByName(ctx, name)
	if err != nil || st == nil {
		return ErrNotFound
	}
	if st.Source == stack.SourceGit {
		return fmt.Errorf("stack %s is already git-backed", name)
	}

	// Update source type in DB
	st.Source = stack.SourceGit
	st.UpdatedAt = time.Now().UTC()
	if err := s.stacks.Update(ctx, st); err != nil {
		return fmt.Errorf("updating stack source: %w", err)
	}

	// Create git config
	if composePath == "" {
		composePath = "compose.yaml"
	}
	gitCfg := &stack.GitSource{
		RepoURL:     repoURL,
		Branch:      branch,
		ComposePath: composePath,
		EnvPath:     envPath,
		AutoSync:    true,
		AuthMethod:  stack.GitAuthNone,
		SyncStatus:  stack.GitSynced,
		Credentials: creds,
	}
	if creds != nil && creds.Token != "" {
		gitCfg.AuthMethod = stack.GitAuthToken
	} else if creds != nil && creds.SSHKey != "" {
		gitCfg.AuthMethod = stack.GitAuthSSH
	} else if creds != nil && creds.Username != "" {
		gitCfg.AuthMethod = stack.GitAuthBasic
	}

	now := time.Now()
	gitCfg.LastSyncAt = &now

	return s.gitCfgs.Upsert(ctx, name, gitCfg)
}

// ConvertToLocal detaches a git-backed stack from its git repo,
// keeping the compose file on disk. The git config is deleted.
func (s *StackService) ConvertToLocal(ctx context.Context, name string) error {
	s.locks.Lock(name)
	defer s.locks.Unlock(name)

	st, err := s.stacks.GetByName(ctx, name)
	if err != nil || st == nil {
		return ErrNotFound
	}
	if st.Source == stack.SourceLocal {
		return fmt.Errorf("stack %s is already local", name)
	}

	// Update source type first (crash between ops leaves stack in local state = acceptable)
	st.Source = stack.SourceLocal
	st.UpdatedAt = time.Now().UTC()
	if err := s.stacks.Update(ctx, st); err != nil {
		return fmt.Errorf("updating stack source: %w", err)
	}

	// Delete git config
	if err := s.gitCfgs.Delete(ctx, name); err != nil {
		return fmt.Errorf("deleting git config: %w", err)
	}

	// Remove .git directory but keep compose file
	gitDir := filepath.Join(st.Path, ".git")
	os.RemoveAll(gitDir)

	return nil
}

// copyDir recursively copies a directory tree.
func copyDir(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		relPath, _ := filepath.Rel(src, path)
		destPath := filepath.Join(dst, relPath)

		if info.IsDir() {
			return os.MkdirAll(destPath, info.Mode())
		}

		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(destPath, data, info.Mode())
	})
}

// ResolvedCreds describes where each credential type comes from.
type ResolvedCreds struct {
	SSHSource   string // "per-stack: inline PEM", "per-stack: /path/to/key", "global: /home/composer/.ssh/id_x", "none"
	TokenSource string // "per-stack", "global", "none"
	AgeSource   string // "per-stack", "global: SOPS_AGE_KEYS env", "global: data dir", "none"
}

// ResolveCredentials returns the per-stack credentials and the resolved fallback chain.
func (s *StackService) ResolveCredentials(ctx context.Context, name string) (*stack.GitCredentials, string, ResolvedCreds, error) {
	st, err := s.stacks.GetByName(ctx, name)
	if err != nil || st == nil {
		return nil, "", ResolvedCreds{}, ErrNotFound
	}

	var creds *stack.GitCredentials
	authMethod := "none"
	cfg, err := s.gitCfgs.GetByStackName(ctx, name)
	if err == nil && cfg != nil {
		creds = cfg.Credentials
		authMethod = string(cfg.AuthMethod)
	}

	resolved := ResolvedCreds{SSHSource: "none", TokenSource: "none", AgeSource: "none"}

	// SSH resolution
	if creds != nil && creds.SSHKeyFile != "" {
		resolved.SSHSource = "per-stack: " + creds.SSHKeyFile
	} else if creds != nil && creds.SSHKey != "" {
		resolved.SSHSource = "per-stack: inline PEM"
	} else {
		// Scan global SSH keys
		for _, dir := range []string{"/home/composer/.ssh"} {
			entries, _ := os.ReadDir(dir)
			for _, e := range entries {
				n := e.Name()
				if e.IsDir() || n == "known_hosts" || n == "config" || n == "authorized_keys" || strings.HasSuffix(n, ".pub") {
					continue
				}
				resolved.SSHSource = "global: " + filepath.Join(dir, n)
				break // first key found
			}
			if resolved.SSHSource != "none" {
				break
			}
		}
	}

	// Token resolution
	if creds != nil && creds.Token != "" {
		resolved.TokenSource = "per-stack"
	} else {
		tokenPath := filepath.Join(s.dataDir, "git-token")
		if data, err := os.ReadFile(tokenPath); err == nil && len(data) > 0 {
			resolved.TokenSource = "global: data dir"
		}
	}

	// Age key resolution
	if creds != nil && creds.AgeKey != "" {
		resolved.AgeSource = "per-stack"
	} else {
		ageKey := sops.LoadGlobalAgeKey(s.dataDir)
		if ageKey != "" {
			// Detect source
			src := "global"
			if os.Getenv("COMPOSER_SOPS_AGE_KEY") != "" {
				src = "global: COMPOSER_SOPS_AGE_KEY env"
			} else if os.Getenv("SOPS_AGE_KEY") != "" {
				src = "global: SOPS_AGE_KEY env"
			} else if os.Getenv("SOPS_AGE_KEYS") != "" {
				src = "global: SOPS_AGE_KEYS env"
			} else {
				src = "global: data dir"
			}
			resolved.AgeSource = src
		}
	}

	return creds, authMethod, resolved, nil
}

// UpdateCredentials updates the per-stack credential overrides in the git config.
func (s *StackService) UpdateCredentials(ctx context.Context, name string, creds *stack.GitCredentials) error {
	cfg, err := s.gitCfgs.GetByStackName(ctx, name)
	if err != nil || cfg == nil {
		return ErrNotFound
	}
	cfg.Credentials = creds

	// Update auth method based on what's set
	if creds == nil {
		cfg.AuthMethod = stack.GitAuthNone
	} else if creds.Token != "" {
		cfg.AuthMethod = stack.GitAuthToken
	} else if creds.SSHKeyFile != "" {
		cfg.AuthMethod = stack.GitAuthSSHFile
	} else if creds.SSHKey != "" {
		cfg.AuthMethod = stack.GitAuthSSH
	} else if creds.Username != "" {
		cfg.AuthMethod = stack.GitAuthBasic
	} else {
		cfg.AuthMethod = stack.GitAuthNone
	}

	return s.gitCfgs.Upsert(ctx, name, cfg)
}

// ClearCredentialField clears a single per-stack credential field without touching others.
// Valid fields: "token", "ssh_key", "ssh_key_file", "age_key", "username", "password".
func (s *StackService) ClearCredentialField(ctx context.Context, name string, field string) error {
	cfg, err := s.gitCfgs.GetByStackName(ctx, name)
	if err != nil || cfg == nil {
		return ErrNotFound
	}
	if cfg.Credentials == nil {
		return nil // nothing to clear
	}

	switch field {
	case "token":
		cfg.Credentials.Token = ""
	case "ssh_key":
		cfg.Credentials.SSHKey = ""
		cfg.Credentials.SSHKeyPassphrase = ""
	case "ssh_key_file":
		cfg.Credentials.SSHKeyFile = ""
	case "age_key":
		cfg.Credentials.AgeKey = ""
	case "username":
		cfg.Credentials.Username = ""
	case "password":
		cfg.Credentials.Password = ""
	default:
		return fmt.Errorf("unknown credential field %q", field)
	}

	// Recalculate auth method from remaining credentials.
	creds := cfg.Credentials
	if creds.Token != "" {
		cfg.AuthMethod = stack.GitAuthToken
	} else if creds.SSHKeyFile != "" {
		cfg.AuthMethod = stack.GitAuthSSHFile
	} else if creds.SSHKey != "" {
		cfg.AuthMethod = stack.GitAuthSSH
	} else if creds.Username != "" {
		cfg.AuthMethod = stack.GitAuthBasic
	} else {
		cfg.AuthMethod = stack.GitAuthNone
	}

	return s.gitCfgs.Upsert(ctx, name, cfg)
}

// resolveComposeFile returns the compose file name for a stack.
// Priority: git config compose_path > detect from disk (sane defaults).
// Always returns a specific file to prevent docker compose from merging
// multiple compose files in the same directory.
func (s *StackService) resolveComposeFile(ctx context.Context, name string) string {
	// 1. Git config compose_path
	cfg, err := s.gitCfgs.GetByStackName(ctx, name)
	if err == nil && cfg != nil && cfg.ComposePath != "" {
		return cfg.ComposePath
	}

	// 2. Detect from disk -- check common names in priority order
	st, err := s.stacks.GetByName(ctx, name)
	if err != nil || st == nil {
		return ""
	}
	for _, candidate := range []string{
		"compose.yaml",
		"compose.yml",
		"docker-compose.yaml",
		"docker-compose.yml",
	} {
		if _, err := os.Stat(filepath.Join(st.Path, candidate)); err == nil {
			return candidate
		}
	}

	return ""
}

// resolveAgeKey returns the age key for SOPS decryption of a stack.
// Checks per-stack credential first, then global.
func (s *StackService) resolveAgeKey(ctx context.Context, stackName string) string {
	var perStackAgeKey string
	cfg, err := s.gitCfgs.GetByStackName(ctx, stackName)
	if err == nil && cfg != nil && cfg.Credentials != nil {
		perStackAgeKey = cfg.Credentials.AgeKey
	}
	return sops.ResolveAgeKey(perStackAgeKey, s.dataDir)
}

// SOPS function seams: production binds the real sops CLI wrappers, unit
// tests swap them to force deterministic decrypt outcomes.
var (
	sopsAvailable   = sops.IsAvailable
	sopsDecryptEnv  = sops.DecryptEnvFile
	sopsDecryptComp = sops.DecryptComposeSecrets
)

// decryptSopsFiles decrypts a SOPS-encrypted .env and one or more compose
// files with the given age key, saving .sops backups so the caller can
// re-encrypt after the compose operation. A genuine decrypt failure (wrong
// key, corrupt ciphertext) is returned so the caller aborts instead of
// running docker compose against ciphertext. No sops binary, no age key,
// and files without a SOPS payload stay non-fatal (nil).
func decryptSopsFiles(log *zap.Logger, stackName, envFile, ageKey string, composePaths ...string) error {
	if !sopsAvailable() {
		return nil
	}
	if ageKey == "" {
		log.Debug("no age key available for SOPS decryption", zap.String("stack", stackName))
		return nil
	}
	if decrypted, err := sopsDecryptEnv(envFile, ageKey); err != nil {
		log.Error("sops: failed to decrypt .env", zap.String("stack", stackName), zap.String("path", envFile), zap.Error(err))
		return fmt.Errorf("decrypting .env for stack %s: %w", stackName, err)
	} else if decrypted {
		log.Info("sops: decrypted .env", zap.String("stack", stackName), zap.String("path", envFile))
	}
	for _, composePath := range composePaths {
		if _, err := os.Stat(composePath); err != nil {
			continue
		}
		if decrypted, err := sopsDecryptComp(composePath, ageKey); err != nil {
			log.Error("sops: failed to decrypt compose", zap.String("stack", stackName), zap.String("file", filepath.Base(composePath)), zap.Error(err))
			return fmt.Errorf("decrypting compose for stack %s: %w", stackName, err)
		} else if decrypted {
			log.Info("sops: decrypted compose", zap.String("stack", stackName), zap.String("file", filepath.Base(composePath)))
		}
	}
	return nil
}

// decryptSopsSecrets decrypts SOPS-encrypted .env and compose files in the stack
// directory before docker compose operations. Saves encrypted originals as .sops
// backups so reEncryptSopsSecrets can restore them after deploy.
// A genuine decrypt failure is returned so the caller aborts instead of
// running compose against ciphertext; no sops binary, no age key, or files
// without a SOPS payload stay non-fatal (nil).
// Every caller must check the returned error in one of these forms:
// err := s.decryptSopsSecrets|err = s.decryptSopsSecrets|if err := s.decryptSopsSecrets
func (s *StackService) decryptSopsSecrets(ctx context.Context, stackName, stackPath string) error {
	ageKey := s.resolveAgeKey(ctx, stackName)
	var composePaths []string
	for _, name := range []string{"compose.yaml", "compose.yml", "docker-compose.yaml", "docker-compose.yml"} {
		composePaths = append(composePaths, filepath.Join(stackPath, name))
	}
	return decryptSopsFiles(s.log, stackName, s.resolveEnvFile(ctx, stackName, stackPath), ageKey, composePaths...)
}

// reEncryptSopsSecretsCtx restores SOPS-encrypted .env / compose files. The
// stackName is used to look up GitSource.EnvPath when present; pass "" for
// non-git stacks.
func (s *StackService) reEncryptSopsSecretsCtx(ctx context.Context, stackName, stackPath string) {
	envFile := s.resolveEnvFile(ctx, stackName, stackPath)
	if err := sops.ReEncryptEnvFile(envFile); err != nil {
		s.log.Error("sops: failed to re-encrypt .env", zap.String("path", envFile), zap.Error(err))
	} else {
		if _, err := os.Stat(envFile + ".sops"); err != nil {
			s.log.Info("sops: re-encrypted .env", zap.String("path", envFile))
		}
	}
	for _, name := range []string{"compose.yaml", "compose.yml", "docker-compose.yaml", "docker-compose.yml"} {
		sops.ReEncryptComposeSecrets(filepath.Join(stackPath, name))
	}
}

// resolveEnvFile returns the absolute path to the .env file for a stack.
// For git stacks, honours GitSource.EnvPath; for local stacks, defaults to
// "<stackPath>/.env".
func (s *StackService) resolveEnvFile(ctx context.Context, stackName, stackPath string) string {
	if stackName != "" && s.gitCfgs != nil {
		if cfg, _ := s.gitCfgs.GetByStackName(ctx, stackName); cfg != nil {
			return cfg.ResolveEnvPath(stackPath)
		}
	}
	return filepath.Join(stackPath, ".env")
}

// publishEvent sends an event to the bus if one is configured.
func (s *StackService) publishEvent(evt event.Event) {
	if s.bus != nil {
		s.bus.Publish(evt)
	}
}

// ActionContext holds the resolved context for a streaming compose action.
// The caller must call Cleanup() when done (releases lock, re-encrypts SOPS,
// removes the ephemeral DOCKER_CONFIG tempdir).
type ActionContext struct {
	StackPath   string
	ComposeFile string
	// DockerConfigDir is the path to an ephemeral directory containing a
	// config.json with the stack's resolved registry credentials. Empty when
	// no creds are registered for this stack. The caller MUST plumb this
	// into the compose command's context via docker.WithDockerConfigDir(),
	// otherwise `docker compose pull/up` runs with the host's bare
	// ~/.docker/config.json and private-registry pulls fail with 'unauthorized'.
	DockerConfigDir string
	cleanup         func()
}

// Cleanup releases the stack lock, re-encrypts SOPS secrets, and removes
// the ephemeral DOCKER_CONFIG dir.
func (ac *ActionContext) Cleanup() {
	if ac.cleanup != nil {
		ac.cleanup()
	}
}

// PrepareAction locks the stack, resolves the compose file, decrypts SOPS
// secrets, materialises the DOCKER_CONFIG tempdir, and returns an
// ActionContext for the caller to run compose commands against.
// The caller MUST call ActionContext.Cleanup() when done.
func (s *StackService) PrepareAction(ctx context.Context, name string) (*ActionContext, error) {
	s.locks.Lock(name)

	st, err := s.stacks.GetByName(ctx, name)
	if err != nil {
		s.locks.Unlock(name)
		return nil, err
	}
	if st == nil {
		s.locks.Unlock(name)
		return nil, ErrNotFound
	}

	cf := s.resolveComposeFile(ctx, name)
	if err := s.decryptSopsSecrets(ctx, name, st.Path); err != nil {
		// No ActionContext is returned, so its Cleanup never runs: re-encrypt
		// the partial decrypt here and release the lock like the other error
		// paths.
		s.reEncryptSopsSecretsCtx(ctx, name, st.Path)
		s.locks.Unlock(name)
		return nil, err
	}

	// Materialise a DOCKER_CONFIG with the resolved global + per-stack creds.
	// withRegistryAuth returns ctx + cleanup; we don't need the ctx (the WS
	// handler builds its own background ctx with a longer deadline) but the
	// dir is the bit we need to surface. Pull it back out of the augmented
	// ctx and store it on the ActionContext.
	regCtx, regCleanup := s.withRegistryAuth(ctx, name)
	dockerCfgDir := docker.DockerConfigDirFromCtx(regCtx)

	return &ActionContext{
		StackPath:       st.Path,
		ComposeFile:     cf,
		DockerConfigDir: dockerCfgDir,
		cleanup: func() {
			regCleanup()
			s.reEncryptSopsSecretsCtx(ctx, name, st.Path)
			s.locks.Unlock(name)
		},
	}, nil
}

// PublishActionEvent emits the appropriate domain event for a completed compose action.
func (s *StackService) PublishActionEvent(name, action string, actionErr error) {
	now := time.Now()
	if actionErr != nil {
		s.publishEvent(event.StackError{Name: name, Error: actionErr.Error(), Timestamp: now})
		return
	}
	switch action {
	case "up", "update", "build":
		s.publishEvent(event.StackDeployed{Name: name, Timestamp: now})
	case "down":
		s.publishEvent(event.StackStopped{Name: name, Timestamp: now})
	case "restart":
		s.publishEvent(event.StackDeployed{Name: name, Timestamp: now})
	case "pull":
		s.publishEvent(event.StackUpdated{Name: name, Timestamp: now})
	}
}

// ComposeForStack returns the Compose CLI wrapper for the stack's docker host
// (e.g., streaming WS handlers). Falls back to the default host's wrapper.
func (s *StackService) ComposeForStack(ctx context.Context, name string) (*docker.Compose, error) {
	st, err := s.stacks.GetByName(ctx, name)
	if err != nil {
		return nil, err
	}
	if st == nil {
		return nil, fmt.Errorf("stack %q not found", name)
	}
	return s.composeFor(ctx, st)
}

// DecryptEnvContent returns the decrypted .env content for display in the UI.
// If the file is SOPS-encrypted, decrypts in memory without modifying disk.
// If not encrypted or sops is unavailable, returns raw content.
func (s *StackService) DecryptEnvContent(ctx context.Context, stackName, envPath string) string {
	data, err := os.ReadFile(envPath)
	if err != nil {
		return ""
	}
	if !sops.IsAvailable() || !sops.IsSopsEncrypted(data) {
		return string(data)
	}
	ageKey := s.resolveAgeKey(ctx, stackName)
	plaintext, err := sops.DecryptInMemory(envPath, ageKey)
	if err != nil {
		return string(data) // fallback to raw if decrypt fails
	}
	return string(plaintext)
}

// EncryptEnvContent encrypts plaintext .env content with the stack's age key.
// Returns SOPS ciphertext. If sops is unavailable or no age key is resolved,
// returns the plaintext unchanged (the file will be stored unencrypted).
func (s *StackService) EncryptEnvContent(ctx context.Context, stackName, plaintext string) (string, error) {
	if plaintext == "" || !sops.IsAvailable() {
		return plaintext, nil
	}
	// Check if the plaintext is already SOPS-encrypted (double-encrypt guard).
	if sops.IsSopsEncrypted([]byte(plaintext)) {
		return plaintext, nil
	}
	ageKey := s.resolveAgeKey(ctx, stackName)
	if ageKey == "" {
		return plaintext, nil
	}
	encrypted, err := sops.Encrypt([]byte(plaintext), "dotenv", ageKey)
	if err != nil {
		return plaintext, fmt.Errorf("re-encrypting .env for stack %q: %w", stackName, err)
	}
	return string(encrypted), nil
}

// Containers returns the containers for a stack.
func (s *StackService) Containers(ctx context.Context, stackName string) ([]domcontainer.Container, error) {
	// Empty stack name = return all containers across all hosts.
	// Fan out one ListContainers call per distinct host referenced by known stacks.
	if stackName == "" {
		return s.listAllContainers(ctx)
	}

	st, err := s.stacks.GetByName(ctx, stackName)
	if err != nil {
		return nil, err
	}
	if st == nil {
		return nil, ErrNotFound
	}
	cl, cErr := s.clientFor(ctx, st)
	if cErr != nil {
		return nil, cErr
	}
	return cl.ListContainers(ctx, stackName)
}

// hostListTimeout bounds a single per-host container listing. A dead or
// blackholed host must not stall the fan-out past this.
const hostListTimeout = 3 * time.Second

// HostContainers is the per-host result of a container listing fan-out.
// HostID 0 is the local/default daemon. Stacks lists the known stacks
// pinned to that host, filled even for failed hosts (the stack-to-host map
// comes from the DB, not the daemon).
type HostContainers struct {
	HostID     int64
	Stacks     []string
	Containers []domcontainer.Container
	Reachable  bool
}

// ListContainersByHost fans out one ListContainers("") call per distinct
// host referenced by known stacks and returns per-host results with
// reachability. The local daemon and every remote host are listed
// concurrently, each under its own hostListTimeout. A failing host still
// yields a result with Reachable=false, so callers can distinguish "host
// down" from "host has no containers".
func (s *StackService) ListContainersByHost(ctx context.Context) ([]HostContainers, error) {
	stacks, err := s.stacks.List(ctx)
	if err != nil {
		return nil, err
	}

	// Group known stacks by host (nil HostID tiles into the local host 0).
	hostStacks := make(map[int64][]string)
	for _, st := range stacks {
		if st.HostID == nil {
			hostStacks[0] = append(hostStacks[0], st.Name)
		} else {
			hostStacks[*st.HostID] = append(hostStacks[*st.HostID], st.Name)
		}
	}

	var (
		mu      sync.Mutex
		results []HostContainers
		wg      sync.WaitGroup
	)
	appendResult := func(hr HostContainers) {
		mu.Lock()
		results = append(results, hr)
		mu.Unlock()
	}

	// Local daemon.
	if len(hostStacks[0]) > 0 && s.docker != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			listCtx, cancel := context.WithTimeout(ctx, hostListTimeout)
			defer cancel()
			localResult, localErr := s.docker.ListContainers(listCtx, "")
			if localErr != nil {
				s.log.Warn("listAllContainers: local daemon failed", zap.Error(localErr))
				appendResult(HostContainers{HostID: 0, Stacks: hostStacks[0], Reachable: false})
			} else {
				appendResult(HostContainers{HostID: 0, Stacks: hostStacks[0], Containers: localResult, Reachable: true})
			}
		}()
	}

	// Remote hosts. The ClientFor call runs inside the goroutine under the
	// per-host timeout: client construction does a 10s Info probe on first
	// use for a new host, so it must not run before the timeout starts.
	if s.factory != nil {
		for hostID, names := range hostStacks {
			if hostID == 0 {
				continue
			}
			wg.Add(1)
			go func(hostID int64, names []string) {
				defer wg.Done()
				hostCtx, cancel := context.WithTimeout(ctx, hostListTimeout)
				defer cancel()
				cl, err := s.factory.ClientFor(hostCtx, &hostID)
				if err != nil {
					s.log.Warn("listAllContainers: resolving client for docker host",
						zap.Int64("host_id", hostID), zap.Error(err))
					appendResult(HostContainers{HostID: hostID, Stacks: names, Reachable: false})
					return
				}
				remoteResult, remoteErr := cl.ListContainers(hostCtx, "")
				if remoteErr != nil {
					s.log.Warn("listAllContainers: listing containers on host",
						zap.Int64("host_id", hostID), zap.Error(remoteErr))
					appendResult(HostContainers{HostID: hostID, Stacks: names, Reachable: false})
					return
				}
				appendResult(HostContainers{HostID: hostID, Stacks: names, Containers: remoteResult, Reachable: true})
			}(hostID, names)
		}
	}

	wg.Wait()
	return results, nil
}

// listAllContainers merges the per-host fan-out into one container list.
// Failing hosts simply contribute nothing.
func (s *StackService) listAllContainers(ctx context.Context) ([]domcontainer.Container, error) {
	hosts, err := s.ListContainersByHost(ctx)
	if err != nil {
		return nil, err
	}
	var all []domcontainer.Container
	for _, h := range hosts {
		all = append(all, h.Containers...)
	}
	return all, nil
}

// ExecCompose runs an arbitrary docker compose subcommand against a stack.
// The command string is split into args and passed to `docker compose <args>`.
// Returns stdout, stderr, and exit code.
func (s *StackService) ExecCompose(ctx context.Context, name string, args []string) (*docker.ComposeResult, error) {
	st, err := s.stacks.GetByName(ctx, name)
	if err != nil {
		return nil, err
	}
	if st == nil {
		return nil, ErrNotFound
	}
	cw, cErr := s.composeFor(ctx, st)
	if cErr != nil {
		return nil, cErr
	}
	return cw.Exec(ctx, st.Path, args)
}

func deriveStackStatus(containers []domcontainer.Container, oneShots map[string]bool) stack.Status {
	if len(containers) == 0 {
		return stack.StatusStopped
	}

	running := 0
	oneOff := 0
	for _, c := range containers {
		if c.IsRunning() {
			running++
		} else if c.Status == domcontainer.StatusExited && (c.IsOneOff() || oneShots[c.ServiceName]) {
			// One-off tasks (init containers, migration runners, restore jobs)
			// don't contribute to the long-running service count regardless of
			// exit code — a failed restore doesn't make the stack "partial".
			oneOff++
			// Declared one-shot services (restart: "no" / "on-failure" in the
			// compose file) count too: the container list API carries no restart
			// policy, so the compose file is the only signal separating a
			// completed job (memledger-migrate) from a stopped service.
		}
	}

	// Long-running services = total minus exited one-off containers
	longRunning := len(containers) - oneOff

	switch {
	case longRunning == 0 && oneOff > 0:
		// All containers are exited one-offs (e.g. pure init stack)
		return stack.StatusStopped
	case running == longRunning:
		// All long-running services are up
		return stack.StatusRunning
	case running == 0:
		return stack.StatusStopped
	default:
		return stack.StatusPartial
	}
}

// composeContentFor resolves a stack's compose file body: the git config's
// compose path first (when the stack is git-backed), then the common file
// names in the stack dir. Sets st.GitConfig when it supplies the path.
// Returns "" when nothing readable exists.
func (s *StackService) composeContentFor(ctx context.Context, st *stack.Stack) string {
	if st.Source == stack.SourceGit && st.GitConfig == nil {
		cfg, err := s.gitCfgs.GetByStackName(ctx, st.Name)
		if err == nil && cfg != nil {
			st.GitConfig = cfg
		}
	}
	if st.Source == stack.SourceGit && st.GitConfig != nil && st.GitConfig.ComposePath != "" {
		if data, err := os.ReadFile(filepath.Join(st.Path, st.GitConfig.ComposePath)); err == nil {
			return string(data)
		}
	}
	for _, name := range []string{"compose.yaml", "compose.yml", "docker-compose.yaml", "docker-compose.yml"} {
		if data, err := os.ReadFile(filepath.Join(st.Path, name)); err == nil {
			return string(data)
		}
	}
	return ""
}

// OneShotServicesByStack returns, per stack name, the set of service names
// declared with restart: "no" / "on-failure" - jobs that complete (migrations,
// seeds, builds) rather than stay up. The StatusRefresher uses it to keep
// completed one-shot services from dragging a fully-up stack into "partial".
// Compose files are re-read per call (refresh cadence is seconds, files are
// small); a stack whose file is missing or unparseable maps to nil.
func (s *StackService) OneShotServicesByStack(ctx context.Context) map[string]map[string]bool {
	stacks, err := s.stacks.List(ctx)
	if err != nil {
		return nil
	}
	out := make(map[string]map[string]bool, len(stacks))
	for _, st := range stacks {
		out[st.Name] = composeOneShotServices(s.composeContentFor(ctx, st))
	}
	return out
}

// composeOneShotServices parses a compose file body and returns the set of
// service names declared with restart: "no" or "on-failure" (scalar form, or
// the {on-failure: N} map form). Unspecified restart is NOT one-shot: compose
// defaults it to "no", so a long-running service without an explicit policy
// must still count as a service. A parse failure returns nil (caller falls
// back to the label-based one-off rule).
func composeOneShotServices(composeContent string) map[string]bool {
	if composeContent == "" {
		return nil
	}
	var doc struct {
		Services map[string]struct {
			Restart yaml.Node `yaml:"restart"`
		} `yaml:"services"`
	}
	if err := yaml.Unmarshal([]byte(composeContent), &doc); err != nil {
		return nil
	}
	var out map[string]bool
	for svc, spec := range doc.Services {
		oneShot := false
		switch spec.Restart.Kind {
		case yaml.ScalarNode:
			var s string
			if err := spec.Restart.Decode(&s); err == nil && (s == "no" || s == "on-failure") {
				oneShot = true
			}
		case yaml.MappingNode:
			// Map form only exists for on-failure: {policy: <cond>, attempts: N}.
			var m map[string]any
			if err := spec.Restart.Decode(&m); err == nil {
				if _, ok := m["on-failure"]; ok {
					oneShot = true
				}
			}
		}
		if oneShot {
			if out == nil {
				out = make(map[string]bool)
			}
			out[svc] = true
		}
	}
	return out
}
