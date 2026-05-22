package api_test

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/go-chi/chi/v5"

	composer "github.com/erfianugrah/composer"
	"github.com/erfianugrah/composer/internal/api"
)

// buildSpec returns the canonical build-time OpenAPI spec — same path the
// `dumpopenapi` binary takes. Tests below assert various invariants on it.
func buildSpec(t *testing.T) *huma.OpenAPI {
	t.Helper()
	router := chi.NewMux()
	apiInstance := humachi.New(router, api.HumaConfig(composer.Version))
	api.RegisterHumaHandlers(apiInstance, api.Deps{}, true)
	api.DocumentRawRoutes(apiInstance)
	return apiInstance.OpenAPI()
}

// TestSpecMatchesCommitted ensures the build-time dumped spec matches the
// committed web/src/lib/api/openapi.json. Same guarantee CI's `make generate
// && git diff --exit-code` enforces, but available in local `go test` runs.
func TestSpecMatchesCommitted(t *testing.T) {
	got, err := json.MarshalIndent(buildSpec(t), "", "  ")
	if err != nil {
		t.Fatalf("marshal generated spec: %v", err)
	}

	repoRoot := findRepoRoot(t)
	committedPath := filepath.Join(repoRoot, "web", "src", "lib", "api", "openapi.json")
	want, err := os.ReadFile(committedPath)
	if err != nil {
		t.Fatalf("read committed spec %s: %v", committedPath, err)
	}

	if string(got) != string(want) {
		t.Errorf("runtime-generated spec drifts from committed %s.\n"+
			"Run `make generate` and commit the result.", committedPath)
	}
}

// TestSpecTagsConsistent guards the original drift class: a tag declared in
// HumaConfig but no operation carries it (or vice versa). The whole point of
// this refactor is preventing that, so the test fails loudly if it recurs.
func TestSpecTagsConsistent(t *testing.T) {
	spec := buildSpec(t)

	declared := make(map[string]bool, len(spec.Tags))
	for _, tag := range spec.Tags {
		declared[tag.Name] = true
	}

	used := make(map[string]bool)
	for _, pathItem := range spec.Paths {
		for _, op := range []*huma.Operation{
			pathItem.Get, pathItem.Post, pathItem.Put, pathItem.Patch,
			pathItem.Delete, pathItem.Head, pathItem.Options, pathItem.Trace,
		} {
			if op == nil {
				continue
			}
			for _, tag := range op.Tags {
				used[tag] = true
			}
		}
	}

	for tag := range declared {
		if !used[tag] {
			t.Errorf("tag %q declared in HumaConfig but not used by any operation", tag)
		}
	}
	for tag := range used {
		if !declared[tag] {
			t.Errorf("tag %q used by an operation but not declared in HumaConfig", tag)
		}
	}
}

// TestSpecHasMinimumPaths is a coarse smoke test: catches the regression where
// RegisterHumaHandlers gets gutted by accident and the dumper produces a
// near-empty spec. Threshold deliberately well below current count (~82) so
// it doesn't churn with normal API growth/shrinkage.
func TestSpecHasMinimumPaths(t *testing.T) {
	spec := buildSpec(t)
	const min = 50
	if len(spec.Paths) < min {
		t.Errorf("spec has only %d paths, expected at least %d — RegisterHumaHandlers likely broken",
			len(spec.Paths), min)
	}
}

// TestSpecDocumentsRawRoutes pins the raw-chi-handler documentation stubs.
// If someone removes DocumentRawRoutes from the call site, this fails.
func TestSpecDocumentsRawRoutes(t *testing.T) {
	spec := buildSpec(t)
	for _, path := range []string{
		"/api/v1/auth/oauth/{provider}",
		"/api/v1/auth/oauth/{provider}/callback",
		"/api/v1/hooks/{id}",
	} {
		if _, ok := spec.Paths[path]; !ok {
			t.Errorf("spec missing raw-route documentation for %s", path)
		}
	}
}

func findRepoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for i := 0; i < 8; i++ {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	out, err := exec.Command("git", "rev-parse", "--show-toplevel").Output()
	if err != nil {
		t.Fatalf("locate repo root: %v", err)
	}
	return string(out[:len(out)-1])
}
