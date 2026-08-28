package app

import (
	"context"
	"errors"
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
	svc := NewStackService(newMockStackRepo(), newMockGitConfigRepo(), nil, nil, nil, nil, t.TempDir(), t.TempDir(), NewStackLocks(), nil, nil)

	blocked := []string{"/etc", "/proc", "/sys", "/dev", "/root", "/boot", "/var/run"}
	for _, path := range blocked {
		_, err := svc.ImportFromDir(context.Background(), path)
		assert.Error(t, err, "should block import from %s", path)
		assert.Contains(t, err.Error(), "not permitted", "path=%s", path)
	}
}

func TestImportFromDir_BlocksSensitiveSubPaths(t *testing.T) {
	svc := NewStackService(newMockStackRepo(), newMockGitConfigRepo(), nil, nil, nil, nil, t.TempDir(), t.TempDir(), NewStackLocks(), nil, nil)

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

	svc := NewStackService(newMockStackRepo(), newMockGitConfigRepo(), nil, nil, nil, nil, t.TempDir(), t.TempDir(), NewStackLocks(), nil, nil)
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
	svc := NewStackService(newMockStackRepo(), newMockGitConfigRepo(), nil, nil, nil, nil, stacksDir, t.TempDir(), NewStackLocks(), nil, nil)

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

	svc := NewStackService(repo, newMockGitConfigRepo(), nil, nil, nil, nil, t.TempDir(), t.TempDir(), NewStackLocks(), nil, nil)
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

	svc := NewStackService(newMockStackRepo(), newMockGitConfigRepo(), nil, nil, nil, nil, t.TempDir(), t.TempDir(), NewStackLocks(), nil, nil)
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

	svc := NewStackService(newMockStackRepo(), newMockGitConfigRepo(), nil, nil, nil, nil, t.TempDir(), t.TempDir(), NewStackLocks(), nil, nil)
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

	svc := NewStackService(newMockStackRepo(), newMockGitConfigRepo(), nil, nil, nil, nil, t.TempDir(), t.TempDir(), NewStackLocks(), nil, nil)
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

	svc := NewStackService(newMockStackRepo(), gitCfgs, nil, nil, nil, nil, t.TempDir(), t.TempDir(), NewStackLocks(), nil, nil)
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

	svc := NewStackService(newMockStackRepo(), gitCfgs, nil, nil, nil, nil, t.TempDir(), t.TempDir(), NewStackLocks(), nil, nil)
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

	svc := NewStackService(newMockStackRepo(), gitCfgs, nil, nil, nil, nil, t.TempDir(), t.TempDir(), NewStackLocks(), nil, nil)
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

	svc := NewStackService(newMockStackRepo(), gitCfgs, nil, nil, nil, nil, t.TempDir(), t.TempDir(), NewStackLocks(), nil, nil)
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

	svc := NewStackService(newMockStackRepo(), gitCfgs, nil, nil, nil, nil, t.TempDir(), t.TempDir(), NewStackLocks(), nil, nil)
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

	svc := NewStackService(newMockStackRepo(), gitCfgs, nil, nil, nil, nil, t.TempDir(), t.TempDir(), NewStackLocks(), nil, nil)
	// Empty creds (no fields set) should result in AuthNone
	err := svc.UpdateCredentials(context.Background(), "mystack", &stack.GitCredentials{})
	require.NoError(t, err)

	cfg := gitCfgs.configs["mystack"]
	assert.Equal(t, stack.GitAuthNone, cfg.AuthMethod)
}

