//go:build integration

package postgres_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/erfianugrah/composer/internal/domain/auth"
	"github.com/erfianugrah/composer/internal/domain/stack"
	"github.com/erfianugrah/composer/internal/infra/store/postgres"
)

// --- Session Repo Tests ---

func TestSessionRepo_CreateAndGet(t *testing.T) {
	db := setupTestDB(t)
	userRepo := postgres.NewUserRepo(db.Pool)
	sessionRepo := postgres.NewSessionRepo(db.Pool)
	ctx := context.Background()

	// Create a user first (FK constraint)
	user, _ := auth.NewUser("sess@example.com", "strongpassword1", auth.RoleAdmin)
	require.NoError(t, userRepo.Create(ctx, user))

	session, _ := auth.NewSession(user.ID, user.Role, 24*time.Hour)
	require.NoError(t, sessionRepo.Create(ctx, session))

	got, err := sessionRepo.GetByID(ctx, session.ID)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, session.ID, got.ID)
	assert.Equal(t, user.ID, got.UserID)
	assert.Equal(t, auth.RoleAdmin, got.Role)
}

func TestSessionRepo_DeleteByUserID(t *testing.T) {
	db := setupTestDB(t)
	userRepo := postgres.NewUserRepo(db.Pool)
	sessionRepo := postgres.NewSessionRepo(db.Pool)
	ctx := context.Background()

	user, _ := auth.NewUser("del@example.com", "strongpassword1", auth.RoleAdmin)
	require.NoError(t, userRepo.Create(ctx, user))

	s1, _ := auth.NewSession(user.ID, user.Role, time.Hour)
	s2, _ := auth.NewSession(user.ID, user.Role, time.Hour)
	require.NoError(t, sessionRepo.Create(ctx, s1))
	require.NoError(t, sessionRepo.Create(ctx, s2))

	require.NoError(t, sessionRepo.DeleteByUserID(ctx, user.ID))

	got1, _ := sessionRepo.GetByID(ctx, s1.ID)
	got2, _ := sessionRepo.GetByID(ctx, s2.ID)
	assert.Nil(t, got1)
	assert.Nil(t, got2)
}

func TestSessionRepo_DeleteExpired(t *testing.T) {
	db := setupTestDB(t)
	userRepo := postgres.NewUserRepo(db.Pool)
	sessionRepo := postgres.NewSessionRepo(db.Pool)
	ctx := context.Background()

	user, _ := auth.NewUser("exp@example.com", "strongpassword1", auth.RoleAdmin)
	require.NoError(t, userRepo.Create(ctx, user))

	// Create an already-expired session
	expired, _ := auth.NewSession(user.ID, user.Role, time.Hour)
	expired.ExpiresAt = time.Now().UTC().Add(-time.Minute) // in the past
	require.NoError(t, sessionRepo.Create(ctx, expired))

	// Create a valid session
	valid, _ := auth.NewSession(user.ID, user.Role, time.Hour)
	require.NoError(t, sessionRepo.Create(ctx, valid))

	count, err := sessionRepo.DeleteExpired(ctx)
	require.NoError(t, err)
	assert.Equal(t, 1, count)

	// Valid session should still exist
	got, _ := sessionRepo.GetByID(ctx, valid.ID)
	assert.NotNil(t, got)

	// Expired session should be gone
	gone, _ := sessionRepo.GetByID(ctx, expired.ID)
	assert.Nil(t, gone)
}

// --- API Key Repo Tests ---

func TestAPIKeyRepo_CreateAndGetByHash(t *testing.T) {
	db := setupTestDB(t)
	userRepo := postgres.NewUserRepo(db.Pool)
	keyRepo := postgres.NewAPIKeyRepo(db.Pool)
	ctx := context.Background()

	user, _ := auth.NewUser("key@example.com", "strongpassword1", auth.RoleAdmin)
	require.NoError(t, userRepo.Create(ctx, user))

	result, _ := auth.NewAPIKey("deploy", auth.RoleOperator, user.ID, nil)
	require.NoError(t, keyRepo.Create(ctx, &result.APIKey))

	got, err := keyRepo.GetByHashedKey(ctx, result.HashedKey)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "deploy", got.Name)
	assert.Equal(t, auth.RoleOperator, got.Role)
}

