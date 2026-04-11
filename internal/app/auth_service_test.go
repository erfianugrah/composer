//go:build integration

package app_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"

	"github.com/erfianugrah/composer/internal/app"
	"github.com/erfianugrah/composer/internal/domain/auth"
	"github.com/erfianugrah/composer/internal/infra/store"
)

func setupAuthService(t *testing.T) *app.AuthService {
	t.Helper()
	ctx := context.Background()

	pgCtr, err := tcpostgres.Run(ctx,
		"postgres:17-alpine",
		tcpostgres.WithDatabase("composer_test"),
		tcpostgres.WithUsername("test"),
		tcpostgres.WithPassword("test"),
		tcpostgres.BasicWaitStrategies(),
		tcpostgres.WithSQLDriver("pgx"),
	)
	require.NoError(t, err)
	t.Cleanup(func() { pgCtr.Terminate(context.Background()) })

	connStr, err := pgCtr.ConnectionString(ctx, "sslmode=disable")
	require.NoError(t, err)

	db, err := store.New(ctx, connStr, "")
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })

	users := store.NewUserRepo(db.SQL)
	sessions := store.NewSessionRepo(db.SQL)
	keys := store.NewAPIKeyRepo(db.SQL)

	svc := app.NewAuthService(users, sessions, keys)
	svc.SetTxRunner(store.NewDBTxRunner(db))
	return svc
}

func TestAuthService_Bootstrap(t *testing.T) {
	svc := setupAuthService(t)
	ctx := context.Background()

	// First bootstrap succeeds
	user, err := svc.Bootstrap(ctx, "admin@example.com", "strongpassword1")
	require.NoError(t, err)
	assert.Equal(t, "admin@example.com", user.Email)
	assert.Equal(t, auth.RoleAdmin, user.Role)

	// Second bootstrap fails
	_, err = svc.Bootstrap(ctx, "other@example.com", "strongpassword1")
	assert.ErrorIs(t, err, app.ErrBootstrapDone)
}

func TestAuthService_LoginLogout(t *testing.T) {
	svc := setupAuthService(t)
	ctx := context.Background()

	// Bootstrap user
	_, err := svc.Bootstrap(ctx, "user@example.com", "mypassword123")
	require.NoError(t, err)

	// Login with correct password
	session, err := svc.Login(ctx, "user@example.com", "mypassword123", 24*time.Hour)
	require.NoError(t, err)
	assert.NotEmpty(t, session.ID)
	assert.Equal(t, auth.RoleAdmin, session.Role)

	// Validate session
	valid, err := svc.ValidateSession(ctx, session.ID)
	require.NoError(t, err)
	assert.NotNil(t, valid)

	// Logout
	err = svc.Logout(ctx, session.ID)
	require.NoError(t, err)

	// Session should now be invalid
	gone, err := svc.ValidateSession(ctx, session.ID)
	require.NoError(t, err)
	assert.Nil(t, gone)
}

func TestAuthService_Login_InvalidCredentials(t *testing.T) {
	svc := setupAuthService(t)
	ctx := context.Background()

	svc.Bootstrap(ctx, "user@example.com", "mypassword123")

	// Wrong password
	_, err := svc.Login(ctx, "user@example.com", "wrongpassword", 24*time.Hour)
	assert.ErrorIs(t, err, app.ErrInvalidCredentials)

	// Wrong email
	_, err = svc.Login(ctx, "nobody@example.com", "mypassword123", 24*time.Hour)
	assert.ErrorIs(t, err, app.ErrInvalidCredentials)
}

func TestAuthService_SessionFixation(t *testing.T) {
	svc := setupAuthService(t)
	ctx := context.Background()

	svc.Bootstrap(ctx, "user@example.com", "mypassword123")

	// First login
	session1, _ := svc.Login(ctx, "user@example.com", "mypassword123", 24*time.Hour)

	// Second login should revoke the first session
	session2, _ := svc.Login(ctx, "user@example.com", "mypassword123", 24*time.Hour)

	assert.NotEqual(t, session1.ID, session2.ID)

	// First session should be invalid
	old, _ := svc.ValidateSession(ctx, session1.ID)
	assert.Nil(t, old)

	// Second session should be valid
	current, _ := svc.ValidateSession(ctx, session2.ID)
	assert.NotNil(t, current)
}

func TestAuthService_APIKey(t *testing.T) {
	svc := setupAuthService(t)
	ctx := context.Background()

	user, _ := svc.Bootstrap(ctx, "admin@example.com", "strongpassword1")

	// Create key
	result, err := svc.CreateAPIKey(ctx, "deploy-key", auth.RoleOperator, user.ID, nil)
	require.NoError(t, err)
	assert.NotEmpty(t, result.PlaintextKey)
	assert.Equal(t, "deploy-key", result.Name)
	assert.Equal(t, auth.RoleOperator, result.Role)

	// Validate with plaintext key
	valid, err := svc.ValidateAPIKey(ctx, result.PlaintextKey)
	require.NoError(t, err)
	require.NotNil(t, valid)
	assert.Equal(t, auth.RoleOperator, valid.Role)

	// Invalid key
	invalid, err := svc.ValidateAPIKey(ctx, "ck_invalid_key")
	require.NoError(t, err)
	assert.Nil(t, invalid)

	// List keys
	keys, err := svc.ListAPIKeys(ctx)
	require.NoError(t, err)
	assert.Len(t, keys, 1)

	// Delete key
	err = svc.DeleteAPIKey(ctx, result.ID)
	require.NoError(t, err)

	// Should no longer validate
	deleted, _ := svc.ValidateAPIKey(ctx, result.PlaintextKey)
	assert.Nil(t, deleted)
}
