package git

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/plumbing/transport"
	"github.com/go-git/go-git/v5/plumbing/transport/http"
	"github.com/go-git/go-git/v5/plumbing/transport/ssh"
	sshlib "golang.org/x/crypto/ssh"

	"github.com/erfianugrah/composer/internal/infra/crypto"

	domstack "github.com/erfianugrah/composer/internal/domain/stack"
)

// hostKeyCallback returns an SSH host key callback.
// Uses known_hosts file if available, falls back to insecure if COMPOSER_SSH_INSECURE_HOST_KEY=true.
func hostKeyCallback() sshlib.HostKeyCallback {
	// Try known_hosts files
	for _, path := range []string{
		"/home/composer/.ssh/known_hosts",
		filepath.Join(os.Getenv("HOME"), ".ssh", "known_hosts"),
	} {
		if _, err := os.Stat(path); err == nil {
			cb, err := ssh.NewKnownHostsCallback(path)
			if err == nil {
				return cb
			}
		}
	}
	// Fallback: only if explicitly opted in
	if os.Getenv("COMPOSER_SSH_INSECURE_HOST_KEY") == "true" {
		return hostKeyCallback()
	}
	// Default: insecure (backwards compat, but known_hosts shipped in Docker image)
	return hostKeyCallback()
}

