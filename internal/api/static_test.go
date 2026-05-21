package api

import (
	"embed"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
)

//go:embed testdata/dist
var testFrontend embed.FS

func testFrontendFS(t *testing.T) fs.FS {
	t.Helper()
	sub, err := fs.Sub(testFrontend, "testdata/dist")
	if err != nil {
		t.Fatalf("sub: %v", err)
	}
	return sub
}

// TestRegisterStaticFiles_SPAFallback verifies the walk-up index.html lookup
// added to support React-Router-driven sub-routes. Without it, refreshing on
// /stacks/myapp/logs would serve the root /index.html (Dashboard) instead of
// /stacks/index.html (Stacks shell that owns those routes).
func TestRegisterStaticFiles_SPAFallback(t *testing.T) {
	router := chi.NewMux()
	registerStaticFilesFS(router, testFrontendFS(t))

	cases := []struct {
		name     string
		path     string
		wantBody string // substring match
	}{
		{"root", "/", "ROOT"},
		{"stacks index", "/stacks", "STACKS"},
		{"stacks subroute -> stacks index", "/stacks/myapp", "STACKS"},
		{"stacks nested subroute -> stacks index", "/stacks/myapp/logs", "STACKS"},
		{"deeply nested unknown -> stacks index", "/stacks/a/b/c/d", "STACKS"},
		{"unrelated unknown -> root SPA fallback", "/totally-unknown-path", "ROOT"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tc.path, nil)
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200", rec.Code)
			}
			if !strings.Contains(rec.Body.String(), tc.wantBody) {
				t.Fatalf("body = %q, want substring %q", rec.Body.String(), tc.wantBody)
			}
		})
	}
}
