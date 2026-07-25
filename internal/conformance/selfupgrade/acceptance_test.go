// Package selfupgrade_test is the ACCEPTANCE CONTRACT for the self-upgrade
// feature specified in SELF_UPGRADE_PLAN.md (rev 3).
//
// These tests are OUTSIDE the implementation loop's write scope: implement
// against them, do not modify them. They pin:
//
//   - package path: github.com/erfianugrah/composer/internal/app/selfupgrade
//   - public API names and signatures (below)
//   - the OpenAPI routes the feature must expose
//
// Everything pinned here is a pure function or a static artifact, so the
// suite runs without Docker.
package selfupgrade_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/erfianugrah/composer/internal/app/selfupgrade"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testdata(t *testing.T, name string) string {
	t.Helper()
	_, thisFile, _, _ := runtime.Caller(0)
	data, err := os.ReadFile(filepath.Join(filepath.Dir(thisFile), "testdata", name))
	require.NoError(t, err)
	return string(data)
}

// --- SelfContainerID support: pure cgroup parser --------------------------

// Pinned: func ParseCgroupContainerID(content string) string
// Returns the container ID parsed from /proc/self/cgroup content, or "".
func TestParseCgroupContainerID(t *testing.T) {
	const id64 = "c0ffee1234567890abcdef1234567890abcdef1234567890abcdef12345678"

	cases := []struct {
		name    string
		content string
		want    string
	}{
		{
			name:    "cgroup v1 docker",
			content: "12:devices:/docker/" + id64 + "\n11:cpu,cpuacct:/docker/" + id64 + "\n",
			want:    id64,
		},
		{
			name:    "cgroup v2 docker",
			content: "0::/docker/" + id64 + "\n",
			want:    id64,
		},
		{
			name:    "cgroup v2 kubepods",
			content: "0::/kubepods/besteffort/pod9f2b0c1a-1234-5678-9abc-def012345678/" + id64 + "\n",
			want:    id64,
		},
		{
			name:    "cgroup v1 containerd-style suffix",
			content: "10:memory:/system.slice/docker-" + id64 + ".scope\n",
			want:    id64,
		},
		{
			name:    "not a container",
			content: "0::/\n",
			want:    "",
		},
		{
			name:    "empty",
			content: "",
			want:    "",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, selfupgrade.ParseCgroupContainerID(tc.content))
		})
	}
}

// --- Compose deployment detection: pure label extraction -------------------

// Pinned:
//
//	type ComposeProject struct {
//	    Name            string   // com.docker.compose.project
//	    WorkingDir      string   // com.docker.compose.project.working_dir
//	    ConfigFiles     []string // com.docker.compose.project.config_files (comma-separated, split)
//	    EnvironmentFile string   // com.docker.compose.project.environment_file (may be "")
//	}
//	func ParseComposeProject(labels map[string]string) (ComposeProject, bool)
//
// The bool reports whether the labels describe a compose deployment.
func TestParseComposeProject(t *testing.T) {
	t.Run("full label set, multiple config files", func(t *testing.T) {
		labels := map[string]string{
			"com.docker.compose.project":                  "composer",
			"com.docker.compose.project.working_dir":      "/srv/composer/deploy",
			"com.docker.compose.project.config_files":     "/srv/composer/deploy/compose.yaml,/srv/composer/deploy/compose.override.yaml",
			"com.docker.compose.project.environment_file": "/srv/composer/deploy/.env",
			"com.docker.compose.service":                  "composer",
			"com.docker.compose.config-hash":              "abc123",
		}
		p, ok := selfupgrade.ParseComposeProject(labels)
		require.True(t, ok, "compose labels present must report compose deployment")
		assert.Equal(t, "composer", p.Name)
		assert.Equal(t, "/srv/composer/deploy", p.WorkingDir)
		assert.Equal(t, []string{
			"/srv/composer/deploy/compose.yaml",
			"/srv/composer/deploy/compose.override.yaml",
		}, p.ConfigFiles)
		assert.Equal(t, "/srv/composer/deploy/.env", p.EnvironmentFile)
	})

	t.Run("no env file", func(t *testing.T) {
		labels := map[string]string{
			"com.docker.compose.project":              "composer",
			"com.docker.compose.project.working_dir":  "/srv/composer/deploy",
			"com.docker.compose.project.config_files": "/srv/composer/deploy/compose.yaml",
		}
		p, ok := selfupgrade.ParseComposeProject(labels)
		require.True(t, ok)
		assert.Equal(t, "", p.EnvironmentFile)
		assert.Len(t, p.ConfigFiles, 1)
	})

	t.Run("plain docker run (Unraid) has no compose labels", func(t *testing.T) {
		labels := map[string]string{
			"net.unraid.docker.managed": "dockerman",
			"net.unraid.docker.icon":    "https://example.invalid/icon.png",
		}
		_, ok := selfupgrade.ParseComposeProject(labels)
		assert.False(t, ok, "no compose project label -> not a compose deployment")
	})

	t.Run("nil labels", func(t *testing.T) {
		_, ok := selfupgrade.ParseComposeProject(nil)
		assert.False(t, ok)
	})
}