func TestUpdateCredentials_NotFound(t *testing.T) {
	svc := NewStackService(newMockStackRepo(), newMockGitConfigRepo(), nil, nil, nil, nil, t.TempDir(), t.TempDir(), NewStackLocks(), nil, nil)
	err := svc.UpdateCredentials(context.Background(), "nonexistent", nil)
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestClearCredentialField_SSHKey(t *testing.T) {
	gitCfgs := newMockGitConfigRepo()
	gitCfgs.configs["mystack"] = &stack.GitSource{
		RepoURL:    "git@github.com:test/repo.git",
		AuthMethod: stack.GitAuthSSH,
		Credentials: &stack.GitCredentials{
			SSHKey:           "-----BEGIN OPENSSH PRIVATE KEY-----\nfake\n-----END OPENSSH PRIVATE KEY-----",
			SSHKeyPassphrase: "secret",
			AgeKey:           "AGE-SECRET-KEY-abc",
		},
	}

	svc := NewStackService(newMockStackRepo(), gitCfgs, nil, nil, nil, nil, t.TempDir(), t.TempDir(), NewStackLocks(), nil, nil)
	err := svc.ClearCredentialField(context.Background(), "mystack", "ssh_key")
	require.NoError(t, err)

	cfg := gitCfgs.configs["mystack"]
	assert.Empty(t, cfg.Credentials.SSHKey)
	assert.Empty(t, cfg.Credentials.SSHKeyPassphrase, "passphrase should be cleared with ssh_key")
	assert.Equal(t, "AGE-SECRET-KEY-abc", cfg.Credentials.AgeKey, "other fields untouched")
	assert.Equal(t, stack.GitAuthNone, cfg.AuthMethod, "auth method recalculated")
}

func TestClearCredentialField_Token(t *testing.T) {
	gitCfgs := newMockGitConfigRepo()
	gitCfgs.configs["mystack"] = &stack.GitSource{
		RepoURL:    "https://github.com/test/repo",
		AuthMethod: stack.GitAuthToken,
		Credentials: &stack.GitCredentials{
			Token:  "ghp_abc123",
			AgeKey: "AGE-SECRET-KEY-abc",
		},
	}

	svc := NewStackService(newMockStackRepo(), gitCfgs, nil, nil, nil, nil, t.TempDir(), t.TempDir(), NewStackLocks(), nil, nil)
	err := svc.ClearCredentialField(context.Background(), "mystack", "token")
	require.NoError(t, err)

	cfg := gitCfgs.configs["mystack"]
	assert.Empty(t, cfg.Credentials.Token)
	assert.Equal(t, "AGE-SECRET-KEY-abc", cfg.Credentials.AgeKey)
	assert.Equal(t, stack.GitAuthNone, cfg.AuthMethod)
}

func TestClearCredentialField_InvalidField(t *testing.T) {
	gitCfgs := newMockGitConfigRepo()
	gitCfgs.configs["mystack"] = &stack.GitSource{
		RepoURL:     "https://github.com/test/repo",
		AuthMethod:  stack.GitAuthToken,
		Credentials: &stack.GitCredentials{Token: "ghp_abc"},
	}

	svc := NewStackService(newMockStackRepo(), gitCfgs, nil, nil, nil, nil, t.TempDir(), t.TempDir(), NewStackLocks(), nil, nil)
	err := svc.ClearCredentialField(context.Background(), "mystack", "bogus")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unknown credential field")
}

func TestClearCredentialField_NotFound(t *testing.T) {
	svc := NewStackService(newMockStackRepo(), newMockGitConfigRepo(), nil, nil, nil, nil, t.TempDir(), t.TempDir(), NewStackLocks(), nil, nil)
	err := svc.ClearCredentialField(context.Background(), "nonexistent", "token")
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestClearCredentialField_NilCreds(t *testing.T) {
	gitCfgs := newMockGitConfigRepo()
	gitCfgs.configs["mystack"] = &stack.GitSource{
		RepoURL:    "https://github.com/test/repo",
		AuthMethod: stack.GitAuthNone,
	}

	svc := NewStackService(newMockStackRepo(), gitCfgs, nil, nil, nil, nil, t.TempDir(), t.TempDir(), NewStackLocks(), nil, nil)
	err := svc.ClearCredentialField(context.Background(), "mystack", "token")
	assert.NoError(t, err, "nil credentials is a no-op")
}

// ---------------------------------------------------------------------------
// deriveStackStatus (pure function, no deps)
// ---------------------------------------------------------------------------

func TestDeriveStackStatus_NoContainers(t *testing.T) {
	assert.Equal(t, stack.StatusStopped, deriveStackStatus(nil))
}

// ---------------------------------------------------------------------------
// composeFor / clientFor -- loud-failure for host-pinned stacks
// ---------------------------------------------------------------------------

func TestComposeFor_NilHostID_ReturnsDefault(t *testing.T) {
	// nil HostID means local daemon. composeFor should return s.compose, nil.
	// s.compose may be nil (e.g. when docker is not configured) -- that's fine;
	// the contract is that it doesn't error for nil HostID.
	svc := NewStackService(newMockStackRepo(), newMockGitConfigRepo(), nil, nil, nil, nil, t.TempDir(), t.TempDir(), NewStackLocks(), nil, nil)
	st, _ := stack.NewStack("test", "/tmp/test", stack.SourceLocal)
	_, err := svc.composeFor(context.Background(), st)
	assert.NoError(t, err, "nil HostID must never error on composeFor")
}

func TestComposeFor_HostPinned_NoFactory(t *testing.T) {
	// Host-pinned stack but no factory configured -> hard error.
	svc := NewStackService(newMockStackRepo(), newMockGitConfigRepo(), nil, nil, nil, nil, t.TempDir(), t.TempDir(), NewStackLocks(), nil, nil)
	hostID := int64(42)
	st, _ := stack.NewStackWithHost("test", "/tmp/test", stack.SourceLocal, &hostID)
	_, err := svc.composeFor(context.Background(), st)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no host factory is configured")
}

func TestClientFor_NilHostID_ReturnsDefault(t *testing.T) {
	// nil HostID returns s.docker. When s.docker is nil, that's an error
	// (no docker client available) -- not a silent nil return.
	svc := NewStackService(newMockStackRepo(), newMockGitConfigRepo(), nil, nil, nil, nil, t.TempDir(), t.TempDir(), NewStackLocks(), nil, nil)
	st, _ := stack.NewStack("test", "/tmp/test", stack.SourceLocal)
	_, err := svc.clientFor(context.Background(), st)
	assert.Error(t, err, "nil HostID with nil docker must error")
	assert.Contains(t, err.Error(), "docker client not available")
}

func TestClientFor_HostPinned_NoFactory(t *testing.T) {
	svc := NewStackService(newMockStackRepo(), newMockGitConfigRepo(), nil, nil, nil, nil, t.TempDir(), t.TempDir(), NewStackLocks(), nil, nil)
	hostID := int64(42)
	st, _ := stack.NewStackWithHost("test", "/tmp/test", stack.SourceLocal, &hostID)
	_, err := svc.clientFor(context.Background(), st)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no host factory is configured")
}

// ---------------------------------------------------------------------------
// Stack List -- StatusUnknown on unreachable host
// ---------------------------------------------------------------------------

func TestList_StatusUnknown_WhenHostUnreachable(t *testing.T) {
	// A local stack with containers returns normally.
	// A host-pinned stack with a factory that can't resolve the host
	// must report StatusUnknown (never "stopped").
	repo := newMockStackRepo()
	localSt, _ := stack.NewStack("local-stack", "/tmp/local", stack.SourceLocal)
	hostID := int64(99)
	remoteSt, _ := stack.NewStackWithHost("remote-stack", "/tmp/remote", stack.SourceLocal, &hostID)
	require.NoError(t, repo.Create(context.Background(), localSt))
	require.NoError(t, repo.Create(context.Background(), remoteSt))

	svc := NewStackService(repo, newMockGitConfigRepo(), nil, nil, nil, nil, t.TempDir(), t.TempDir(), NewStackLocks(), nil, nil)

	stacks, err := svc.List(context.Background())
	require.NoError(t, err)
	require.Len(t, stacks, 2)

	for _, st := range stacks {
		if st.HostID != nil {
			assert.Equal(t, stack.StatusUnknown, st.Status, "host-pinned stack without reachable host must be StatusUnknown")
		}
	}
}

// ---------------------------------------------------------------------------
// SOPS decrypt failure propagation
// ---------------------------------------------------------------------------

// withSopsFakes swaps the sops function seams for deterministic outcomes and
// restores the real bindings when the test finishes.
func withSopsFakes(t *testing.T, available bool, envDecrypted bool, envErr error, compDecrypted bool, compErr error) {
	t.Helper()
	oldAvail, oldEnv, oldComp := sopsAvailable, sopsDecryptEnv, sopsDecryptComp
	t.Cleanup(func() { sopsAvailable, sopsDecryptEnv, sopsDecryptComp = oldAvail, oldEnv, oldComp })
	sopsAvailable = func() bool { return available }
	sopsDecryptEnv = func(_, _ string) (bool, error) { return envDecrypted, envErr }
	sopsDecryptComp = func(_, _ string) (bool, error) { return compDecrypted, compErr }
}

func TestDecryptSopsSecrets_SopsFailureReturnsError(t *testing.T) {
	withSopsFakes(t, true, false, errors.New("age: key not found"), false, nil)

	repo := newMockStackRepo()
	gitCfgs := newMockGitConfigRepo()
	stackPath := t.TempDir()
	st, err := stack.NewStackWithHost("sops-fail", stackPath, stack.SourceLocal, nil)
	require.NoError(t, err)
	require.NoError(t, repo.Create(context.Background(), st))
	gitCfgs.configs["sops-fail"] = &stack.GitSource{Credentials: &stack.GitCredentials{AgeKey: "AGE-SECRET-KEY-TEST"}}

	svc := NewStackService(repo, gitCfgs, nil, nil, nil, nil, t.TempDir(), t.TempDir(), NewStackLocks(), nil, nil)

	err = svc.decryptSopsSecrets(context.Background(), "sops-fail", stackPath)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "decrypting .env")
}

func TestDecryptSopsSecrets_NoOpPathsStayNil(t *testing.T) {
	repo := newMockStackRepo()
	gitCfgs := newMockGitConfigRepo()
	stackPath := t.TempDir()
	st, err := stack.NewStackWithHost("sops-noop", stackPath, stack.SourceLocal, nil)
	require.NoError(t, err)
	require.NoError(t, repo.Create(context.Background(), st))
	svc := NewStackService(repo, gitCfgs, nil, nil, nil, nil, t.TempDir(), t.TempDir(), NewStackLocks(), nil, nil)

	// sops binary absent: no decrypt is ever attempted.
	withSopsFakes(t, false, false, nil, false, nil)
	assert.NoError(t, svc.decryptSopsSecrets(context.Background(), "sops-noop", stackPath))

	// no age key: non-fatal skip.
	withSopsFakes(t, true, false, nil, false, nil)
	assert.NoError(t, svc.decryptSopsSecrets(context.Background(), "sops-noop", stackPath))

	// age key present but files are not SOPS-encrypted: nothing to decrypt.
	gitCfgs.configs["sops-noop"] = &stack.GitSource{Credentials: &stack.GitCredentials{AgeKey: "AGE-SECRET-KEY-TEST"}}
	withSopsFakes(t, true, false, nil, false, nil)
	assert.NoError(t, svc.decryptSopsSecrets(context.Background(), "sops-noop", stackPath))
}

func TestDeploy_SopsDecryptFailureAborts(t *testing.T) {
	withSopsFakes(t, true, false, errors.New("age: key not found"), false, nil)

	repo := newMockStackRepo()
	gitCfgs := newMockGitConfigRepo()
	stackPath := t.TempDir()
	require.NoError(t, os.MkdirAll(stackPath, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(stackPath, "compose.yaml"), []byte("services: {}\n"), 0600))
	require.NoError(t, os.WriteFile(filepath.Join(stackPath, ".env"), []byte("DB_PASSWORD=ENC[AES256_GCM,data=x]\n"), 0600))
	st, err := stack.NewStackWithHost("deploy-abort", stackPath, stack.SourceLocal, nil)
	require.NoError(t, err)
	require.NoError(t, repo.Create(context.Background(), st))
	gitCfgs.configs["deploy-abort"] = &stack.GitSource{Credentials: &stack.GitCredentials{AgeKey: "AGE-SECRET-KEY-TEST"}}

	svc := NewStackService(repo, gitCfgs, nil, nil, nil, nil, t.TempDir(), t.TempDir(), NewStackLocks(), nil, nil)

	result, err := svc.Deploy(context.Background(), "deploy-abort")
	require.Error(t, err)
	assert.Nil(t, result, "deploy must abort before touching docker")
	assert.Contains(t, err.Error(), "decrypting .env")
}

func TestCreate_SopsDecryptFailureRollsBack(t *testing.T) {
	withSopsFakes(t, true, false, errors.New("age: key not found"), false, nil)

	repo := newMockStackRepo()
	gitCfgs := newMockGitConfigRepo()
	stacksDir := t.TempDir()
	gitCfgs.configs["create-abort"] = &stack.GitSource{Credentials: &stack.GitCredentials{AgeKey: "AGE-SECRET-KEY-TEST"}}

	svc := NewStackService(repo, gitCfgs, nil, nil, nil, nil, stacksDir, t.TempDir(), NewStackLocks(), nil, nil)

	st, err := svc.Create(context.Background(), "create-abort", "services: {}\n", nil)
	require.Error(t, err)
	assert.Nil(t, st)

	got, _ := repo.GetByName(context.Background(), "create-abort")
	assert.Nil(t, got, "stack row must be rolled back on decrypt failure")
	_, statErr := os.Stat(filepath.Join(stacksDir, "create-abort"))
	assert.Error(t, statErr, "stack directory must be rolled back on decrypt failure")
}
