package app

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/erfianugrah/composer/internal/domain/stack"
)

// ---------------------------------------------------------------------------
// Mock repositories
// ---------------------------------------------------------------------------

// mockStackRepo is a minimal in-memory StackRepository.
type mockStackRepo struct {
	stacks map[string]*stack.Stack
}

func newMockStackRepo() *mockStackRepo {
	return &mockStackRepo{stacks: make(map[string]*stack.Stack)}
}

func (r *mockStackRepo) Create(_ context.Context, s *stack.Stack) error {
	r.stacks[s.Name] = s
	return nil
}

func (r *mockStackRepo) GetByName(_ context.Context, name string) (*stack.Stack, error) {
	return r.stacks[name], nil
}

func (r *mockStackRepo) List(_ context.Context) ([]*stack.Stack, error) {
	var list []*stack.Stack
	for _, s := range r.stacks {
		list = append(list, s)
	}
	return list, nil
}

func (r *mockStackRepo) Update(_ context.Context, s *stack.Stack) error {
	r.stacks[s.Name] = s
	return nil
}

func (r *mockStackRepo) Delete(_ context.Context, name string) error {
	delete(r.stacks, name)
	return nil
}

// mockGitConfigRepo is a minimal in-memory GitConfigRepository.
type mockGitConfigRepo struct {
	configs map[string]*stack.GitSource
}

func newMockGitConfigRepo() *mockGitConfigRepo {
	return &mockGitConfigRepo{configs: make(map[string]*stack.GitSource)}
}

func (r *mockGitConfigRepo) GetByStackName(_ context.Context, name string) (*stack.GitSource, error) {
	return r.configs[name], nil
}

func (r *mockGitConfigRepo) Upsert(_ context.Context, name string, cfg *stack.GitSource) error {
	r.configs[name] = cfg
	return nil
}

func (r *mockGitConfigRepo) Delete(_ context.Context, name string) error {
	delete(r.configs, name)
	return nil
}

func (r *mockGitConfigRepo) UpdateSyncStatus(_ context.Context, _ string, _ stack.GitSyncStatus, _ string) error {
	return nil
}

// ---------------------------------------------------------------------------
// ImportFromDir — blocklist validation
// ---------------------------------------------------------------------------

func TestImportFromDir_BlocksSensitivePaths(t *testing.T) {
	svc := NewStackService(newMockStackRepo(), newMockGitConfigRepo(), nil, nil, nil, nil, t.TempDir(), t.TempDir())

	blocked := []string{"/etc", "/proc", "/sys", "/dev", "/root", "/boot", "/var/run"}
	for _, path := range blocked {
		_, err := svc.ImportFromDir(context.Background(), path)
		assert.Error(t, err, "should block import from %s", path)
		assert.Contains(t, err.Error(), "not permitted", "path=%s", path)
	}
}

func TestImportFromDir_BlocksSensitiveSubPaths(t *testing.T) {
	svc := NewStackService(newMockStackRepo(), newMockGitConfigRepo(), nil, nil, nil, nil, t.TempDir(), t.TempDir())

	// Sub-paths under blocked dirs should also be blocked (or fail with permission denied)
	subpaths := []string{"/etc/nginx", "/proc/1", "/sys/class", "/dev/shm"}
	for _, path := range subpaths {
		_, err := svc.ImportFromDir(context.Background(), path)
		assert.Error(t, err, "should block import from %s", path)
		assert.Contains(t, err.Error(), "not permitted", "path=%s", path)
	}

	// /root/.ssh may fail with permission denied before reaching blocklist check —
	// either way it must error
	_, err := svc.ImportFromDir(context.Background(), "/root/.ssh")
	assert.Error(t, err, "should block or deny /root/.ssh")
}

func TestImportFromDir_BlocksSymlinkToSensitive(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("skip symlink test as root")
	}

	tmpDir := t.TempDir()
	symlink := filepath.Join(tmpDir, "etc-link")
	err := os.Symlink("/etc", symlink)
	require.NoError(t, err)

	svc := NewStackService(newMockStackRepo(), newMockGitConfigRepo(), nil, nil, nil, nil, t.TempDir(), t.TempDir())
	_, err = svc.ImportFromDir(context.Background(), symlink)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not permitted")
}

