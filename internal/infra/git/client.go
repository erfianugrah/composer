package git

import (
	"fmt"
	"strings"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/plumbing/transport"
	"github.com/go-git/go-git/v5/plumbing/transport/http"
	"github.com/go-git/go-git/v5/plumbing/transport/ssh"

	domstack "github.com/erfianugrah/composer/internal/domain/stack"
)

// buildAuth creates the transport.AuthMethod from git credentials.
func buildAuth(creds *domstack.GitCredentials) transport.AuthMethod {
	if creds == nil {
		return nil
	}
	if creds.SSHKey != "" {
		// SSH key auth: the key content is stored in the credentials
		keys, err := ssh.NewPublicKeys("git", []byte(creds.SSHKey), creds.SSHKeyPassphrase)
		if err != nil {
			return nil // fall back to no auth
		}
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

// Client wraps go-git for stack git operations.
type Client struct{}

func NewClient() *Client {
	return &Client{}
}

// Clone clones a git repository into the target directory.
func (c *Client) Clone(repoURL, branch, targetDir string, creds *domstack.GitCredentials) error {
	opts := &git.CloneOptions{
		URL:           repoURL,
		ReferenceName: plumbing.NewBranchReferenceName(branch),
		SingleBranch:  true,
		Depth:         0, // full clone for log/diff
		Auth:          buildAuth(creds),
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

	// Pull with credentials
	pullOpts := &git.PullOptions{
		RemoteName: "origin",
		Auth:       buildAuth(creds),
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

	// Push
	pushOpts := &git.PushOptions{Auth: buildAuth(creds)}

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
