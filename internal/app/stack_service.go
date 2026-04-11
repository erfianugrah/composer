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
	"github.com/erfianugrah/composer/internal/domain/stack"
	"github.com/erfianugrah/composer/internal/infra/docker"
	"github.com/erfianugrah/composer/internal/infra/sops"
)

// StackService orchestrates stack management operations.
type StackService struct {
	stacks    stack.StackRepository
	gitCfgs   stack.GitConfigRepository
	docker    *docker.Client
	compose   *docker.Compose
	bus       event.Bus
	log       *zap.Logger
	stacksDir string
	dataDir   string
	locks     stackLocks // per-stack mutex to prevent concurrent operations
}

// stackLocks provides per-stack mutual exclusion.
type stackLocks struct {
	mu    sync.Mutex
	locks map[string]*sync.Mutex
}

func (l *stackLocks) lock(name string) {
	l.mu.Lock()
	m, ok := l.locks[name]
	if !ok {
		m = &sync.Mutex{}
		l.locks[name] = m
	}
	l.mu.Unlock()
	m.Lock()
}

func (l *stackLocks) unlock(name string) {
	l.mu.Lock()
	m := l.locks[name]
	l.mu.Unlock()
	if m != nil {
		m.Unlock()
	}
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
		locks:     stackLocks{locks: make(map[string]*sync.Mutex)},
	}
}

