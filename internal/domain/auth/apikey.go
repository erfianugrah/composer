package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"time"
)

const (
	apiKeyPrefix    = "ck_"
	apiKeyRandBytes = 32
)

// APIKey represents a long-lived API credential for automation.
type APIKey struct {
	ID         string // "ck_"-prefixed identifier
	Name       string
	HashedKey  string // SHA-256 hex of the full key (stored in DB)
	Role       Role
	CreatedBy  string
	LastUsedAt *time.Time
	ExpiresAt  *time.Time // nil = never expires
	CreatedAt  time.Time
}

// APIKeyWithSecret is returned only on creation -- contains the plaintext key
// that is shown to the user exactly once.
type APIKeyWithSecret struct {
	APIKey
	PlaintextKey string
}

// NewAPIKey creates a new API key. Returns the key with its plaintext secret.
// The plaintext key must be shown to the user once and never stored.
func NewAPIKey(name string, role Role, createdBy string, expiresAt *time.Time) (*APIKeyWithSecret, error) {
	if name == "" {
		return nil, fmt.Errorf("API key name is required")
	}
	if !role.Valid() {
		return nil, fmt.Errorf("invalid role %q", role)
	}
	if createdBy == "" {
		return nil, fmt.Errorf("createdBy is required")
	}

	// Generate random key bytes
	buf := make([]byte, apiKeyRandBytes)
	if _, err := rand.Read(buf); err != nil {
		return nil, fmt.Errorf("generating key: %w", err)
	}

	plaintext := apiKeyPrefix + hex.EncodeToString(buf)
	hashed := HashAPIKey(plaintext)

	now := time.Now().UTC()
	return &APIKeyWithSecret{
		APIKey: APIKey{
			ID:        plaintext[:len(apiKeyPrefix)+16], // first 8 random bytes as ID
			Name:      name,
			HashedKey: hashed,
			Role:      role,
			CreatedBy: createdBy,
			ExpiresAt: expiresAt,
			CreatedAt: now,
		},
		PlaintextKey: plaintext,
	}, nil
}

// HashAPIKey computes the SHA-256 hex digest of a plaintext API key.
func HashAPIKey(plaintext string) string {
	h := sha256.Sum256([]byte(plaintext))
	return hex.EncodeToString(h[:])
}

// IsExpired returns true if the key has a set expiry and it has passed.
func (k *APIKey) IsExpired() bool {
	if k.ExpiresAt == nil {
		return false
	}
	return time.Now().UTC().After(*k.ExpiresAt)
}