// --- docker-run reconstruction (Unraid path) -------------------------------

// Pinned:
//
//	type RunSpec struct{ ... } // fields NOT pinned
//	func ReconstructRunSpec(inspectJSON []byte) (*RunSpec, error)
//	func (s *RunSpec) DockerRunArgs(newImage string) ([]string, error)
//
// DockerRunArgs renders the equivalent `docker run` argument vector with the
// image replaced by newImage.
func TestReconstructRunSpec_UnraidFixture(t *testing.T) {
	spec, err := selfupgrade.ReconstructRunSpec([]byte(testdata(t, "inspect-unraid.json")))
	require.NoError(t, err)
	require.NotNil(t, spec)

	args, err := spec.DockerRunArgs("ghcr.io/erfianugrah/composer:9.9.9-amd64")
	require.NoError(t, err)
	joined := strings.Join(args, " ")

	// identity
	assert.Contains(t, joined, "--name composer")
	// env round-trips
	assert.Contains(t, joined, "PUID=99")
	assert.Contains(t, joined, "PGID=100")
	assert.Contains(t, joined, "COMPOSER_DATA_DIR=/opt/composer")
	// binds round-trip (docker socket MUST be preserved rw)
	assert.Contains(t, joined, "/var/run/docker.sock:/var/run/docker.sock")
	assert.Contains(t, joined, "/mnt/user/appdata/composer:/opt/composer")
	// published port round-trips
	assert.Contains(t, joined, "8080:8080")
	// restart policy round-trips
	assert.Contains(t, joined, "--restart unless-stopped")
	// Unraid template metadata labels survive
	assert.Contains(t, joined, "net.unraid.docker.managed=dockerman")
	// the NEW image is used, not the old one
	assert.Contains(t, joined, "ghcr.io/erfianugrah/composer:9.9.9-amd64")
	assert.NotContains(t, joined, "latest-amd64")
	// image is the final positional argument
	assert.Equal(t, "ghcr.io/erfianugrah/composer:9.9.9-amd64", args[len(args)-1])
}

// --- OpenAPI surface --------------------------------------------------------

// The feature must expose (and `make generate` must commit) these routes.
func TestOpenAPISpecContainsUpgradeRoutes(t *testing.T) {
	_, thisFile, _, _ := runtime.Caller(0)
	specPath := filepath.Join(filepath.Dir(thisFile), "..", "..", "..", "web", "src", "lib", "api", "openapi.json")
	data, err := os.ReadFile(specPath)
	require.NoError(t, err)

	var spec struct {
		Paths map[string]map[string]json.RawMessage `json:"paths"`
	}
	require.NoError(t, json.Unmarshal(data, &spec))

	upgrade, ok := spec.Paths["/api/v1/system/upgrade"]
	require.True(t, ok, "openapi.json must declare /api/v1/system/upgrade")
	assert.Contains(t, upgrade, "post", "POST /api/v1/system/upgrade must exist")

	status, ok := spec.Paths["/api/v1/system/upgrade/status"]
	require.True(t, ok, "openapi.json must declare /api/v1/system/upgrade/status")
	assert.Contains(t, status, "get", "GET /api/v1/system/upgrade/status must exist")
}
