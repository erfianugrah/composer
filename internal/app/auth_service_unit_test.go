package app

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/erfianugrah/composer/internal/domain/auth"
)

// mockUserRepo is a minimal in-memory UserRepository for unit tests.
type mockUserRepo struct {
	users map[string]*auth.User
}

func newMockUserRepo() *mockUserRepo {
	return &mockUserRepo{users: make(map[string]*auth.User)}
}

func (r *mockUserRepo) Create(_ context.Context, u *auth.User) error {
	r.users[u.ID] = u
	return nil
}
func (r *mockUserRepo) GetByID(_ context.Context, id string) (*auth.User, error) {
	return r.users[id], nil
}
func (r *mockUserRepo) GetByEmail(_ context.Context, email string) (*auth.User, error) {
	for _, u := range r.users {
		if u.Email == email {
			return u, nil
		}
	}
	return nil, nil
}
func (r *mockUserRepo) List(_ context.Context) ([]*auth.User, error) {
	var list []*auth.User
	for _, u := range r.users {
		list = append(list, u)
	}
	return list, nil
}
func (r *mockUserRepo) Update(_ context.Context, u *auth.User) error {
	r.users[u.ID] = u
	return nil
}
func (r *mockUserRepo) Delete(_ context.Context, id string) error {
	delete(r.users, id)
	return nil
}
func (r *mockUserRepo) Count(_ context.Context) (int, error) {
	return len(r.users), nil
}

// mockSessionRepo is a minimal in-memory SessionRepository.
type mockSessionRepo struct {
	sessions map[string]*auth.Session
}

func newMockSessionRepo() *mockSessionRepo {
	return &mockSessionRepo{sessions: make(map[string]*auth.Session)}
}

func (r *mockSessionRepo) Create(_ context.Context, s *auth.Session) error {
	r.sessions[s.ID] = s
	return nil
}
func (r *mockSessionRepo) GetByID(_ context.Context, id string) (*auth.Session, error) {
	return r.sessions[id], nil
}
func (r *mockSessionRepo) DeleteByID(_ context.Context, id string) error {
	delete(r.sessions, id)
	return nil
}
func (r *mockSessionRepo) DeleteByUserID(_ context.Context, userID string) error {
	for id, s := range r.sessions {
		if s.UserID == userID {
			delete(r.sessions, id)
		}
	}
	return nil
}
func (r *mockSessionRepo) DeleteExpired(_ context.Context) (int, error) {
	return 0, nil
}

// mockKeyRepo is a minimal in-memory APIKeyRepository.
type mockKeyRepo struct{}

func (r *mockKeyRepo) Create(_ context.Context, _ *auth.APIKey) error            { return nil }
func (r *mockKeyRepo) GetByID(_ context.Context, _ string) (*auth.APIKey, error) { return nil, nil }
func (r *mockKeyRepo) GetByHashedKey(_ context.Context, _ string) (*auth.APIKey, error) {
	return nil, nil
}
func (r *mockKeyRepo) List(_ context.Context) ([]*auth.APIKey, error)   { return nil, nil }
func (r *mockKeyRepo) Delete(_ context.Context, _ string) error         { return nil }
func (r *mockKeyRepo) UpdateLastUsed(_ context.Context, _ string) error { return nil }

// mockTxRunner tracks whether RunInTx was called.
type mockTxRunner struct {
	called bool
}

func (m *mockTxRunner) RunInTx(_ context.Context, fn func(ctx context.Context) error) error {
	m.called = true
	return fn(context.Background())
}

func TestAuthService_Login_UsesTxRunner(t *testing.T) {
	users := newMockUserRepo()
	sessions := newMockSessionRepo()
	svc := NewAuthService(users, sessions, &mockKeyRepo{})
	txRunner := &mockTxRunner{}
	svc.SetTxRunner(txRunner)

	// Bootstrap a user
	_, err := svc.Bootstrap(context.Background(), "test@example.com", "strongpassword1")
	require.NoError(t, err)

	// Login should use the tx runner
	session, err := svc.Login(context.Background(), "test@example.com", "strongpassword1", time.Hour)
	require.NoError(t, err)
	assert.NotNil(t, session)
	assert.True(t, txRunner.called, "Login should use TxRunner when set")
}

func TestAuthService_Login_WorksWithoutTxRunner(t *testing.T) {
	users := newMockUserRepo()
	sessions := newMockSessionRepo()
	svc := NewAuthService(users, sessions, &mockKeyRepo{})
	// No SetTxRunner call — tx is nil

	_, err := svc.Bootstrap(context.Background(), "test@example.com", "strongpassword1")
	require.NoError(t, err)

	session, err := svc.Login(context.Background(), "test@example.com", "strongpassword1", time.Hour)
	require.NoError(t, err)
	assert.NotNil(t, session)
}

func TestAuthService_Login_InvalidCredentials(t *testing.T) {
	users := newMockUserRepo()
	sessions := newMockSessionRepo()
	svc := NewAuthService(users, sessions, &mockKeyRepo{})

	_, err := svc.Bootstrap(context.Background(), "test@example.com", "strongpassword1")
	require.NoError(t, err)

	_, err = svc.Login(context.Background(), "test@example.com", "wrongpassword", time.Hour)
	assert.ErrorIs(t, err, ErrInvalidCredentials)
}

func TestAuthService_Login_NonexistentUser(t *testing.T) {
	users := newMockUserRepo()
	sessions := newMockSessionRepo()
	svc := NewAuthService(users, sessions, &mockKeyRepo{})

	_, err := svc.Login(context.Background(), "nobody@example.com", "password123", time.Hour)
	assert.ErrorIs(t, err, ErrInvalidCredentials)
}

func TestAuthService_Login_RevokesOldSessions(t *testing.T) {
	users := newMockUserRepo()
	sessions := newMockSessionRepo()
	svc := NewAuthService(users, sessions, &mockKeyRepo{})

	_, err := svc.Bootstrap(context.Background(), "test@example.com", "strongpassword1")
	require.NoError(t, err)

	// Login twice — first session should be revoked
	s1, _ := svc.Login(context.Background(), "test@example.com", "strongpassword1", time.Hour)
	s2, _ := svc.Login(context.Background(), "test@example.com", "strongpassword1", time.Hour)

	// s1 should be gone from the session repo
	got1, _ := svc.ValidateSession(context.Background(), s1.ID)
	assert.Nil(t, got1, "first session should be revoked after second login")

	// s2 should still be valid
	got2, _ := svc.ValidateSession(context.Background(), s2.ID)
	assert.NotNil(t, got2, "second session should be valid")
}
