package git_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/erfianugrah/composer/internal/infra/git"
)

// setupBareRepo creates a bare git repo with a compose.yaml and returns its path.
func setupBareRepo(t *testing.T) string {
	t.Helper()

	// Create a temp dir for the bare repo
	bareDir := filepath.Join(t.TempDir(), "test-repo.git")
	workDir := filepath.Join(t.TempDir(), "work")

	// Init bare repo with 'main' as default branch
	run(t, "", "git", "init", "--bare", "--initial-branch=main", bareDir)

	// Clone it to a working directory, add a compose file, commit, push
	run(t, "", "git", "clone", bareDir, workDir)
	run(t, workDir, "git", "config", "user.email", "test@example.com")
	run(t, workDir, "git", "config", "user.name", "Test")

	// Create initial compose.yaml
	compose := "services:\n  web:\n    image: nginx:alpine\n"
	require.NoError(t, os.WriteFile(filepath.Join(workDir, "compose.yaml"), []byte(compose), 0644))
	run(t, workDir, "git", "add", ".")
	run(t, workDir, "git", "commit", "-m", "initial commit")
	run(t, workDir, "git", "push", "origin", "main")

	// Add a second commit
	compose2 := "services:\n  web:\n    image: nginx:latest\n    ports:\n      - \"8080:80\"\n"
	require.NoError(t, os.WriteFile(filepath.Join(workDir, "compose.yaml"), []byte(compose2), 0644))
	run(t, workDir, "git", "add", ".")
	run(t, workDir, "git", "commit", "-m", "update nginx to latest, add port")
	run(t, workDir, "git", "push", "origin", "main")

	return bareDir
}

func run(t *testing.T, dir string, name string, args ...string) {
	t.Helper()
	cmd := exec.Command(name, args...)
	if dir != "" {
		cmd.Dir = dir
	}
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("command %s %v failed: %v\n%s", name, args, err, out)
	}
}

func TestGitClient_CloneAndLog(t *testing.T) {
	bareRepo := setupBareRepo(t)
	cloneDir := filepath.Join(t.TempDir(), "clone")

	client := git.NewClient()

	// Clone
	err := client.Clone(bareRepo, "main", cloneDir, nil)
	require.NoError(t, err)

	// Verify file exists
	content, err := os.ReadFile(filepath.Join(cloneDir, "compose.yaml"))
	require.NoError(t, err)
	assert.Contains(t, string(content), "nginx:latest")

	// IsRepo
	assert.True(t, client.IsRepo(cloneDir))
	assert.False(t, client.IsRepo(t.TempDir()))

	// HeadSHA
	sha, err := client.HeadSHA(cloneDir)
	require.NoError(t, err)
	assert.Len(t, sha, 40)

	// Log
	commits, err := client.Log(cloneDir, "compose.yaml", 10)
	require.NoError(t, err)
	require.Len(t, commits, 2)
	assert.Equal(t, "update nginx to latest, add port", commits[0].Message)
	assert.Equal(t, "initial commit", commits[1].Message)
	assert.Equal(t, "Test", commits[0].Author)
	assert.Len(t, commits[0].ShortSHA, 7)
}

func TestGitClient_PullNoChanges(t *testing.T) {
	bareRepo := setupBareRepo(t)
	cloneDir := filepath.Join(t.TempDir(), "clone")

	client := git.NewClient()
	require.NoError(t, client.Clone(bareRepo, "main", cloneDir, nil))

	// Pull when already up to date
	changed, sha, err := client.Pull(cloneDir, "compose.yaml", nil)
	require.NoError(t, err)
	assert.False(t, changed)
	assert.Len(t, sha, 40)
}

func TestGitClient_PullWithChanges(t *testing.T) {
	bareRepo := setupBareRepo(t)
	cloneDir := filepath.Join(t.TempDir(), "clone")
	pushDir := filepath.Join(t.TempDir(), "push")

	client := git.NewClient()
	require.NoError(t, client.Clone(bareRepo, "main", cloneDir, nil))

	// Clone again to a "pusher" directory and make a change
	run(t, "", "git", "clone", bareRepo, pushDir)
	run(t, pushDir, "git", "config", "user.email", "test@example.com")
	run(t, pushDir, "git", "config", "user.name", "Test")

	newCompose := "services:\n  web:\n    image: httpd:alpine\n"
	require.NoError(t, os.WriteFile(filepath.Join(pushDir, "compose.yaml"), []byte(newCompose), 0644))
	run(t, pushDir, "git", "add", ".")
	run(t, pushDir, "git", "commit", "-m", "switch to httpd")
	run(t, pushDir, "git", "push", "origin", "main")

	// Now pull in the original clone
	changed, sha, err := client.Pull(cloneDir, "compose.yaml", nil)
	require.NoError(t, err)
	assert.True(t, changed)
	assert.Len(t, sha, 40)

	// Verify the file updated
	content, err := os.ReadFile(filepath.Join(cloneDir, "compose.yaml"))
	require.NoError(t, err)
	assert.Contains(t, string(content), "httpd:alpine")
}

func TestGitClient_LogLimit(t *testing.T) {
	bareRepo := setupBareRepo(t)
	cloneDir := filepath.Join(t.TempDir(), "clone")

	client := git.NewClient()
	require.NoError(t, client.Clone(bareRepo, "main", cloneDir, nil))

	// Limit to 1 commit
	commits, err := client.Log(cloneDir, "compose.yaml", 1)
	require.NoError(t, err)
	assert.Len(t, commits, 1)
	assert.Equal(t, "update nginx to latest, add port", commits[0].Message)
}

func TestGitClient_CloneInvalidURL(t *testing.T) {
	client := git.NewClient()
	err := client.Clone("https://invalid.example.com/nonexistent.git", "main", t.TempDir(), nil)
	assert.Error(t, err)
}