func TestImportFromDir_AllowsNormalDir(t *testing.T) {
	sourceDir := t.TempDir()
	stackDir := filepath.Join(sourceDir, "mystack")
	require.NoError(t, os.MkdirAll(stackDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(stackDir, "compose.yaml"), []byte("services:\n  web:\n    image: nginx\n"), 0644))

	stacksDir := t.TempDir()
	svc := NewStackService(newMockStackRepo(), newMockGitConfigRepo(), nil, nil, nil, nil, stacksDir, t.TempDir())

	result, err := svc.ImportFromDir(context.Background(), sourceDir)
	require.NoError(t, err)
	assert.Contains(t, result.Imported, "mystack")
	assert.Empty(t, result.Skipped)
	assert.Empty(t, result.Errors)
}

func TestImportFromDir_SkipsExisting(t *testing.T) {
	sourceDir := t.TempDir()
	stackDir := filepath.Join(sourceDir, "existing")
	require.NoError(t, os.MkdirAll(stackDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(stackDir, "compose.yaml"), []byte("services:\n  web:\n    image: nginx\n"), 0644))

	repo := newMockStackRepo()
	existing, err := stack.NewStack("existing", "/some/path", stack.SourceLocal)
	require.NoError(t, err)
	require.NoError(t, repo.Create(context.Background(), existing))

	svc := NewStackService(repo, newMockGitConfigRepo(), nil, nil, nil, nil, t.TempDir(), t.TempDir())
	result, err := svc.ImportFromDir(context.Background(), sourceDir)
	require.NoError(t, err)
	assert.Contains(t, result.Skipped, "existing")
	assert.Empty(t, result.Imported)
}

func TestImportFromDir_SkipsNonStackDirs(t *testing.T) {
	sourceDir := t.TempDir()
	// Dir without compose file — should be silently skipped
	require.NoError(t, os.MkdirAll(filepath.Join(sourceDir, "notastack"), 0755))
	require.NoError(t, os.WriteFile(filepath.Join(sourceDir, "notastack", "README.md"), []byte("hello"), 0644))

	svc := NewStackService(newMockStackRepo(), newMockGitConfigRepo(), nil, nil, nil, nil, t.TempDir(), t.TempDir())
	result, err := svc.ImportFromDir(context.Background(), sourceDir)
	require.NoError(t, err)
	assert.Empty(t, result.Imported)
	assert.Empty(t, result.Skipped)
	assert.Empty(t, result.Errors)
}

func TestImportFromDir_MultipleStacks(t *testing.T) {
	sourceDir := t.TempDir()
	for _, name := range []string{"alpha", "beta", "gamma"} {
		dir := filepath.Join(sourceDir, name)
		require.NoError(t, os.MkdirAll(dir, 0755))
		require.NoError(t, os.WriteFile(filepath.Join(dir, "compose.yaml"), []byte("services:\n  web:\n    image: nginx\n"), 0644))
	}

	svc := NewStackService(newMockStackRepo(), newMockGitConfigRepo(), nil, nil, nil, nil, t.TempDir(), t.TempDir())
	result, err := svc.ImportFromDir(context.Background(), sourceDir)
	require.NoError(t, err)
	assert.Len(t, result.Imported, 3)
	assert.ElementsMatch(t, []string{"alpha", "beta", "gamma"}, result.Imported)
}

func TestImportFromDir_AcceptsAlternateComposeNames(t *testing.T) {
	sourceDir := t.TempDir()
	// docker-compose.yml variant
	stackDir := filepath.Join(sourceDir, "legacy")
	require.NoError(t, os.MkdirAll(stackDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(stackDir, "docker-compose.yml"), []byte("services:\n  web:\n    image: nginx\n"), 0644))

	svc := NewStackService(newMockStackRepo(), newMockGitConfigRepo(), nil, nil, nil, nil, t.TempDir(), t.TempDir())
	result, err := svc.ImportFromDir(context.Background(), sourceDir)
	require.NoError(t, err)
	assert.Contains(t, result.Imported, "legacy")
}

// ---------------------------------------------------------------------------
// UpdateCredentials
// ---------------------------------------------------------------------------

func TestUpdateCredentials_NilCreds(t *testing.T) {
	gitCfgs := newMockGitConfigRepo()
	gitCfgs.configs["mystack"] = &stack.GitSource{
		RepoURL:    "https://github.com/test/repo",
		AuthMethod: stack.GitAuthToken,
	}

	svc := NewStackService(newMockStackRepo(), gitCfgs, nil, nil, nil, nil, t.TempDir(), t.TempDir())
	err := svc.UpdateCredentials(context.Background(), "mystack", nil)
	require.NoError(t, err)

	cfg := gitCfgs.configs["mystack"]
	assert.Equal(t, stack.GitAuthNone, cfg.AuthMethod)
	assert.Nil(t, cfg.Credentials)
}

