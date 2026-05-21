// Package registry holds the domain model for Docker registry credentials.
//
// Two scopes:
//   - Global: stack_name == "". Applied to every stack's docker compose pull/up.
//   - Per-stack: stack_name != "". Overrides the global entry for the same
//     registry on that one stack.
//
// Multiple registries are supported by storing one row per registry: a stack
// with images on both ghcr.io and a private mirror just gets two credentials
// merged into its DOCKER_CONFIG before pull/up.
package registry

import (
	"context"
	"errors"
	"strings"
	"time"
)

// Credential is a registry auth entry.
type Credential struct {
	ID        int64
	Registry  string // e.g. "ghcr.io", "docker.io", "registry.example.com:5000"
	Username  string
	Secret    string // password or PAT, stored encrypted at rest
	Email     string
	StackName string // "" = global
	CreatedAt time.Time
	UpdatedAt time.Time
}

// IsGlobal reports whether the credential applies to all stacks.
func (c *Credential) IsGlobal() bool { return c.StackName == "" }

// Validate enforces the minimum invariants before persisting.
func (c *Credential) Validate() error {
	if strings.TrimSpace(c.Registry) == "" {
		return errors.New("registry is required")
	}
	if strings.TrimSpace(c.Username) == "" {
		return errors.New("username is required")
	}
	if c.Secret == "" {
		return errors.New("secret is required")
	}
	return nil
}

// Repository is the persistence contract for credentials.
type Repository interface {
	// Upsert inserts a new credential or updates an existing one. The unique
	// key is (registry, stack_name); supplying ID==0 triggers an insert.
	Upsert(ctx context.Context, c *Credential) error
	// Delete removes a credential by surrogate ID.
	Delete(ctx context.Context, id int64) error
	// GetByID fetches a single credential. Returns nil, nil when missing.
	GetByID(ctx context.Context, id int64) (*Credential, error)
	// List returns every credential (admin view). Caller must redact secrets.
	List(ctx context.Context) ([]*Credential, error)
	// ListGlobal returns only global credentials (stack_name == "").
	ListGlobal(ctx context.Context) ([]*Credential, error)
	// ListForStack returns per-stack credentials scoped to stackName.
	// Does not include global credentials — merge with ListGlobal at the call site.
	ListForStack(ctx context.Context, stackName string) ([]*Credential, error)
}

// Resolve merges global and per-stack credentials. Per-stack entries win on
// registry-name collision so a stack can override a shared global PAT.
func Resolve(global, perStack []*Credential) []*Credential {
	merged := make(map[string]*Credential, len(global)+len(perStack))
	for _, c := range global {
		merged[c.Registry] = c
	}
	for _, c := range perStack {
		merged[c.Registry] = c
	}
	out := make([]*Credential, 0, len(merged))
	for _, c := range merged {
		out = append(out, c)
	}
	return out
}