func TestAPIKeyRepo_ListAndDelete(t *testing.T) {
	db := setupTestDB(t)
	userRepo := postgres.NewUserRepo(db.Pool)
	keyRepo := postgres.NewAPIKeyRepo(db.Pool)
	ctx := context.Background()

	user, _ := auth.NewUser("keys@example.com", "strongpassword1", auth.RoleAdmin)
	require.NoError(t, userRepo.Create(ctx, user))

	k1, _ := auth.NewAPIKey("key1", auth.RoleViewer, user.ID, nil)
	k2, _ := auth.NewAPIKey("key2", auth.RoleOperator, user.ID, nil)
	require.NoError(t, keyRepo.Create(ctx, &k1.APIKey))
	require.NoError(t, keyRepo.Create(ctx, &k2.APIKey))

	keys, err := keyRepo.List(ctx)
	require.NoError(t, err)
	assert.Len(t, keys, 2)

	require.NoError(t, keyRepo.Delete(ctx, k1.ID))
	keys, _ = keyRepo.List(ctx)
	assert.Len(t, keys, 1)
}

// --- Stack Repo Tests ---

func TestStackRepo_CRUD(t *testing.T) {
	db := setupTestDB(t)
	stackRepo := postgres.NewStackRepo(db.Pool)
	ctx := context.Background()

	st, _ := stack.NewStack("web-app", "/opt/stacks/web-app", stack.SourceLocal)
	require.NoError(t, stackRepo.Create(ctx, st))

	got, err := stackRepo.GetByName(ctx, "web-app")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "web-app", got.Name)
	assert.Equal(t, stack.SourceLocal, got.Source)

	// List
	stacks, err := stackRepo.List(ctx)
	require.NoError(t, err)
	assert.Len(t, stacks, 1)

	// Update
	got.Path = "/opt/stacks/web-app-v2"
	got.UpdatedAt = time.Now().UTC()
	require.NoError(t, stackRepo.Update(ctx, got))

	updated, _ := stackRepo.GetByName(ctx, "web-app")
	assert.Equal(t, "/opt/stacks/web-app-v2", updated.Path)

	// Delete
	require.NoError(t, stackRepo.Delete(ctx, "web-app"))
	gone, _ := stackRepo.GetByName(ctx, "web-app")
	assert.Nil(t, gone)
}

func TestGitConfigRepo_UpsertAndGet(t *testing.T) {
	db := setupTestDB(t)
	stackRepo := postgres.NewStackRepo(db.Pool)
	gitRepo := postgres.NewGitConfigRepo(db.Pool)
	ctx := context.Background()

	// Create parent stack first
	st, _ := stack.NewStack("infra", "/opt/stacks/infra", stack.SourceGit)
	require.NoError(t, stackRepo.Create(ctx, st))

	cfg := &stack.GitSource{
		RepoURL:     "https://github.com/user/infra.git",
		Branch:      "main",
		ComposePath: "compose.yaml",
		AutoSync:    true,
		AuthMethod:  stack.GitAuthNone,
		SyncStatus:  stack.GitSynced,
	}
	require.NoError(t, gitRepo.Upsert(ctx, "infra", cfg))

	got, err := gitRepo.GetByStackName(ctx, "infra")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "https://github.com/user/infra.git", got.RepoURL)
	assert.Equal(t, "main", got.Branch)
	assert.True(t, got.AutoSync)

	// Update sync status
	require.NoError(t, gitRepo.UpdateSyncStatus(ctx, "infra", stack.GitBehind, "abc123"))
	updated, _ := gitRepo.GetByStackName(ctx, "infra")
	assert.Equal(t, stack.GitBehind, updated.SyncStatus)
	assert.Equal(t, "abc123", updated.LastCommitSHA)

	// Delete
	require.NoError(t, gitRepo.Delete(ctx, "infra"))
	gone, _ := gitRepo.GetByStackName(ctx, "infra")
	assert.Nil(t, gone)
}
