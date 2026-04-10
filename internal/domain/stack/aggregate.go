package stack

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

// Stack is the aggregate root for Docker Compose stack management.
type Stack struct {
	Name           string
	Path           string
	Source         Source
	Status         Status
	ComposeContent string
	GitConfig      *GitSource
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// GitAuthMethod defines how to authenticate with a git remote.
type GitAuthMethod string

const (
	GitAuthNone    GitAuthMethod = "none"
	GitAuthToken   GitAuthMethod = "token"
	GitAuthSSH     GitAuthMethod = "ssh_key"
	GitAuthSSHFile GitAuthMethod = "ssh_file"
	GitAuthBasic   GitAuthMethod = "basic"
)

// GitSource holds the configuration for a git-backed stack.
type GitSource struct {
	RepoURL       string
	Branch        string
	ComposePath   string
	AutoSync      bool
	AuthMethod    GitAuthMethod
	Credentials   *GitCredentials
	LastSyncAt    *time.Time
	LastCommitSHA string
	SyncStatus    GitSyncStatus
}

// GitCredentials holds credential data for git authentication.
type GitCredentials struct {
	Token            string `json:"token,omitempty"`
	SSHKey           string `json:"ssh_key,omitempty"`            // PEM-encoded private key content (inline)
	SSHKeyFile       string `json:"ssh_key_file,omitempty"`       // path to SSH key file on disk (per-stack override)
	SSHKeyPassphrase string `json:"ssh_key_passphrase,omitempty"` // optional passphrase for the SSH key
	Username         string `json:"username,omitempty"`
	Password         string `json:"password,omitempty"`
	AgeKey           string `json:"age_key,omitempty"` // per-stack age private key for SOPS decryption
}

// NewStack creates a new local stack.
func NewStack(name, path string, source Source) (*Stack, error) {
	if err := validateName(name); err != nil {
		return nil, err
	}
	if path == "" {
		return nil, errors.New("stack path is required")
	}
	if !source.Valid() {
		return nil, fmt.Errorf("invalid source %q", source)
	}

	now := time.Now().UTC()
	return &Stack{
		Name:      name,
		Path:      path,
		Source:    source,
		Status:    StatusUnknown,
		CreatedAt: now,
		UpdatedAt: now,
	}, nil
}

// NewGitStack creates a new git-backed stack.
func NewGitStack(name, path string, gitConfig *GitSource) (*Stack, error) {
	if gitConfig == nil {
		return nil, errors.New("git config is required for git-backed stacks")
	}
	if err := validateGitSource(gitConfig); err != nil {
		return nil, fmt.Errorf("invalid git config: %w", err)
	}

	s, err := NewStack(name, path, SourceGit)
	if err != nil {
		return nil, err
	}

	gitConfig.SyncStatus = GitSynced
	s.GitConfig = gitConfig
	return s, nil
}

// UpdateCompose sets the compose content and advances the updated timestamp.
func (s *Stack) UpdateCompose(content string) {
	s.ComposeContent = content
	s.UpdatedAt = time.Now().UTC()
}

// SetStatus updates the runtime status.
func (s *Stack) SetStatus(status Status) {
	s.Status = status
	s.UpdatedAt = time.Now().UTC()
}

// validateName checks that a stack name is filesystem-safe.
// Strict allowlist: alphanumeric start, then [a-zA-Z0-9._-], max 128 chars.
// Rejects ".." to prevent path traversal.
func validateName(name string) error {
	if name == "" {
		return errors.New("stack name is required")
	}
	if name == "." || name == ".." || strings.Contains(name, "..") {
		return errors.New("stack name cannot contain '..'")
	}
	if len(name) > 128 {
		return errors.New("stack name too long (max 128)")
	}
	for i, c := range name {
		if i == 0 {
			if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')) {
				return fmt.Errorf("stack name must start with alphanumeric, got %q", string(c))
			}
			continue
		}
		if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '.' || c == '_' || c == '-') {
			return fmt.Errorf("stack name %q contains invalid character %q; allowed: [a-zA-Z0-9._-]", name, string(c))
		}
	}
	return nil
}

// validateGitSource checks all required git fields.
func validateGitSource(g *GitSource) error {
	if g.RepoURL == "" {
		return errors.New("repo URL is required")
	}
	if g.Branch == "" {
		return errors.New("branch is required")
	}
	if g.ComposePath == "" {
		return errors.New("compose path is required")
	}
	return nil
}