func TestUpdateCredentials_WithToken(t *testing.T) {
	gitCfgs := newMockGitConfigRepo()
	gitCfgs.configs["mystack"] = &stack.GitSource{
		RepoURL: "https://github.com/test/repo",
	}

	svc := NewStackService(newMockStackRepo(), gitCfgs, nil, nil, nil, nil, t.TempDir(), t.TempDir())
	err := svc.UpdateCredentials(context.Background(), "mystack", &stack.GitCredentials{Token: "ghp_abc123"})
	require.NoError(t, err)

	cfg := gitCfgs.configs["mystack"]
	assert.Equal(t, stack.GitAuthToken, cfg.AuthMethod)
	assert.Equal(t, "ghp_abc123", cfg.Credentials.Token)
}

func TestUpdateCredentials_WithSSHKey(t *testing.T) {
	gitCfgs := newMockGitConfigRepo()
	gitCfgs.configs["mystack"] = &stack.GitSource{
		RepoURL: "https://github.com/test/repo",
	}

	svc := NewStackService(newMockStackRepo(), gitCfgs, nil, nil, nil, nil, t.TempDir(), t.TempDir())
	err := svc.UpdateCredentials(context.Background(), "mystack", &stack.GitCredentials{SSHKey: "-----BEGIN OPENSSH PRIVATE KEY-----\nfake\n-----END OPENSSH PRIVATE KEY-----"})
	require.NoError(t, err)

	cfg := gitCfgs.configs["mystack"]
	assert.Equal(t, stack.GitAuthSSH, cfg.AuthMethod)
}

func TestUpdateCredentials_WithSSHKeyFile(t *testing.T) {
	gitCfgs := newMockGitConfigRepo()
	gitCfgs.configs["mystack"] = &stack.GitSource{
		RepoURL: "https://github.com/test/repo",
	}

	svc := NewStackService(newMockStackRepo(), gitCfgs, nil, nil, nil, nil, t.TempDir(), t.TempDir())
	err := svc.UpdateCredentials(context.Background(), "mystack", &stack.GitCredentials{SSHKeyFile: "/home/user/.ssh/id_ed25519"})
	require.NoError(t, err)

	cfg := gitCfgs.configs["mystack"]
	assert.Equal(t, stack.GitAuthSSHFile, cfg.AuthMethod)
}

func TestUpdateCredentials_WithBasicAuth(t *testing.T) {
	gitCfgs := newMockGitConfigRepo()
	gitCfgs.configs["mystack"] = &stack.GitSource{
		RepoURL: "https://github.com/test/repo",
	}

	svc := NewStackService(newMockStackRepo(), gitCfgs, nil, nil, nil, nil, t.TempDir(), t.TempDir())
	err := svc.UpdateCredentials(context.Background(), "mystack", &stack.GitCredentials{Username: "admin", Password: "secret"})
	require.NoError(t, err)

	cfg := gitCfgs.configs["mystack"]
	assert.Equal(t, stack.GitAuthBasic, cfg.AuthMethod)
}

func TestUpdateCredentials_EmptyCredsResetsToNone(t *testing.T) {
	gitCfgs := newMockGitConfigRepo()
	gitCfgs.configs["mystack"] = &stack.GitSource{
		RepoURL:    "https://github.com/test/repo",
		AuthMethod: stack.GitAuthToken,
	}

	svc := NewStackService(newMockStackRepo(), gitCfgs, nil, nil, nil, nil, t.TempDir(), t.TempDir())
	// Empty creds (no fields set) should result in AuthNone
	err := svc.UpdateCredentials(context.Background(), "mystack", &stack.GitCredentials{})
	require.NoError(t, err)

	cfg := gitCfgs.configs["mystack"]
	assert.Equal(t, stack.GitAuthNone, cfg.AuthMethod)
}

func TestUpdateCredentials_NotFound(t *testing.T) {
	svc := NewStackService(newMockStackRepo(), newMockGitConfigRepo(), nil, nil, nil, nil, t.TempDir(), t.TempDir())
	err := svc.UpdateCredentials(context.Background(), "nonexistent", nil)
	assert.ErrorIs(t, err, ErrNotFound)
}

// ---------------------------------------------------------------------------
// deriveStackStatus (pure function, no deps)
// ---------------------------------------------------------------------------

func TestDeriveStackStatus_NoContainers(t *testing.T) {
	assert.Equal(t, stack.StatusStopped, deriveStackStatus(nil))
}
