package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

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
	stacksDir string,
	dataDir string,
) *StackService {
	return &StackService{
		stacks:    stacks,
		gitCfgs:   gitCfgs,
		docker:    dockerClient,
		compose:   compose,
		bus:       bus,
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
	if err := os.WriteFile(composePath, []byte(composeContent), 0644); err != nil {
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
func (s *StackService) List(ctx context.Context) ([]*stack.Stack, error) {
	stacks, err := s.stacks.List(ctx)
	if err != nil {
		return nil, err
	}

	for _, st := range stacks {
		containers, err := s.docker.ListContainers(ctx, st.Name)
		if err == nil {
			st.Status = deriveStackStatus(containers)
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

	if err := os.WriteFile(composePath, []byte(composeContent), 0644); err != nil {
		return nil, fmt.Errorf("writing compose file: %w", err)
	}

	if err := s.stacks.Update(ctx, st); err != nil {
		// Rollback: restore old file content
		if oldContent != nil {
			os.WriteFile(composePath, oldContent, 0644)
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
	s.compose.Down(ctx, st.Path, removeVolumes)

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

	// Decrypt SOPS secrets before deploy, re-encrypt after (even on failure)
	s.decryptSopsSecrets(ctx, name, st.Path)
	defer s.reEncryptSopsSecrets(st.Path)

	result, err := s.compose.Up(ctx, st.Path)
	if err != nil {
		s.publishEvent(event.StackError{Name: name, Error: err.Error(), Timestamp: time.Now()})
		return result, err
	}

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

	// Decrypt SOPS secrets before build+deploy, re-encrypt after
	s.decryptSopsSecrets(ctx, name, st.Path)
	defer s.reEncryptSopsSecrets(st.Path)

	result, err := s.compose.BuildAndUp(ctx, st.Path)
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

	result, err := s.compose.Down(ctx, st.Path, false)
	if err != nil {
		return result, err
	}

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

	result, err := s.compose.Restart(ctx, st.Path)
	if err == nil {
		s.publishEvent(event.StackDeployed{Name: name, Timestamp: time.Now()})
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

	result, err := s.compose.Pull(ctx, st.Path)
	if err == nil {
		s.publishEvent(event.StackUpdated{Name: name, Timestamp: time.Now()})
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

	// Delete git config
	if err := s.gitCfgs.Delete(ctx, name); err != nil {
		return fmt.Errorf("deleting git config: %w", err)
	}

	// Update source type
	st.Source = stack.SourceLocal
	st.UpdatedAt = time.Now().UTC()
	if err := s.stacks.Update(ctx, st); err != nil {
		return fmt.Errorf("updating stack source: %w", err)
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

	// Decrypt .env if SOPS-encrypted (saves original as .env.sops)
	sops.DecryptEnvFile(stackPath, ageKey)

	// Decrypt compose file if SOPS-encrypted (saves original as compose.yaml.sops)
	for _, name := range []string{"compose.yaml", "compose.yml", "docker-compose.yaml", "docker-compose.yml"} {
		composePath := filepath.Join(stackPath, name)
		if _, err := os.Stat(composePath); err == nil {
			sops.DecryptComposeSecrets(composePath, ageKey)
		}
	}
}

// reEncryptSopsSecrets restores SOPS-encrypted .env and compose files from
// their .sops backups. Call after deploy completes so secrets are never
// left decrypted at rest.
func (s *StackService) reEncryptSopsSecrets(stackPath string) {
	sops.ReEncryptEnvFile(stackPath)
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
	for _, c := range containers {
		if c.IsRunning() {
			running++
		}
	}

	switch {
	case running == len(containers):
		return stack.StatusRunning
	case running == 0:
		return stack.StatusStopped
	default:
		return stack.StatusPartial
	}
}
