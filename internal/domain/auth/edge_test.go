package auth_test

import (
	"testing"
	"time"

	"github.com/erfianugrah/composer/internal/domain/auth"
)

// --- User edge cases ---

func TestNewUser_EmailEdgeCases(t *testing.T) {
	tests := []struct {
		name    string
		email   string
		wantErr bool
	}{
		{"valid basic", "user@example.com", false},
		{"valid with subdomain", "user@mail.example.com", false},
		{"valid with plus", "user+tag@example.com", false},
		{"valid with dots", "first.last@example.com", false},
		{"empty", "", true},
		{"no at sign", "userexample.com", true},
		{"at only", "@", true},
		{"at with no domain", "user@", true},
		{"at with short domain", "user@x", true},
		{"at with no dot in domain", "user@example", true},
		{"spaces", "user @example.com", false}, // trimmed, then has @
		{"leading whitespace", "  user@example.com  ", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := auth.NewUser(tt.email, "strongpassword1", auth.RoleViewer)
			if tt.wantErr && err == nil {
				t.Error("expected error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

func TestNewUser_PasswordBoundaries(t *testing.T) {
	tests := []struct {
		name     string
		password string
		wantErr  bool
	}{
		{"exactly 8 chars", "12345678", false},
		{"7 chars", "1234567", true},
		{"empty", "", true},
		{"72 bytes (max)", string(make([]byte, 72)), false},
		{"73 bytes (over bcrypt limit)", string(make([]byte, 73)), true},
		{"256 bytes (way over)", string(make([]byte, 256)), true},
		{"unicode password", "p\u00e4ssw\u00f6rd!", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := auth.NewUser("test@example.com", tt.password, auth.RoleViewer)
			if tt.wantErr && err == nil {
				t.Error("expected error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

func TestUser_VerifyPassword_EdgeCases(t *testing.T) {
	u, _ := auth.NewUser("test@example.com", "correcthorse", auth.RoleAdmin)

	tests := []struct {
		name string
		pass string
		want bool
	}{
		{"correct", "correcthorse", true},
		{"wrong", "wronghorse", false},
		{"empty", "", false},
		{"case sensitive", "Correcthorse", false},
		{"trailing space", "correcthorse ", false},
		{"leading space", " correcthorse", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := u.VerifyPassword(tt.pass); got != tt.want {
				t.Errorf("VerifyPassword(%q) = %v, want %v", tt.pass, got, tt.want)
			}
		})
	}
}

// --- Session edge cases ---

func TestNewSession_TokenUniqueness(t *testing.T) {
	seen := make(map[string]bool)
	for i := 0; i < 100; i++ {
		s, err := auth.NewSession("user1", auth.RoleViewer, time.Hour)
		if err != nil {
			t.Fatalf("NewSession() error on iteration %d: %v", i, err)
		}
		if seen[s.ID] {
			t.Fatalf("duplicate session token on iteration %d", i)
		}
		seen[s.ID] = true
	}
}

func TestSession_ExpiryBoundary(t *testing.T) {
	// Session with zero TTL should be immediately expired
	s, _ := auth.NewSession("user1", auth.RoleViewer, 0)
	// ExpiresAt == CreatedAt, so IsExpired() depends on time.Now() > ExpiresAt
	// With 0 TTL, it should be expired almost immediately
	time.Sleep(time.Millisecond)
	if !s.IsExpired() {
		t.Error("session with 0 TTL should be expired after 1ms")
	}
}

// --- APIKey edge cases ---

func TestNewAPIKey_NameEdgeCases(t *testing.T) {
	tests := []struct {
		name    string
		keyName string
		wantErr bool
	}{
		{"normal name", "deploy-key", false},
		{"spaces", "my deploy key", false},
		{"unicode", "cl\u00e9-d\u00e9ploiement", false},
		{"very long", string(make([]byte, 500)), false},
		{"empty", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := auth.NewAPIKey(tt.keyName, auth.RoleViewer, "user1", nil)
			if tt.wantErr && err == nil {
				t.Error("expected error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

func TestAPIKey_ExpiryEdgeCases(t *testing.T) {
	// Expires exactly now -- should be expired after any delay
	now := time.Now().UTC()
	k, _ := auth.NewAPIKey("key", auth.RoleViewer, "user1", &now)
	time.Sleep(time.Millisecond)
	if !k.IsExpired() {
		t.Error("key expiring at creation time should be expired after 1ms")
	}

	// Expires far in the future
	future := time.Now().UTC().Add(100 * 365 * 24 * time.Hour)
	k2, _ := auth.NewAPIKey("key2", auth.RoleViewer, "user1", &future)
	if k2.IsExpired() {
		t.Error("key expiring in 100 years should not be expired")
	}
}

// --- Role edge cases ---

func TestRole_AtLeast_InvalidRole(t *testing.T) {
	invalid := auth.Role("superadmin")
	// Invalid role should have level 0, so AtLeast anything returns false
	if invalid.AtLeast(auth.RoleViewer) {
		t.Error("invalid role should not have sufficient access")
	}
}