// Create creates a new local stack with the given compose content.
func (s *StackService) Create(ctx context.Context, name, composeContent string) (*stack.Stack, error) {
	s.locks.lock(name)
	defer s.locks.unlock(name)

	stackPath := filepath.Join(s.stacksDir, name)

	st, err := stack.NewStack(name, stackPath, stack.SourceLocal)
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
		if _, err := s.compose.Validate(ctx, stackPath); err != nil {
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
	s.decryptSopsSecrets(ctx, name, st.Path)
	if _, err := s.compose.Up(ctx, st.Path, cf); err != nil {
		s.log.Warn("auto-deploy failed (stack created but not running)", zap.String("stack", name), zap.Error(err))
		// Don't fail the create -- stack is saved, user can deploy manually
	} else {
		s.reEncryptSopsSecrets(st.Path)
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
	var composeContent string
	if st.Source == stack.SourceGit {
		cfg, err := s.gitCfgs.GetByStackName(ctx, name)
		if err == nil && cfg != nil {
			st.GitConfig = cfg
			// Use the git config's compose path
			gitComposePath := filepath.Join(st.Path, cfg.ComposePath)
			if data, err := os.ReadFile(gitComposePath); err == nil {
				composeContent = string(data)
			}
		}
	}
	if composeContent == "" {
		// Fallback: try common compose file names
		for _, name := range []string{"compose.yaml", "compose.yml", "docker-compose.yaml", "docker-compose.yml"} {
			if data, err := os.ReadFile(filepath.Join(st.Path, name)); err == nil {
				composeContent = string(data)
				break
			}
		}
	}
	st.ComposeContent = composeContent

	if st.Source == stack.SourceGit && st.GitConfig == nil {
		cfg, err := s.gitCfgs.GetByStackName(ctx, name)
		if err == nil && cfg != nil {
			st.GitConfig = cfg
		}
	}

	containers, err := s.docker.ListContainers(ctx, name)
	if err == nil {
		st.Status = deriveStackStatus(containers)
	}

	return st, nil
}

// List returns all stacks with runtime status.
// P3: Uses a single Docker API call to list ALL containers, then groups by stack.
func (s *StackService) List(ctx context.Context) ([]*stack.Stack, error) {
	stacks, err := s.stacks.List(ctx)
	if err != nil {
		return nil, err
	}

	// One Docker call for all containers instead of N calls per stack
	allContainers, err := s.docker.ListContainers(ctx, "")
	if err == nil {
		// Group by compose project name
		byStack := make(map[string][]domcontainer.Container)
		for _, c := range allContainers {
			if c.StackName != "" {
				byStack[c.StackName] = append(byStack[c.StackName], c)
			}
		}
		for _, st := range stacks {
			st.Status = deriveStackStatus(byStack[st.Name])
		}
	}

	return stacks, nil
}

// Update updates compose content. Writes to disk + DB.
// For git-backed stacks, this creates local changes that diverge from HEAD.
// The sync status is marked "dirty" to warn the user that the next git sync
// will overwrite these local edits unless they are committed and pushed.
func (s *StackService) Update(ctx context.Context, name, composeContent string) (*stack.Stack, error) {
	s.locks.lock(name)
	defer s.locks.unlock(name)

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
	s.locks.lock(name)
	defer s.locks.unlock(name)

	st, err := s.stacks.GetByName(ctx, name)
	if err != nil {
		return err
	}
	if st == nil {
		return ErrNotFound
	}

	// Stop containers first (best effort)
	cf := s.resolveComposeFile(ctx, name)
	s.compose.Down(ctx, st.Path, cf, removeVolumes)

	if err := s.stacks.Delete(ctx, name); err != nil {
		return err
	}

	os.RemoveAll(st.Path)

	s.publishEvent(event.StackDeleted{Name: name, Timestamp: time.Now()})

	// Clean up the per-stack lock to prevent unbounded growth
	s.locks.mu.Lock()
	delete(s.locks.locks, name)
	s.locks.mu.Unlock()

	return nil
}

// Deploy runs docker compose up.
func (s *StackService) Deploy(ctx context.Context, name string) (*docker.ComposeResult, error) {
	s.locks.lock(name)
	defer s.locks.unlock(name)

	st, err := s.stacks.GetByName(ctx, name)
	if err != nil {
		return nil, err
	}
	if st == nil {
		return nil, ErrNotFound
	}

	cf := s.resolveComposeFile(ctx, name)
	s.log.Info("deploying stack", zap.String("stack", name), zap.String("path", st.Path), zap.String("compose_file", cf))
	s.decryptSopsSecrets(ctx, name, st.Path)
	defer s.reEncryptSopsSecrets(st.Path)

	result, err := s.compose.Up(ctx, st.Path, cf)
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
	s.locks.lock(name)
	defer s.locks.unlock(name)

	st, err := s.stacks.GetByName(ctx, name)
	if err != nil {
		return nil, err
	}
	if st == nil {
		return nil, ErrNotFound
	}

	cf := s.resolveComposeFile(ctx, name)
	s.log.Info("build+deploy stack", zap.String("stack", name), zap.String("path", st.Path), zap.String("compose_file", cf))
	s.decryptSopsSecrets(ctx, name, st.Path)
	defer s.reEncryptSopsSecrets(st.Path)

	result, err := s.compose.BuildAndUp(ctx, st.Path, cf)
	if err != nil {
		s.publishEvent(event.StackError{Name: name, Error: err.Error(), Timestamp: time.Now()})
		return result, err
	}

	s.publishEvent(event.StackDeployed{Name: name, Timestamp: time.Now()})
	return result, nil
}

// Stop runs docker compose down.
func (s *StackService) Stop(ctx context.Context, name string) (*docker.ComposeResult, error) {
	s.locks.lock(name)
	defer s.locks.unlock(name)

	st, err := s.stacks.GetByName(ctx, name)
	if err != nil {
		return nil, err
	}
	if st == nil {
		return nil, ErrNotFound
	}

	cf := s.resolveComposeFile(ctx, name)
	s.log.Info("stopping stack", zap.String("stack", name))
	result, err := s.compose.Down(ctx, st.Path, cf, false)
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
	s.locks.lock(name)
	defer s.locks.unlock(name)

	st, err := s.stacks.GetByName(ctx, name)
	if err != nil {
		return nil, err
	}
	if st == nil {
		return nil, ErrNotFound
	}

	cf := s.resolveComposeFile(ctx, name)
	s.log.Info("restarting stack", zap.String("stack", name))
	result, err := s.compose.Restart(ctx, st.Path, cf)
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
	s.locks.lock(name)
	defer s.locks.unlock(name)

	st, err := s.stacks.GetByName(ctx, name)
	if err != nil {
		return nil, err
	}
	if st == nil {
		return nil, ErrNotFound
	}

	cf := s.resolveComposeFile(ctx, name)
	s.log.Info("pulling images", zap.String("stack", name))
	result, err := s.compose.Pull(ctx, st.Path, cf)
	if err == nil {
		s.log.Info("pull completed", zap.String("stack", name))
		s.publishEvent(event.StackUpdated{Name: name, Timestamp: time.Now()})
	} else {
		s.log.Error("pull failed", zap.String("stack", name), zap.Error(err))
	}
	return result, err
}

// Config runs docker compose config and returns the normalized YAML.
func (s *StackService) Config(ctx context.Context, name string) (*docker.ComposeResult, error) {
	st, err := s.stacks.GetByName(ctx, name)
	if err != nil {
		return nil, err
	}
	if st == nil {
		return nil, ErrNotFound
	}
	return s.compose.Config(ctx, st.Path)
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
	return s.compose.Validate(ctx, st.Path)
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
func (s *StackService) ConvertToGit(ctx context.Context, name string, repoURL, branch string, creds *stack.GitCredentials) error {
	s.locks.lock(name)
	defer s.locks.unlock(name)

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
	gitCfg := &stack.GitSource{
		RepoURL:     repoURL,
		Branch:      branch,
		ComposePath: "compose.yaml",
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
	s.locks.lock(name)
	defer s.locks.unlock(name)

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

// decryptSopsSecrets decrypts SOPS-encrypted .env and compose files in the stack
// directory before docker compose operations. Saves encrypted originals as .sops
// backups so reEncryptSopsSecrets can restore them after deploy.
// No-op if sops binary is not installed or files are not SOPS-encrypted.
func (s *StackService) decryptSopsSecrets(ctx context.Context, stackName, stackPath string) {
	if !sops.IsAvailable() {
		return
	}
	ageKey := s.resolveAgeKey(ctx, stackName)
	if ageKey == "" {
		s.log.Debug("no age key available for SOPS decryption", zap.String("stack", stackName))
		return
	}

	if decrypted, err := sops.DecryptEnvFile(stackPath, ageKey); err != nil {
		s.log.Error("sops: failed to decrypt .env", zap.String("stack", stackName), zap.Error(err))
	} else if decrypted {
		s.log.Info("sops: decrypted .env", zap.String("stack", stackName))
	}

	for _, name := range []string{"compose.yaml", "compose.yml", "docker-compose.yaml", "docker-compose.yml"} {
		composePath := filepath.Join(stackPath, name)
		if _, err := os.Stat(composePath); err == nil {
			if decrypted, err := sops.DecryptComposeSecrets(composePath, ageKey); err != nil {
				s.log.Error("sops: failed to decrypt compose", zap.String("stack", stackName), zap.String("file", name), zap.Error(err))
			} else if decrypted {
				s.log.Info("sops: decrypted compose", zap.String("stack", stackName), zap.String("file", name))
			}
		}
	}
}

func (s *StackService) reEncryptSopsSecrets(stackPath string) {
	if err := sops.ReEncryptEnvFile(stackPath); err != nil {
		s.log.Error("sops: failed to re-encrypt .env", zap.String("path", stackPath), zap.Error(err))
	} else {
		// Only log if there was a .sops backup to restore
		envSops := filepath.Join(stackPath, ".env.sops")
		if _, err := os.Stat(envSops); err != nil {
			// .sops already cleaned up = re-encrypt happened
			s.log.Info("sops: re-encrypted .env", zap.String("path", stackPath))
		}
	}
	for _, name := range []string{"compose.yaml", "compose.yml", "docker-compose.yaml", "docker-compose.yml"} {
		sops.ReEncryptComposeSecrets(filepath.Join(stackPath, name))
	}
}

// publishEvent sends an event to the bus if one is configured.
func (s *StackService) publishEvent(evt event.Event) {
	if s.bus != nil {
		s.bus.Publish(evt)
	}
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

// Containers returns the containers for a stack.
func (s *StackService) Containers(ctx context.Context, stackName string) ([]domcontainer.Container, error) {
	return s.docker.ListContainers(ctx, stackName)
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
	return s.compose.Exec(ctx, st.Path, args)
}

func deriveStackStatus(containers []domcontainer.Container) stack.Status {
	if len(containers) == 0 {
		return stack.StatusStopped
	}

	running := 0
	completedOneOff := 0
	for _, c := range containers {
		if c.IsRunning() {
			running++
		} else if c.IsCompletedOneOff() {
			completedOneOff++
		}
	}

	// Long-running services = total minus completed one-off containers
	longRunning := len(containers) - completedOneOff

	switch {
	case longRunning == 0 && completedOneOff > 0:
		// All containers are completed one-offs (unusual but possible)
		return stack.StatusStopped
	case running == longRunning:
		// All long-running services are up (one-offs completed successfully)
		return stack.StatusRunning
	case running == 0:
		return stack.StatusStopped
	default:
		return stack.StatusPartial
	}
}
