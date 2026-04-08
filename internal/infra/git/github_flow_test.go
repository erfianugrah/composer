package git_test

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/erfianugrah/composer/internal/infra/git"
)

// TestGitHubWebhookFlow tests the full flow:
// 1. Create a bare repo with a compose file
// 2. Clone it (simulating CreateGitStack)
// 3. Push a change from another clone (simulating a developer push)
// 4. Validate a simulated GitHub webhook signature
// 5. Parse the payload to extract branch + commit
// 6. Pull to detect the compose change
func TestGitHubWebhookFlow(t *testing.T) {
	// --- Setup: create bare repo with compose.yaml ---
	bareDir := filepath.Join(t.TempDir(), "repo.git")
	devDir := filepath.Join(t.TempDir(), "developer")
	composerDir := filepath.Join(t.TempDir(), "composer-clone")

	// Init bare repo with 'main' as default branch
	runCmd(t, "", "git", "init", "--bare", "--initial-branch=main", bareDir)

	// Developer clones and pushes initial compose
	runCmd(t, "", "git", "clone", bareDir, devDir)
	runCmd(t, devDir, "git", "config", "user.email", "dev@example.com")
	runCmd(t, devDir, "git", "config", "user.name", "Developer")

	compose := "services:\n  web:\n    image: nginx:alpine\n"
	require.NoError(t, os.WriteFile(filepath.Join(devDir, "compose.yaml"), []byte(compose), 0644))
	runCmd(t, devDir, "git", "add", ".")
	runCmd(t, devDir, "git", "commit", "-m", "initial compose")
	runCmd(t, devDir, "git", "push", "origin", "main")

	// --- Step 1: Composer clones (simulates CreateGitStack) ---
	client := git.NewClient()
	require.NoError(t, client.Clone(bareDir, "main", composerDir, nil))

	oldSHA, err := client.HeadSHA(composerDir)
	require.NoError(t, err)
	t.Logf("Initial HEAD: %s", oldSHA[:7])

	// --- Step 2: Developer pushes a compose change ---
	newCompose := "services:\n  web:\n    image: nginx:latest\n    ports:\n      - '8080:80'\n"
	require.NoError(t, os.WriteFile(filepath.Join(devDir, "compose.yaml"), []byte(newCompose), 0644))
	runCmd(t, devDir, "git", "add", ".")
	runCmd(t, devDir, "git", "commit", "-m", "update to nginx:latest with port")
	runCmd(t, devDir, "git", "push", "origin", "main")

	// Get the new commit SHA (from dev's perspective)
	newSHABytes, _ := exec.Command("git", "-C", devDir, "rev-parse", "HEAD").Output()
	pushSHA := string(newSHABytes[:40])
	t.Logf("Developer pushed: %s", pushSHA[:7])

	// --- Step 3: Simulate GitHub webhook payload ---
	webhookSecret := "test-webhook-secret-123"

	payload := map[string]any{
		"ref":   "refs/heads/main",
		"after": pushSHA,
		"head_commit": map[string]any{
			"id":      pushSHA,
			"message": "update to nginx:latest with port",
		},
	}
	body, err := json.Marshal(payload)
	require.NoError(t, err)

	// Compute GitHub HMAC signature
	mac := hmac.New(sha256.New, []byte(webhookSecret))
	mac.Write(body)
	signature := "sha256=" + hex.EncodeToString(mac.Sum(nil))

	headers := map[string]string{
		"x-github-event":      "push",
		"x-hub-signature-256": signature,
	}

	// --- Step 4: Validate signature ---
	assert.True(t, git.ValidateSignature(git.ProviderGitHub, webhookSecret, headers, body),
		"webhook signature should validate")

	// Wrong secret should fail
	assert.False(t, git.ValidateSignature(git.ProviderGitHub, "wrong-secret", headers, body),
		"wrong secret should fail validation")

	// --- Step 5: Parse payload ---
	parsed, err := git.ParsePayload(git.ProviderGitHub, headers, body)
	require.NoError(t, err)
	assert.Equal(t, "push", parsed.Event)
	assert.Equal(t, "main", parsed.Branch)
	assert.Equal(t, pushSHA, parsed.CommitSHA)
	t.Logf("Parsed webhook: event=%s branch=%s commit=%s", parsed.Event, parsed.Branch, parsed.CommitSHA[:7])

	// --- Step 6: Pull and detect change ---
	changed, newSHA, err := client.Pull(composerDir, "compose.yaml", nil)
	require.NoError(t, err)
	assert.True(t, changed, "compose.yaml should have changed")
	assert.Equal(t, pushSHA, newSHA, "new SHA should match pushed commit")
	t.Logf("Pulled: changed=%v newSHA=%s", changed, newSHA[:7])

	// Verify the file content updated
	content, err := os.ReadFile(filepath.Join(composerDir, "compose.yaml"))
	require.NoError(t, err)
	assert.Contains(t, string(content), "nginx:latest")
	assert.Contains(t, string(content), "8080:80")

	// --- Step 7: Second pull should show no changes ---
	changed2, _, err := client.Pull(composerDir, "compose.yaml", nil)
	require.NoError(t, err)
	assert.False(t, changed2, "no changes on second pull")
}

// TestGitHubWebhookBranchFilter tests that webhook branch filtering works.
func TestGitHubWebhookBranchFilter(t *testing.T) {
	body := []byte(`{"ref":"refs/heads/develop","after":"abc123","head_commit":{"id":"abc123"}}`)
	headers := map[string]string{"x-github-event": "push"}

	parsed, err := git.ParsePayload(git.ProviderGitHub, headers, body)
	require.NoError(t, err)
	assert.Equal(t, "develop", parsed.Branch)

	// Simulate branch filter check (as webhook handler does)
	branchFilter := "main"
	assert.NotEqual(t, branchFilter, parsed.Branch, "develop != main, should be filtered")

	// Matching branch
	branchFilter2 := "develop"
	assert.Equal(t, branchFilter2, parsed.Branch, "develop == develop, should pass filter")
}

// TestGitHubPingEvent tests handling of GitHub's ping event (sent on webhook creation).
func TestGitHubPingEvent(t *testing.T) {
	body := []byte(`{"zen":"Approachable is better than simple.","hook_id":12345}`)
	headers := map[string]string{"x-github-event": "ping"}

	parsed, err := git.ParsePayload(git.ProviderGitHub, headers, body)
	require.NoError(t, err)
	assert.Equal(t, "ping", parsed.Event)
	// Ping events don't have ref/branch -- these will be empty
	assert.Empty(t, parsed.Branch)
}

func runCmd(t *testing.T, dir string, name string, args ...string) {
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
