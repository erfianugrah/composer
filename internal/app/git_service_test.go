package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"

	"github.com/erfianugrah/composer/internal/domain/stack"
	gitinfra "github.com/erfianugrah/composer/internal/infra/git"
)

// newLocalGitStack builds a throwaway local bare origin plus a working clone
// containing compose.yaml and a SOPS-encrypted-looking .env. Pull against the
// local origin is a no-op success (AlreadyUpToDate), so SyncAndRedeploy
// reaches the SOPS decrypt block without any network.
func newLocalGitStack(t *testing.T) (stackPath, barePath, branch string) {
	t.Helper()
	tmp := t.TempDir()
	seedPath := filepath.Join(tmp, "seed")
	barePath = filepath.Join(tmp, "origin.git")
	stackPath = filepath.Join(tmp, "stack")

	seed, err := gogit.PlainInit(seedPath, false)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(seedPath, "compose.yaml"), []byte("services: {}\n"), 0600))
	require.NoError(t, os.WriteFile(filepath.Join(seedPath, ".env"), []byte("DB_PASSWORD=ENC[AES256_GCM,data=x]\n"), 0600))
	wt, err := seed.Worktree()
	require.NoError(t, err)
	for _, f := range []string{"compose.yaml", ".env"} {
		_, err = wt.Add(f)
		require.NoError(t, err)
	}
	_, err = wt.Commit("init", &gogit.CommitOptions{Author: &object.Signature{Name: "test", Email: "test@example.com", When: time.Now()}})
	require.NoError(t, err)

	head, err := seed.Head()
	require.NoError(t, err)
	branch = head.Name().Short()

	_, err = gogit.PlainInit(barePath, true)
	require.NoError(t, err)
	remote, err := seed.CreateRemote(&config.RemoteConfig{Name: "origin", URLs: []string{barePath}})
	require.NoError(t, err)
	require.NoError(t, remote.Push(&gogit.PushOptions{RefSpecs: []config.RefSpec{config.RefSpec("refs/heads/" + branch + ":refs/heads/" + branch)}}))

	_, err = gogit.PlainClone(stackPath, false, &gogit.CloneOptions{URL: barePath, ReferenceName: plumbing.NewBranchReferenceName(branch), RemoteName: "origin"})
	require.NoError(t, err)
	return stackPath, barePath, branch
}

func TestSyncAndRedeploy_SopsDecryptFailureAborts(t *testing.T) {
	stackPath, barePath, branch := newLocalGitStack(t)
	withSopsFakes(t, true, false, errors.New("age: key not found"), false, nil)

	repo := newMockStackRepo()
	gitCfgs := newMockGitConfigRepo()
	gitCfg := &stack.GitSource{
		RepoURL:     barePath,
		Branch:      branch,
		ComposePath: "compose.yaml",
		AutoSync:    true,
		Credentials: &stack.GitCredentials{AgeKey: "AGE-SECRET-KEY-TEST"},
	}
	require.NoError(t, gitCfgs.Upsert(context.Background(), "git-abort", gitCfg))
	st, err := stack.NewGitStackWithHost("git-abort", stackPath, gitCfg, nil)
	require.NoError(t, err)
	require.NoError(t, repo.Create(context.Background(), st))

	svc := NewGitService(repo, gitCfgs, gitinfra.NewClient(), nil, nil, nil, t.TempDir(), NewStackLocks(), nil, nil)

	action, err := svc.SyncAndRedeploy(context.Background(), "git-abort")
	require.Error(t, err, "a genuine decrypt failure must abort the redeploy")
	assert.Equal(t, "error", action)
	assert.Contains(t, err.Error(), "decrypting .env")
}
