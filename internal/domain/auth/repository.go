package auth

import "context"

// UserRepository persists and retrieves users.
type UserRepository interface {
	Create(ctx context.Context, user *User) error
	GetByID(ctx context.Context, id string) (*User, error)
	GetByEmail(ctx context.Context, email string) (*User, error)
	List(ctx context.Context) ([]*User, error)
	Update(ctx context.Context, user *User) error
	Delete(ctx context.Context, id string) error
	Count(ctx context.Context) (int, error)
}

// SessionRepository persists and retrieves sessions.
type SessionRepository interface {
	Create(ctx context.Context, session *Session) error
	GetByID(ctx context.Context, id string) (*Session, error)
	DeleteByID(ctx context.Context, id string) error
	DeleteByUserID(ctx context.Context, userID string) error
	DeleteExpired(ctx context.Context) (int, error)
}

// APIKeyRepository persists and retrieves API keys.
type APIKeyRepository interface {
	Create(ctx context.Context, key *APIKey) error
	GetByHashedKey(ctx context.Context, hashedKey string) (*APIKey, error)
	List(ctx context.Context) ([]*APIKey, error)
	Delete(ctx context.Context, id string) error
	UpdateLastUsed(ctx context.Context, id string) error
}
