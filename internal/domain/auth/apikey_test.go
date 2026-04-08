package auth_test

import (
	"strings"
	"testing"
	"time"

	"github.com/erfianugrah/composer/internal/domain/auth"
)

func TestNewAPIKey(t *testing.T) {
	result, err := auth.NewAPIKey("deploy-key", auth.RoleOperator, "user123", nil)
	if err != nil {
		t.Fatalf("NewAPIKey() error: %v", err)
	}

	if !strings.HasPrefix(result.PlaintextKey, "ck_") {
		t.Errorf("plaintext key should start with 'ck_', got %q", result.PlaintextKey[:10])
	}
	// ck_ + 64 hex chars (32 bytes)
	if len(result.PlaintextKey) != 3+64 {
		t.Errorf("plaintext key length = %d, want %d", len(result.PlaintextKey), 3+64)
	}
	if result.HashedKey == "" {
		t.Error("expected non-empty hashed key")
	}
	if result.HashedKey == result.PlaintextKey {
		t.Error("hashed key should differ from plaintext")
	}
	if result.Name != "deploy-key" {
		t.Errorf("Name = %q, want %q", result.Name, "deploy-key")
	}
	if result.Role != auth.RoleOperator {
		t.Errorf("Role = %q, want %q", result.Role, auth.RoleOperator)
	}
	if result.ExpiresAt != nil {
		t.Error("ExpiresAt should be nil for never-expiring key")
	}
}

func TestNewAPIKey_Validation(t *testing.T) {
	tests := []struct {
		name      string
		keyName   string
		role      auth.Role
		createdBy string
	}{
		{"empty name", "", auth.RoleAdmin, "user1"},
		{"invalid role", "key1", auth.Role("root"), "user1"},
		{"empty createdBy", "key1", auth.RoleAdmin, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := auth.NewAPIKey(tt.keyName, tt.role, tt.createdBy, nil)
			if err == nil {
				t.Error("expected error, got nil")
			}
		})
	}
}

func TestAPIKey_HashVerification(t *testing.T) {
	result, _ := auth.NewAPIKey("test", auth.RoleViewer, "user1", nil)

	// Hashing the same plaintext should produce the same hash
	rehash := auth.HashAPIKey(result.PlaintextKey)
	if rehash != result.HashedKey {
		t.Error("re-hashing plaintext key should produce same hash")
	}

	// Different key should produce different hash
	differentHash := auth.HashAPIKey("ck_different")
	if differentHash == result.HashedKey {
		t.Error("different key should produce different hash")
	}
}

func TestAPIKey_UniqueKeys(t *testing.T) {
	k1, _ := auth.NewAPIKey("key1", auth.RoleViewer, "user1", nil)
	k2, _ := auth.NewAPIKey("key2", auth.RoleViewer, "user1", nil)
	if k1.PlaintextKey == k2.PlaintextKey {
		t.Error("two keys should have different plaintext values")
	}
	if k1.HashedKey == k2.HashedKey {
		t.Error("two keys should have different hashes")
	}
}

func TestAPIKey_IsExpired(t *testing.T) {
	// Never expires
	k1, _ := auth.NewAPIKey("key1", auth.RoleViewer, "user1", nil)
	if k1.IsExpired() {
		t.Error("nil ExpiresAt should not be expired")
	}

	// Expires in the future
	future := time.Now().UTC().Add(time.Hour)
	k2, _ := auth.NewAPIKey("key2", auth.RoleViewer, "user1", &future)
	if k2.IsExpired() {
		t.Error("future ExpiresAt should not be expired")
	}

	// Expired in the past
	past := time.Now().UTC().Add(-time.Hour)
	k3, _ := auth.NewAPIKey("key3", auth.RoleViewer, "user1", &past)
	if !k3.IsExpired() {
		t.Error("past ExpiresAt should be expired")
	}
}
