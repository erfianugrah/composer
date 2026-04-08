package app

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/erfianugrah/composer/internal/domain/auth"
)

var (
	ErrInvalidCredentials = errors.New("invalid email or password")
	ErrBootstrapDone      = errors.New("bootstrap already completed, users exist")
	ErrNotFound           = errors.New("not found")
)

// SessionCache is an optional interface for invalidating cached sessions/keys.
type SessionCache interface {
	DeleteSession(ctx context.Context, sessionID string) error
	DeleteAPIKey(ctx context.Context, hashedKey string) error
}

// AuthService orchestrates authentication operations.
type AuthService struct {
	users    auth.UserRepository
	sessions auth.SessionRepository
	keys     auth.APIKeyRepository
	cache    SessionCache // optional, nil = no cache
}

// NewAuthService creates a new AuthService.
func NewAuthService(
	users auth.UserRepository,
	sessions auth.SessionRepository,
	keys auth.APIKeyRepository,
) *AuthService {
	return &AuthService{
		users:    users,
		sessions: sessions,
		keys:     keys,
	}
}

// SetCache attaches an optional session/key cache for invalidation.
func (s *AuthService) SetCache(c SessionCache) {
	s.cache = c
}

// Bootstrap creates the first admin user. Fails if any users already exist.
// Uses a re-check after creation attempt to handle TOCTOU races:
// two concurrent bootstrap calls both see count==0 but only one insert succeeds
// (the second gets a duplicate email error).
func (s *AuthService) Bootstrap(ctx context.Context, email, password string) (*auth.User, error) {
	count, err := s.users.Count(ctx)
	if err != nil {
		return nil, fmt.Errorf("counting users: %w", err)
	}
	if count > 0 {
		return nil, ErrBootstrapDone
	}

	user, err := auth.NewUser(email, password, auth.RoleAdmin)
	if err != nil {
		return nil, err
	}

	if err := s.users.Create(ctx, user); err != nil {
		// Re-check count: if another bootstrap raced us, users now exist
		if c, _ := s.users.Count(ctx); c > 0 {
			return nil, ErrBootstrapDone
		}
		return nil, fmt.Errorf("creating user: %w", err)
	}

	return user, nil
}

// Login verifies credentials and creates a session.
func (s *AuthService) Login(ctx context.Context, email, password string, ttl time.Duration) (*auth.Session, error) {
	user, err := s.users.GetByEmail(ctx, email)
	if err != nil {
		return nil, fmt.Errorf("looking up user: %w", err)
	}
	if user == nil {
		return nil, ErrInvalidCredentials
	}

	if !user.VerifyPassword(password) {
		return nil, ErrInvalidCredentials
	}

	// Revoke existing sessions (session fixation prevention).
	// Cache invalidation happens via TTL since we don't track session IDs per user in cache.
	if err := s.sessions.DeleteByUserID(ctx, user.ID); err != nil {
		return nil, fmt.Errorf("revoking old sessions: %w", err)
	}

	session, err := auth.NewSession(user.ID, user.Role, ttl)
	if err != nil {
		return nil, fmt.Errorf("creating session: %w", err)
	}

	if err := s.sessions.Create(ctx, session); err != nil {
		return nil, fmt.Errorf("persisting session: %w", err)
	}

	// Update last login
	now := time.Now().UTC()
	user.LastLoginAt = &now
	if err := s.users.Update(ctx, user); err != nil {
		return nil, fmt.Errorf("updating last login: %w", err)
	}

	return session, nil
}

// ValidateSession checks if a session token is valid and not expired.
func (s *AuthService) ValidateSession(ctx context.Context, sessionID string) (*auth.Session, error) {
	session, err := s.sessions.GetByID(ctx, sessionID)
	if err != nil {
		return nil, fmt.Errorf("looking up session: %w", err)
	}
	if session == nil {
		return nil, nil
	}
	if session.IsExpired() {
		_ = s.sessions.DeleteByID(ctx, sessionID)
		return nil, nil
	}
	return session, nil
}

// Logout destroys a session and invalidates the cache.
func (s *AuthService) Logout(ctx context.Context, sessionID string) error {
	if s.cache != nil {
		_ = s.cache.DeleteSession(ctx, sessionID) // best-effort
	}
	return s.sessions.DeleteByID(ctx, sessionID)
}

// ValidateAPIKey checks if a plaintext API key is valid and not expired.
func (s *AuthService) ValidateAPIKey(ctx context.Context, plaintextKey string) (*auth.APIKey, error) {
	hashed := auth.HashAPIKey(plaintextKey)
	key, err := s.keys.GetByHashedKey(ctx, hashed)
	if err != nil {
		return nil, fmt.Errorf("looking up key: %w", err)
	}
	if key == nil {
		return nil, nil
	}
	if key.IsExpired() {
		return nil, nil
	}

	// Update last used (fire and forget)
	_ = s.keys.UpdateLastUsed(ctx, key.ID)

	return key, nil
}

// CreateAPIKey creates a new API key. Returns the key with its plaintext secret.
func (s *AuthService) CreateAPIKey(ctx context.Context, name string, role auth.Role, createdBy string, expiresAt *time.Time) (*auth.APIKeyWithSecret, error) {
	result, err := auth.NewAPIKey(name, role, createdBy, expiresAt)
	if err != nil {
		return nil, err
	}

	if err := s.keys.Create(ctx, &result.APIKey); err != nil {
		return nil, fmt.Errorf("persisting api key: %w", err)
	}

	return result, nil
}

// DeleteAPIKey revokes an API key and invalidates the cache.
func (s *AuthService) DeleteAPIKey(ctx context.Context, id string) error {
	// Note: we don't have the hashed key here, but the cache entry will expire via TTL.
	// For immediate invalidation, the caller should provide the hashed key.
	return s.keys.Delete(ctx, id)
}

// ListAPIKeys returns all API keys (hashed, never plaintext).
func (s *AuthService) ListAPIKeys(ctx context.Context) ([]*auth.APIKey, error) {
	return s.keys.List(ctx)
}

// CleanupExpiredSessions removes all expired sessions. Returns the count deleted.
func (s *AuthService) CleanupExpiredSessions(ctx context.Context) (int, error) {
	return s.sessions.DeleteExpired(ctx)
}
