package stack_test

import (
	"testing"
	"time"

	"github.com/erfianugrah/composer/internal/domain/stack"
)

func TestNewStack_Local(t *testing.T) {
	s, err := stack.NewStack("web-app", "/opt/stacks/web-app", stack.SourceLocal)
	if err != nil {
		t.Fatalf("NewStack() error: %v", err)
	}

	if s.Name != "web-app" {
		t.Errorf("Name = %q, want %q", s.Name, "web-app")
	}
	if s.Path != "/opt/stacks/web-app" {
		t.Errorf("Path = %q, want %q", s.Path, "/opt/stacks/web-app")
	}
	if s.Source != stack.SourceLocal {
		t.Errorf("Source = %q, want %q", s.Source, stack.SourceLocal)
	}
	if s.Status != stack.StatusUnknown {
		t.Errorf("Status = %q, want %q", s.Status, stack.StatusUnknown)
	}
	if s.GitConfig != nil {
		t.Error("GitConfig should be nil for local stack")
	}
	if s.CreatedAt.IsZero() {
		t.Error("expected non-zero CreatedAt")
	}
}

func TestNewStack_Git(t *testing.T) {
	git := &stack.GitSource{
		RepoURL:     "https://github.com/user/infra.git",
		Branch:      "main",
		ComposePath: "docker/compose.yaml",
		AutoSync:    true,
		AuthMethod:  stack.GitAuthNone,
	}
	s, err := stack.NewGitStack("infra", "/opt/stacks/infra", git)
	if err != nil {
		t.Fatalf("NewGitStack() error: %v", err)
	}

	if s.Source != stack.SourceGit {
		t.Errorf("Source = %q, want %q", s.Source, stack.SourceGit)
	}
	if s.GitConfig == nil {
		t.Fatal("GitConfig should not be nil for git stack")
	}
	if s.GitConfig.RepoURL != "https://github.com/user/infra.git" {
		t.Errorf("RepoURL = %q", s.GitConfig.RepoURL)
	}
	if s.GitConfig.Branch != "main" {
		t.Errorf("Branch = %q, want %q", s.GitConfig.Branch, "main")
	}
}

func TestNewStack_Validation(t *testing.T) {
	tests := []struct {
		name   string
		sName  string
		path   string
		source stack.Source
	}{
		{"empty name", "", "/opt/stacks/x", stack.SourceLocal},
		{"empty path", "x", "", stack.SourceLocal},
		{"invalid source", "x", "/opt/stacks/x", stack.Source("s3")},
		{"name with slash", "web/app", "/opt/stacks/x", stack.SourceLocal},
		{"name with space", "web app", "/opt/stacks/x", stack.SourceLocal},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := stack.NewStack(tt.sName, tt.path, tt.source)
			if err == nil {
				t.Error("expected error, got nil")
			}
		})
	}
}

func TestNewGitStack_Validation(t *testing.T) {
	tests := []struct {
		name string
		git  *stack.GitSource
	}{
		{"nil git config", nil},
		{"empty repo URL", &stack.GitSource{RepoURL: "", Branch: "main", ComposePath: "compose.yaml", AuthMethod: stack.GitAuthNone}},
		{"empty branch", &stack.GitSource{RepoURL: "https://github.com/x/y.git", Branch: "", ComposePath: "compose.yaml", AuthMethod: stack.GitAuthNone}},
		{"empty compose path", &stack.GitSource{RepoURL: "https://github.com/x/y.git", Branch: "main", ComposePath: "", AuthMethod: stack.GitAuthNone}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := stack.NewGitStack("test", "/opt/stacks/test", tt.git)
			if err == nil {
				t.Error("expected error, got nil")
			}
		})
	}
}

func TestStack_UpdateCompose(t *testing.T) {
	s, _ := stack.NewStack("web", "/opt/stacks/web", stack.SourceLocal)
	originalUpdated := s.UpdatedAt

	// Small sleep to ensure time.Now() advances
	time.Sleep(time.Millisecond)

	content := "services:\n  web:\n    image: nginx:latest\n"
	s.UpdateCompose(content)

	if s.ComposeContent != content {
		t.Errorf("ComposeContent = %q, want %q", s.ComposeContent, content)
	}
	if !s.UpdatedAt.After(originalUpdated) {
		t.Error("UpdatedAt should have advanced after UpdateCompose")
	}
}

func TestStack_SetStatus(t *testing.T) {
	s, _ := stack.NewStack("web", "/opt/stacks/web", stack.SourceLocal)

	s.SetStatus(stack.StatusRunning)
	if s.Status != stack.StatusRunning {
		t.Errorf("Status = %q, want %q", s.Status, stack.StatusRunning)
	}

	s.SetStatus(stack.StatusStopped)
	if s.Status != stack.StatusStopped {
		t.Errorf("Status = %q, want %q", s.Status, stack.StatusStopped)
	}
}
