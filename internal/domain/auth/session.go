package auth

import (
	"crypto/rand"
	"encoding/base64"
	"time"
)

const (
	sessionTokenBytes = 32
	DefaultSessionTTL = 24 * time.Hour
)

// Session represents an authenticated browser session.
type Session struct {
	ID        string // base64url-encoded random token
	UserID    string
	Role      Role // denormalized for fast middleware checks
	CreatedAt time.Time
	ExpiresAt time.Time
}

// NewSession creates a new session with a cryptographically random token.
func NewSession(userID string, role Role, ttl time.Duration) (*Session, error) {
	token, err := generateSessionToken()
	if err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	return &Session{
		ID:        token,
		UserID:    userID,
		Role:      role,
		CreatedAt: now,
		ExpiresAt: now.Add(ttl),
	}, nil
}

// IsExpired returns true if the session has passed its expiry time.
func (s *Session) IsExpired() bool {
	return time.Now().UTC().After(s.ExpiresAt)
}

// generateSessionToken creates a 32-byte random token, base64url-encoded.
func generateSessionToken() (string, error) {
	buf := make([]byte, sessionTokenBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}
