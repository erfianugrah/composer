package handler_test

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/go-chi/chi/v5"

	"github.com/erfianugrah/composer/internal/api/handler"
	"github.com/erfianugrah/composer/internal/app"
	"github.com/erfianugrah/composer/internal/domain/host"
	"go.uber.org/zap/zaptest"
)

// fakeHostRepo is a minimal in-memory host.Repository for handler tests.
type fakeHostRepo struct {
	hosts       map[int64]*host.Host
	byName      map[string]*host.Host
	next        int64
	stackCounts map[int64]int
}

func newFakeHostRepo() *fakeHostRepo {
	return &fakeHostRepo{
		hosts:       map[int64]*host.Host{},
		byName:      map[string]*host.Host{},
		stackCounts: map[int64]int{},
	}
}

func (r *fakeHostRepo) Create(_ context.Context, h *host.Host) error {
	r.next++
	h.ID = r.next
	r.hosts[h.ID] = h
	r.byName[h.Name] = h
	return nil
}

func (r *fakeHostRepo) GetByID(_ context.Context, id int64) (*host.Host, error) {
	return r.hosts[id], nil
}

func (r *fakeHostRepo) GetByName(_ context.Context, name string) (*host.Host, error) {
	return r.byName[name], nil
}

func (r *fakeHostRepo) List(_ context.Context) ([]*host.Host, error) {
	out := make([]*host.Host, 0, len(r.hosts))
	for _, h := range r.hosts {
		out = append(out, h)
	}
	return out, nil
}

func (r *fakeHostRepo) Update(_ context.Context, h *host.Host) error {
	delete(r.byName, r.hosts[h.ID].Name)
	r.hosts[h.ID] = h
	r.byName[h.Name] = h
	return nil
}

func (r *fakeHostRepo) Delete(_ context.Context, id int64) error {
	if h, ok := r.hosts[id]; ok {
		delete(r.byName, h.Name)
	}
	delete(r.hosts, id)
	return nil
}

func (r *fakeHostRepo) CountStacks(_ context.Context, id int64) (int, error) {
	return r.stackCounts[id], nil
}

func setupHostAPI(t *testing.T) (huma.API, http.Handler) {
	t.Helper()
	repo := newFakeHostRepo()
	svc := app.NewHostService(repo, zaptest.NewLogger(t))
	router := chi.NewMux()
	cfg := huma.DefaultConfig("test", "0.0.0")
	cfg.OpenAPI.Security = []map[string][]string{} // no auth for tests
	api := humachi.New(router, cfg)
	handler.NewHostHandler(svc).Register(api)
	return api, router
}

func TestListHosts(t *testing.T) {
	_, router := setupHostAPI(t)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/v1/hosts", nil)
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestCreateHost(t *testing.T) {
	_, router := setupHostAPI(t)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/api/v1/hosts",
		body(`{"name":"remote1","endpoint":"tcp://docker-remote.example:2376"}`))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK && w.Code != http.StatusCreated {
		t.Fatalf("expected 200/201, got %d: %s", w.Code, w.Body.String())
	}
}

func TestCreateHostInvalidEndpoint(t *testing.T) {
	_, router := setupHostAPI(t)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/api/v1/hosts",
		body(`{"name":"bad","endpoint":"http://x"}`))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)

	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422, got %d: %s", w.Code, w.Body.String())
	}
}

func body(s string) *bytes.Reader { return bytes.NewReader([]byte(s)) }
