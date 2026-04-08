//go:build integration

package postgres_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/erfianugrah/composer/internal/domain/auth"
	"github.com/erfianugrah/composer/internal/infra/store/postgres"
)

// setupTestDB starts a Postgres container and runs migrations.
func setupTestDB(t *testing.T) *postgres.DB {
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

	// Wait for readiness
	err = wait.ForListeningPort("5432/tcp").WaitUntilReady(ctx, pgCtr)
	require.NoError(t, err)

	db, err := postgres.New(ctx, connStr)
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })

	return db
}

func TestUserRepo_CreateAndGetByID(t *testing.T) {
	db := setupTestDB(t)
	repo := postgres.NewUserRepo(db.Pool)
	ctx := context.Background()

	user, err := auth.NewUser("alice@example.com", "strongpassword1", auth.RoleAdmin)
	require.NoError(t, err)

	err = repo.Create(ctx, user)
	require.NoError(t, err)

	got, err := repo.GetByID(ctx, user.ID)
	require.NoError(t, err)
	require.NotNil(t, got)

	assert.Equal(t, user.ID, got.ID)
	assert.Equal(t, "alice@example.com", got.Email)
	assert.Equal(t, auth.RoleAdmin, got.Role)
	assert.NotEmpty(t, got.PasswordHash)
}

func TestUserRepo_GetByEmail(t *testing.T) {
	db := setupTestDB(t)
	repo := postgres.NewUserRepo(db.Pool)
	ctx := context.Background()

	user, _ := auth.NewUser("bob@example.com", "strongpassword1", auth.RoleOperator)
	require.NoError(t, repo.Create(ctx, user))

	got, err := repo.GetByEmail(ctx, "bob@example.com")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, user.ID, got.ID)

	// Not found
	notFound, err := repo.GetByEmail(ctx, "nobody@example.com")
	require.NoError(t, err)
	assert.Nil(t, notFound)
}

func TestUserRepo_List(t *testing.T) {
	db := setupTestDB(t)
	repo := postgres.NewUserRepo(db.Pool)
	ctx := context.Background()

	u1, _ := auth.NewUser("first@example.com", "strongpassword1", auth.RoleAdmin)
	u2, _ := auth.NewUser("second@example.com", "strongpassword1", auth.RoleViewer)
	require.NoError(t, repo.Create(ctx, u1))
	time.Sleep(time.Millisecond) // ensure ordering
	require.NoError(t, repo.Create(ctx, u2))

	users, err := repo.List(ctx)
	require.NoError(t, err)
	assert.Len(t, users, 2)
	assert.Equal(t, "first@example.com", users[0].Email)
	assert.Equal(t, "second@example.com", users[1].Email)
}

func TestUserRepo_Update(t *testing.T) {
	db := setupTestDB(t)
	repo := postgres.NewUserRepo(db.Pool)
	ctx := context.Background()

	user, _ := auth.NewUser("charlie@example.com", "strongpassword1", auth.RoleViewer)
	require.NoError(t, repo.Create(ctx, user))

	user.UpdateRole(auth.RoleAdmin)
	require.NoError(t, repo.Update(ctx, user))

	got, _ := repo.GetByID(ctx, user.ID)
	assert.Equal(t, auth.RoleAdmin, got.Role)
}

func TestUserRepo_Delete(t *testing.T) {
	db := setupTestDB(t)
	repo := postgres.NewUserRepo(db.Pool)
	ctx := context.Background()

	user, _ := auth.NewUser("delete@example.com", "strongpassword1", auth.RoleViewer)
	require.NoError(t, repo.Create(ctx, user))

	require.NoError(t, repo.Delete(ctx, user.ID))

	got, err := repo.GetByID(ctx, user.ID)
	require.NoError(t, err)
	assert.Nil(t, got)
}

func TestUserRepo_Count(t *testing.T) {
	db := setupTestDB(t)
	repo := postgres.NewUserRepo(db.Pool)
	ctx := context.Background()

	count, err := repo.Count(ctx)
	require.NoError(t, err)
	assert.Equal(t, 0, count)

	u, _ := auth.NewUser("count@example.com", "strongpassword1", auth.RoleAdmin)
	require.NoError(t, repo.Create(ctx, u))

	count, err = repo.Count(ctx)
	require.NoError(t, err)
	assert.Equal(t, 1, count)
}
