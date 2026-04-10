package auth

import (
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
)

const (
	bcryptCost     = 12
	minPasswordLen = 8
)

// User is the aggregate root for identity & access.
type User struct {
	ID           string
	Email        string
	PasswordHash string
	Role         Role
	AuthProvider string // "local", "github", "google"
	CreatedAt    time.Time
	UpdatedAt    time.Time
	LastLoginAt  *time.Time
}

// NewUser creates a new User with a hashed password.
// Email is lowercased. Validates all inputs.
func NewUser(email, password string, role Role) (*User, error) {
	email = strings.ToLower(strings.TrimSpace(email))

	if email == "" || !isValidEmail(email) {
		return nil, errors.New("invalid email address")
	}
	if len(password) < minPasswordLen {
		return nil, fmt.Errorf("password must be at least %d characters", minPasswordLen)
	}
	if len(password) > 72 {
		return nil, errors.New("password must not exceed 72 bytes (bcrypt limit)")
	}
	if !role.Valid() {
		return nil, fmt.Errorf("invalid role %q", role)
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcryptCost)
	if err != nil {
		return nil, fmt.Errorf("hashing password: %w", err)
	}

	now := time.Now().UTC()
	return &User{
		ID:           generateID(),
		Email:        email,
		PasswordHash: string(hash),
		Role:         role,
		AuthProvider: "local",
		CreatedAt:    now,
		UpdatedAt:    now,
	}, nil
}

// VerifyPassword checks if the plaintext password matches the stored hash.
// Uses bcrypt's constant-time comparison.
func (u *User) VerifyPassword(plaintext string) bool {
	if plaintext == "" {
		return false
	}
	return bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(plaintext)) == nil
}

// ChangePassword verifies the old password, then sets a new one.
func (u *User) ChangePassword(oldPassword, newPassword string) error {
	if !u.VerifyPassword(oldPassword) {
		return errors.New("incorrect current password")
	}
	if len(newPassword) < minPasswordLen {
		return fmt.Errorf("new password must be at least %d characters", minPasswordLen)
	}
	if len(newPassword) > 72 {
		return errors.New("new password must not exceed 72 bytes (bcrypt limit)")
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcryptCost)
	if err != nil {
		return fmt.Errorf("hashing password: %w", err)
	}

	u.PasswordHash = string(hash)
	u.UpdatedAt = time.Now().UTC()
	return nil
}

// UpdateRole changes the user's role.
func (u *User) UpdateRole(newRole Role) error {
	if !newRole.Valid() {
		return fmt.Errorf("invalid role %q", newRole)
	}
	u.Role = newRole
	u.UpdatedAt = time.Now().UTC()
	return nil
}

// isValidEmail performs a basic email validation.
// Requires at least one character before @, at least one character after @,
// and at least one dot after @.
func isValidEmail(email string) bool {
	at := strings.IndexByte(email, '@')
	if at < 1 {
		return false // no @ or nothing before @
	}
	domain := email[at+1:]
	if len(domain) < 3 || !strings.Contains(domain, ".") {
		return false // domain too short or no dot
	}
	return true
}

// generateID creates a simple unique ID using timestamp + random bytes.
// In production we use ULID, but this keeps the domain layer dependency-free.
// The actual ULID generation lives in the infrastructure layer and is injected
// via the repository when persisting.
func generateID() string {
	var buf [16]byte
	binary.BigEndian.PutUint64(buf[:8], uint64(time.Now().UnixNano()))
	if _, err := rand.Read(buf[8:]); err != nil {
		panic("crypto/rand.Read failed: " + err.Error())
	}
	return fmt.Sprintf("%x", buf)
}
