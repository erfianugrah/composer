package stack_test

import (
	"strings"
	"testing"

	"github.com/erfianugrah/composer/internal/domain/stack"
)

func TestNewStack_NameEdgeCases(t *testing.T) {
	tests := []struct {
		name    string
		sName   string
		wantErr bool
	}{
		{"normal", "web-app", false},
		{"with dots", "my.stack", false},
		{"with underscore", "my_stack", false},
		{"with dash", "my-stack", false},
		{"numeric", "12345", false},
		{"empty", "", true},
		{"slash", "web/app", true},
		{"backslash", "web\\app", true},
		{"space", "web app", true},
		{"colon", "web:app", true},
		{"asterisk", "web*app", true},
		{"question mark", "web?app", true},
		{"quotes", "web\"app", true},
		{"angle brackets", "web<app>", true},
		{"pipe", "web|app", true},
		{"very long (512 chars)", strings.Repeat("a", 512), true}, // rejected: max 128 chars
		{"single char", "x", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := stack.NewStack(tt.sName, "/opt/stacks/"+tt.sName, stack.SourceLocal)
			if tt.wantErr && err == nil {
				t.Errorf("expected error for name %q, got nil", tt.sName)
			}
			if !tt.wantErr && err != nil {
				t.Errorf("unexpected error for name %q: %v", tt.sName, err)
			}
		})
	}
}

func TestNewGitStack_BranchEdgeCases(t *testing.T) {
	tests := []struct {
		name    string
		branch  string
		wantErr bool
	}{
		{"main", "main", false},
		{"feature branch", "feature/new-thing", false},
		{"with dots", "release-1.2.3", false},
		{"empty", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			git := &stack.GitSource{
				RepoURL:     "https://github.com/user/repo.git",
				Branch:      tt.branch,
				ComposePath: "compose.yaml",
				AuthMethod:  stack.GitAuthNone,
			}
			_, err := stack.NewGitStack("test", "/opt/stacks/test", git)
			if tt.wantErr && err == nil {
				t.Error("expected error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

func TestStack_StatusTransitions(t *testing.T) {
	s, _ := stack.NewStack("web", "/opt/stacks/web", stack.SourceLocal)

	// Should start as unknown
	if s.Status != stack.StatusUnknown {
		t.Errorf("initial status = %q, want %q", s.Status, stack.StatusUnknown)
	}

	// Test all valid status transitions
	statuses := []stack.Status{
		stack.StatusRunning,
		stack.StatusStopped,
		stack.StatusPartial,
		stack.StatusError,
		stack.StatusSyncing,
		stack.StatusUnknown,
	}
	for _, status := range statuses {
		s.SetStatus(status)
		if s.Status != status {
			t.Errorf("after SetStatus(%q), Status = %q", status, s.Status)
		}
	}
}

func TestSource_Valid(t *testing.T) {
	tests := []struct {
		source stack.Source
		want   bool
	}{
		{stack.SourceLocal, true},
		{stack.SourceGit, true},
		{stack.Source("s3"), false},
		{stack.Source(""), false},
	}
	for _, tt := range tests {
		if got := tt.source.Valid(); got != tt.want {
			t.Errorf("Source(%q).Valid() = %v, want %v", tt.source, got, tt.want)
		}
	}
}

func TestStatus_Valid(t *testing.T) {
	tests := []struct {
		status stack.Status
		want   bool
	}{
		{stack.StatusRunning, true},
		{stack.StatusStopped, true},
		{stack.StatusPartial, true},
		{stack.StatusError, true},
		{stack.StatusSyncing, true},
		{stack.StatusUnknown, true},
		{stack.Status("booting"), false},
		{stack.Status(""), false},
	}
	for _, tt := range tests {
		if got := tt.status.Valid(); got != tt.want {
			t.Errorf("Status(%q).Valid() = %v, want %v", tt.status, got, tt.want)
		}
	}
}
