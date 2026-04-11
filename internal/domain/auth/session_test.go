package auth_test

import (
	"testing"
	"time"

	"github.com/erfianugrah/composer/internal/domain/auth"
)

func TestNewSession(t *testing.T) {
	s, err := auth.NewSession("user123", auth.RoleAdmin, auth.DefaultSessionTTL)
	if err != nil {
		t.Fatalf("NewSession() error: %v", err)
	}

	if s.ID == "" {
		t.Error("expected non-empty session ID")
	}
	if len(s.ID) < 40 { // 32 bytes base64url = 43 chars
		t.Errorf("session ID too short: %d chars", len(s.ID))
	}
	if s.UserID != "user123" {
		t.Errorf("UserID = %q, want %q", s.UserID, "user123")
	}
	if s.Role != auth.RoleAdmin {
		t.Errorf("Role = %q, want %q", s.Role, auth.RoleAdmin)
	}
	if s.CreatedAt.IsZero() {
		t.Error("expected non-zero CreatedAt")
	}
	if !s.ExpiresAt.After(s.CreatedAt) {
		t.Error("ExpiresAt should be after CreatedAt")
	}
}

func TestSession_UniqueTokens(t *testing.T) {
	s1, _ := auth.NewSession("user1", auth.RoleViewer, time.Hour)
	s2, _ := auth.NewSession("user1", auth.RoleViewer, time.Hour)
	if s1.ID == s2.ID {
		t.Error("two sessions should have different tokens")
	}
}

func TestSession_IsExpired(t *testing.T) {
	// Not expired
	s, _ := auth.NewSession("user1", auth.RoleViewer, time.Hour)
	if s.IsExpired() {
		t.Error("fresh session should not be expired")
	}

	// Force expired by setting ExpiresAt in the past
	s.ExpiresAt = time.Now().UTC().Add(-time.Minute)
	if !s.IsExpired() {
		t.Error("session with past ExpiresAt should be expired")
	}
}

func TestNewSession_InvalidRole(t *testing.T) {
	// Invalid role should be rejected (B1 fix)
	_, err := auth.NewSession("user1", auth.Role("superadmin"), time.Hour)
	if err == nil {
		t.Error("expected error for invalid role, got nil")
	}
}

func TestNewSession_EmptyRole(t *testing.T) {
	_, err := auth.NewSession("user1", auth.Role(""), time.Hour)
	if err == nil {
		t.Error("expected error for empty role, got nil")
	}
}