// isAllowedSSHKeyPath validates that a key file path is within allowed SSH directories.
// Prevents arbitrary file reads via the SSHKeyFile credential field.
func isAllowedSSHKeyPath(path string) bool {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return false
	}
	allowedDirs := []string{
		filepath.Join(os.Getenv("HOME"), ".ssh"),
		"/home/composer/.ssh",
	}
	for _, dir := range allowedDirs {
		resolved, _ := filepath.EvalSymlinks(dir)
		if resolved == "" {
			resolved = dir
		}
		if strings.HasPrefix(absPath, resolved+string(filepath.Separator)) || strings.HasPrefix(absPath, dir+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

// buildAuth creates the transport.AuthMethod from git credentials.
func buildAuth(creds *domstack.GitCredentials) transport.AuthMethod {
	if creds == nil {
		return nil
	}
	// Per-stack SSH key file takes priority over inline key
	// Path restricted to SSH directories to prevent arbitrary file reads (S11)
	if creds.SSHKeyFile != "" && isAllowedSSHKeyPath(creds.SSHKeyFile) {
		keyContent, err := crypto.DecryptFile(creds.SSHKeyFile)
		if err == nil && keyContent != "" {
			keys, err := ssh.NewPublicKeys("git", []byte(keyContent), creds.SSHKeyPassphrase)
			if err == nil {
				keys.HostKeyCallback = hostKeyCallback()
				return keys
			}
		}
	}
	if creds.SSHKey != "" {
		// SSH key auth: the key content is stored in the credentials
		keys, err := ssh.NewPublicKeys("git", []byte(creds.SSHKey), creds.SSHKeyPassphrase)
		if err != nil {
			return nil // fall back to no auth
		}
		// Accept any host key (container environment, no known_hosts)
		keys.HostKeyCallback = hostKeyCallback()
		return keys
	}
	if creds.Token != "" {
		return &http.BasicAuth{
			Username: "x-access-token", // works for GitHub, GitLab, Gitea
			Password: creds.Token,
		}
	}
	if creds.Username != "" {
		return &http.BasicAuth{
			Username: creds.Username,
			Password: creds.Password,
		}
	}
	return nil
}

// loadGlobalGitToken reads the global git token from COMPOSER_DATA_DIR/git-token.
// Returns empty string if not configured.
func loadGlobalGitToken() string {
	dataDir := os.Getenv("COMPOSER_DATA_DIR")
	if dataDir == "" {
		dataDir = "/opt/composer"
	}
	tokenPath := filepath.Join(dataDir, "git-token")
	token, err := crypto.DecryptFile(tokenPath)
	if err != nil || token == "" {
		return ""
	}
	return strings.TrimSpace(token)
}

// buildSSHAuthFromAgent tries SSH key files in standard locations.
// Scans all files in ~/.ssh/ and /home/composer/.ssh/ (not just id_ed25519/id_rsa).
// Key files are transparently decrypted if encrypted at rest.
// Skips .pub, known_hosts, config, and authorized_keys.
func buildSSHAuthFromAgent() transport.AuthMethod {
	for _, sshDir := range []string{
		filepath.Join(os.Getenv("HOME"), ".ssh"),
		"/home/composer/.ssh",
	} {
		entries, err := os.ReadDir(sshDir)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			name := entry.Name()
			// Skip non-key files
			if name == "known_hosts" || name == "config" || name == "authorized_keys" ||
				strings.HasSuffix(name, ".pub") {
				continue
			}
			keyPath := filepath.Join(sshDir, name)
			keyContent, err := crypto.DecryptFile(keyPath)
			if err != nil || keyContent == "" {
				continue
			}
			keys, err := ssh.NewPublicKeys("git", []byte(keyContent), "")
			if err == nil {
				keys.HostKeyCallback = hostKeyCallback()
				return keys
			}
		}
	}
	return nil
}

// Client wraps go-git for stack git operations.
type Client struct{}

func NewClient() *Client {
	return &Client{}
}

// isSSHURL returns true if the URL uses SSH protocol.
func isSSHURL(url string) bool {
	return strings.HasPrefix(url, "git@") || strings.HasPrefix(url, "ssh://")
}

// Clone clones a git repository into the target directory.
// Supports both HTTPS and SSH URLs. For SSH without explicit credentials,
// tries the SSH agent and default key files (~/.ssh/id_ed25519, id_rsa).
func (c *Client) Clone(repoURL, branch, targetDir string, creds *domstack.GitCredentials) error {
	auth := buildAuth(creds)

	// Fallback chain when no per-stack credentials matched
	if auth == nil {
		if isSSHURL(repoURL) {
			auth = buildSSHAuthFromAgent()
		} else if token := loadGlobalGitToken(); token != "" {
			auth = &http.BasicAuth{Username: "x-access-token", Password: token}
		}
	}

	opts := &git.CloneOptions{
		URL:           repoURL,
		ReferenceName: plumbing.NewBranchReferenceName(branch),
		SingleBranch:  true,
		Depth:         0, // full clone for log/diff
		Auth:          auth,
	}

	_, err := git.PlainClone(targetDir, false, opts)
	if err != nil {
		return fmt.Errorf("cloning %s: %w", repoURL, err)
	}
	return nil
}

// Pull pulls the latest changes from the remote. Returns whether compose file changed.
func (c *Client) Pull(stackDir, composePath string, creds *domstack.GitCredentials) (changed bool, newSHA string, err error) {
	repo, err := git.PlainOpen(stackDir)
	if err != nil {
		return false, "", fmt.Errorf("opening repo: %w", err)
	}

	wt, err := repo.Worktree()
	if err != nil {
		return false, "", fmt.Errorf("getting worktree: %w", err)
	}

	// Get current HEAD
	oldHead, err := repo.Head()
	if err != nil {
		return false, "", fmt.Errorf("getting HEAD: %w", err)
	}
	oldSHA := oldHead.Hash().String()

	// Pull with credentials -- fallback chain: per-stack → global SSH → global token
	auth := buildAuth(creds)
	if auth == nil {
		remote, _ := repo.Remote("origin")
		if remote != nil && len(remote.Config().URLs) > 0 {
			remoteURL := remote.Config().URLs[0]
			if isSSHURL(remoteURL) {
				auth = buildSSHAuthFromAgent()
				if auth == nil {
					return false, "", fmt.Errorf("SSH repo requires SSH keys. Add keys via Settings > SSH Keys, or switch to HTTPS URL with a token")
				}
			} else if token := loadGlobalGitToken(); token != "" {
				auth = &http.BasicAuth{Username: "x-access-token", Password: token}
			}
		}
	}
	pullOpts := &git.PullOptions{
		RemoteName: "origin",
		Auth:       auth,
	}
	err = wt.Pull(pullOpts)
	if err == git.NoErrAlreadyUpToDate {
		return false, oldSHA, nil
	}
	if err != nil {
		return false, oldSHA, fmt.Errorf("pulling: %w", err)
	}

	// Get new HEAD
	newHead, err := repo.Head()
	if err != nil {
		return false, "", fmt.Errorf("getting new HEAD: %w", err)
	}
	newSHA = newHead.Hash().String()

	// Check if compose file changed between old and new HEAD
	changed, err = fileChangedBetweenCommits(repo, oldHead.Hash(), newHead.Hash(), composePath)
	if err != nil {
		// If diff fails, assume changed to be safe
		return true, newSHA, nil
	}

	return changed, newSHA, nil
}

// Log returns recent commits that touched the compose file.
func (c *Client) Log(stackDir, composePath string, limit int) ([]CommitInfo, error) {
	repo, err := git.PlainOpen(stackDir)
	if err != nil {
		return nil, fmt.Errorf("opening repo: %w", err)
	}

	logOpts := &git.LogOptions{
		PathFilter: func(path string) bool {
			return path == composePath
		},
	}

	iter, err := repo.Log(logOpts)
	if err != nil {
		return nil, fmt.Errorf("getting log: %w", err)
	}
	defer iter.Close()

	var commits []CommitInfo
	err = iter.ForEach(func(commit *object.Commit) error {
		if len(commits) >= limit {
			return fmt.Errorf("limit reached")
		}
		commits = append(commits, CommitInfo{
			SHA:      commit.Hash.String(),
			ShortSHA: commit.Hash.String()[:7],
			Message:  strings.TrimSpace(commit.Message),
			Author:   commit.Author.Name,
			Date:     commit.Author.When,
		})
		return nil
	})

	// "limit reached" is not a real error
	if err != nil && err.Error() != "limit reached" {
		return nil, err
	}

	return commits, nil
}

// Checkout checks out a specific commit.
func (c *Client) Checkout(stackDir, commitSHA string) error {
	repo, err := git.PlainOpen(stackDir)
	if err != nil {
		return fmt.Errorf("opening repo: %w", err)
	}

	wt, err := repo.Worktree()
	if err != nil {
		return fmt.Errorf("getting worktree: %w", err)
	}

	return wt.Checkout(&git.CheckoutOptions{
		Hash: plumbing.NewHash(commitSHA),
	})
}

// CommitAndPush commits changes to the compose file and pushes to remote.
func (c *Client) CommitAndPush(stackDir, composePath, message, authorName, authorEmail string, creds *domstack.GitCredentials) (string, error) {
	repo, err := git.PlainOpen(stackDir)
	if err != nil {
		return "", fmt.Errorf("opening repo: %w", err)
	}

	wt, err := repo.Worktree()
	if err != nil {
		return "", fmt.Errorf("getting worktree: %w", err)
	}

	// Stage the compose file
	if _, err := wt.Add(composePath); err != nil {
		return "", fmt.Errorf("staging %s: %w", composePath, err)
	}

	// Commit
	commit, err := wt.Commit(message, &git.CommitOptions{
		Author: &object.Signature{
			Name:  authorName,
			Email: authorEmail,
			When:  time.Now(),
		},
	})
	if err != nil {
		return "", fmt.Errorf("committing: %w", err)
	}

	// Push -- per-stack creds → global token fallback
	pushAuth := buildAuth(creds)
	if pushAuth == nil {
		if token := loadGlobalGitToken(); token != "" {
			pushAuth = &http.BasicAuth{Username: "x-access-token", Password: token}
		}
	}
	pushOpts := &git.PushOptions{Auth: pushAuth}

	if err := repo.Push(pushOpts); err != nil && err != git.NoErrAlreadyUpToDate {
		return commit.String(), fmt.Errorf("pushing: %w", err)
	}

	return commit.String(), nil
}

// HeadSHA returns the current HEAD commit SHA.
func (c *Client) HeadSHA(stackDir string) (string, error) {
	repo, err := git.PlainOpen(stackDir)
	if err != nil {
		return "", err
	}
	head, err := repo.Head()
	if err != nil {
		return "", err
	}
	return head.Hash().String(), nil
}

// IsRepo returns true if the directory is a git repository.
func (c *Client) IsRepo(dir string) bool {
	_, err := git.PlainOpen(dir)
	return err == nil
}

// CommitInfo holds information about a git commit.
type CommitInfo struct {
	SHA      string    `json:"sha"`
	ShortSHA string    `json:"short_sha"`
	Message  string    `json:"message"`
	Author   string    `json:"author"`
	Date     time.Time `json:"date"`
}

// fileChangedBetween checks if a specific file changed between two commits.
func fileChangedBetweenCommits(repo *git.Repository, oldHash, newHash plumbing.Hash, filePath string) (bool, error) {
	oldCommit, err := repo.CommitObject(oldHash)
	if err != nil {
		return false, err
	}
	newCommit, err := repo.CommitObject(newHash)
	if err != nil {
		return false, err
	}

	oldTree, err := oldCommit.Tree()
	if err != nil {
		return false, err
	}
	newTree, err := newCommit.Tree()
	if err != nil {
		return false, err
	}

	// Get old file hash
	oldEntry, err := oldTree.FindEntry(filePath)
	if err != nil {
		// File didn't exist before -> it changed
		return true, nil
	}

	// Get new file hash
	newEntry, err := newTree.FindEntry(filePath)
	if err != nil {
		// File was deleted -> it changed
		return true, nil
	}

	return oldEntry.Hash != newEntry.Hash, nil
}

// DiffLine represents a single line in a diff output.
type DiffLine struct {
	Type    string // "add", "remove", "context"
	Content string
	OldLine int
	NewLine int
}

// WorkingDiff compares the working tree version of a file against HEAD.
// Returns the diff lines showing what changed.
func (c *Client) WorkingDiff(stackDir, composePath string) ([]DiffLine, bool, error) {
	repo, err := git.PlainOpen(stackDir)
	if err != nil {
		return nil, false, fmt.Errorf("opening repo: %w", err)
	}

	// Get committed content from HEAD
	head, err := repo.Head()
	if err != nil {
		return nil, false, fmt.Errorf("getting HEAD: %w", err)
	}

	commit, err := repo.CommitObject(head.Hash())
	if err != nil {
		return nil, false, fmt.Errorf("getting commit: %w", err)
	}

	tree, err := commit.Tree()
	if err != nil {
		return nil, false, fmt.Errorf("getting tree: %w", err)
	}

	var committedContent string
	entry, err := tree.File(composePath)
	if err == nil {
		committedContent, _ = entry.Contents()
	}

	// Get working tree content from disk
	diskPath := filepath.Join(stackDir, composePath)
	diskBytes, err := os.ReadFile(diskPath)
	if err != nil {
		return nil, false, fmt.Errorf("reading disk file: %w", err)
	}
	workingContent := string(diskBytes)

	if committedContent == workingContent {
		return nil, false, nil
	}

	// Produce a simple line-by-line diff
	oldLines := strings.Split(committedContent, "\n")
	newLines := strings.Split(workingContent, "\n")

	var diff []DiffLine
	oi, ni := 0, 0
	for oi < len(oldLines) || ni < len(newLines) {
		if oi < len(oldLines) && ni < len(newLines) && oldLines[oi] == newLines[ni] {
			diff = append(diff, DiffLine{Type: "context", Content: oldLines[oi], OldLine: oi + 1, NewLine: ni + 1})
			oi++
			ni++
		} else if oi < len(oldLines) && (ni >= len(newLines) || !containsLine(newLines[ni:], oldLines[oi])) {
			diff = append(diff, DiffLine{Type: "remove", Content: oldLines[oi], OldLine: oi + 1})
			oi++
		} else {
			diff = append(diff, DiffLine{Type: "add", Content: newLines[ni], NewLine: ni + 1})
			ni++
		}
	}

	return diff, true, nil
}

// IsDirty returns true if the working tree has uncommitted changes to the compose file.
func (c *Client) IsDirty(stackDir, composePath string) bool {
	_, hasDiff, err := c.WorkingDiff(stackDir, composePath)
	if err != nil {
		return false // can't determine, assume clean
	}
	return hasDiff
}

// containsLine checks if a line exists in the remaining slice (simple lookahead).
func containsLine(lines []string, target string) bool {
	for i, l := range lines {
		if l == target && i < 10 { // only look ahead 10 lines
			return true
		}
	}
	return false
}
