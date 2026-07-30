package app_test

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/erfianugrah/composer/internal/app"
	"github.com/erfianugrah/composer/internal/domain/host"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
)

// memHostRepo is a minimal in-memory host.Repository for unit tests.
type memHostRepo struct {
	mu          sync.Mutex
	rows        map[int64]*host.Host
	byName      map[string]*host.Host
	next        int64
	stackCounts map[int64]int
}

func newMemHostRepo() *memHostRepo {
	return &memHostRepo{
		rows:        map[int64]*host.Host{},
		byName:      map[string]*host.Host{},
		stackCounts: map[int64]int{},
	}
}

func (r *memHostRepo) Create(_ context.Context, h *host.Host) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.byName[h.Name]; ok {
		return fmt.Errorf("duplicate host name %q", h.Name)
	}
	r.next++
	h.ID = r.next
	r.rows[h.ID] = h
	r.byName[h.Name] = h
	return nil
}

func (r *memHostRepo) GetByID(_ context.Context, id int64) (*host.Host, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	h, ok := r.rows[id]
	if !ok {
		return nil, nil
	}
	return h, nil
}

func (r *memHostRepo) GetByName(_ context.Context, name string) (*host.Host, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	h, ok := r.byName[name]
	if !ok {
		return nil, nil
	}
	return h, nil
}

func (r *memHostRepo) List(_ context.Context) ([]*host.Host, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]*host.Host, 0, len(r.rows))
	for _, h := range r.rows {
		out = append(out, h)
	}
	return out, nil
}

func (r *memHostRepo) Update(_ context.Context, h *host.Host) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	old, ok := r.rows[h.ID]
	if !ok {
		return nil
	}
	delete(r.byName, old.Name)
	r.rows[h.ID] = h
	r.byName[h.Name] = h
	return nil
}

func (r *memHostRepo) Delete(_ context.Context, id int64) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if h, ok := r.rows[id]; ok {
		delete(r.byName, h.Name)
		delete(r.rows, id)
	}
	return nil
}

func (r *memHostRepo) CountStacks(_ context.Context, id int64) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.stackCounts[id], nil
}

func (r *memHostRepo) setStackCount(id int64, n int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.stackCounts[id] = n
}

func TestHostServiceCreateValidates(t *testing.T) {
	repo := newMemHostRepo()
	svc := app.NewHostService(repo, zaptest.NewLogger(t))
	ctx := context.Background()

	// Invalid name -> error
	err := svc.Create(ctx, &host.Host{Name: "", Endpoint: "tcp://x:2375"})
	assert.Error(t, err)

	// Valid -> succeeds
	err = svc.Create(ctx, &host.Host{Name: "remote1", Endpoint: "tcp://x:2375"})
	require.NoError(t, err)

	// Duplicate name -> repo rejects
	err = svc.Create(ctx, &host.Host{Name: "remote1", Endpoint: "tcp://y:2375"})
	assert.Error(t, err)
}

func TestHostServiceDeleteGuard(t *testing.T) {
	repo := newMemHostRepo()
	svc := app.NewHostService(repo, zaptest.NewLogger(t))
	ctx := context.Background()

	require.NoError(t, svc.Create(ctx, &host.Host{Name: "busy", Endpoint: "tcp://x:2375"}))
	h, err := svc.List(ctx)
	require.NoError(t, err)
	require.Len(t, h, 1)

	// Set stack count > 0 on the repo
	repo.setStackCount(h[0].ID, 3)

	// Delete should fail
	err = svc.Delete(ctx, h[0].ID)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "3 stack")

	// After clearing stack count, delete succeeds
	repo.setStackCount(h[0].ID, 0)
	err = svc.Delete(ctx, h[0].ID)
	require.NoError(t, err)
}

func TestHostServiceResolve(t *testing.T) {
	repo := newMemHostRepo()
	svc := app.NewHostService(repo, zaptest.NewLogger(t))
	ctx := context.Background()

	require.NoError(t, svc.Create(ctx, &host.Host{Name: "remote1", Endpoint: "tcp://x:2375"}))
	h, _ := repo.GetByName(ctx, "remote1")
	require.NotNil(t, h)

	// Empty name -> nil, nil (default host)
	id, err := svc.ResolveHostID(ctx, "")
	require.NoError(t, err)
	assert.Nil(t, id)

	// "local" -> nil, nil (default host)
	id, err = svc.ResolveHostID(ctx, "local")
	require.NoError(t, err)
	assert.Nil(t, id)

	// Known remote -> its ID
	id, err = svc.ResolveHostID(ctx, "remote1")
	require.NoError(t, err)
	require.NotNil(t, id)
	assert.Equal(t, h.ID, *id)

	// Unknown -> error
	_, err = svc.ResolveHostID(ctx, "nope")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unknown docker host")
}

func TestHostService_OnHostCreated(t *testing.T) {
	repo := newMemHostRepo()
	svc := app.NewHostService(repo, zaptest.NewLogger(t))
	ctx := context.Background()

	var calledHostID int64
	var calledHostName string
	svc.OnHostCreated = func(ctx context.Context, hostID int64, hostName string) {
		calledHostID = hostID
		calledHostName = hostName
	}

	err := svc.Create(ctx, &host.Host{Name: "remote2", Endpoint: "tcp://x:2375"})
	require.NoError(t, err)
	assert.Equal(t, "remote2", calledHostName)
	assert.Greater(t, calledHostID, int64(0))

	// Nil callback is safe (no panic). Create still succeeds.
	svcNil := app.NewHostService(repo, zaptest.NewLogger(t))
	svcNil.OnHostCreated = nil
	err = svcNil.Create(ctx, &host.Host{Name: "remote3", Endpoint: "tcp://y:2375"})
	require.NoError(t, err)
}
