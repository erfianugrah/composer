package selfupgrade

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseCgroupContainerID_NonContainer(t *testing.T) {
	// Standard host /proc/self/cgroup should return empty string.
	result := ParseCgroupContainerID("0::/user.slice/user-1000.slice/session-3.scope\n")
	assert.Empty(t, result)
}

func TestParseCgroupContainerID_Empty(t *testing.T) {
	assert.Empty(t, ParseCgroupContainerID(""))
}

func TestParseCgroupContainerID_NoDocker(t *testing.T) {
	assert.Empty(t, ParseCgroupContainerID(
		"12:memory:/user.slice/user-1000.slice/session-1.scope\n"+
			"11:devices:/user.slice\n"))
}

func TestParseComposeProject_NotCompose(t *testing.T) {
	labels := map[string]string{"some.other.label": "value"}
	_, ok := ParseComposeProject(labels)
	assert.False(t, ok)
}

func TestParseComposeProject_NilMap(t *testing.T) {
	_, ok := ParseComposeProject(nil)
	assert.False(t, ok)
}

func TestParseComposeProject_Compose(t *testing.T) {
	labels := map[string]string{
		"com.docker.compose.project":              "composer",
		"com.docker.compose.project.working_dir":   "/opt/stacks/composer",
		"com.docker.compose.project.config_files":  "compose.yaml,docker-compose.yaml",
		"com.docker.compose.project.environment_file": ".env",
	}
	cp, ok := ParseComposeProject(labels)
	require.True(t, ok)
	assert.Equal(t, "composer", cp.Name)
	assert.Equal(t, "/opt/stacks/composer", cp.WorkingDir)
	assert.Equal(t, []string{"compose.yaml", "docker-compose.yaml"}, cp.ConfigFiles)
	assert.Equal(t, ".env", cp.EnvironmentFile)
}

func TestParseComposeProject_NoConfigFiles(t *testing.T) {
	labels := map[string]string{
		"com.docker.compose.project": "composer",
	}
	cp, ok := ParseComposeProject(labels)
	require.True(t, ok)
	assert.Equal(t, "composer", cp.Name)
	assert.Nil(t, cp.ConfigFiles)
}

func TestReconstructRunSpec_UnraidFixture(t *testing.T) {
	fixturePath := filepath.Join("..", "..", "conformance", "selfupgrade", "testdata", "inspect-unraid.json")
	data, err := os.ReadFile(fixturePath)
	require.NoError(t, err)

	spec, err := ReconstructRunSpec(data)
	require.NoError(t, err)

	assert.Equal(t, "composer", spec.Name)
	assert.Equal(t, "ghcr.io/erfianugrah/composer:latest-amd64", spec.Image)
	assert.Contains(t, spec.Env, "COMPOSER_PORT=8080")
	assert.Equal(t, "unless-stopped", spec.RestartPolicy)
	assert.Equal(t, "proxynet", spec.NetworkMode)

	// Port bindings.
	bindings, ok := spec.PortBindings["8080/tcp"]
	require.True(t, ok)
	require.Len(t, bindings, 1)
	assert.Equal(t, "8080", bindings[0].HostPort)

	// DockerRunArgs with a new image.
	args, err := spec.DockerRunArgs("ghcr.io/erfianugrah/composer:v0.16.0")
	require.NoError(t, err)
	assert.Contains(t, args, "--name", "composer")
	assert.Contains(t, args, "--network", "proxynet")
	assert.Contains(t, args, "--restart", "unless-stopped")
	// Image is the last arg.
	assert.Equal(t, "ghcr.io/erfianugrah/composer:v0.16.0", args[len(args)-1])
}

func TestReconstructRunSpec_InvalidJSON(t *testing.T) {
	_, err := ReconstructRunSpec([]byte("not json"))
	assert.Error(t, err)
}

func TestDockerRunArgs_EmptyImage(t *testing.T) {
	spec := &RunSpec{Name: "test"}
	_, err := spec.DockerRunArgs("")
	assert.Error(t, err)
}

func TestDetectDeploymentType_Compose(t *testing.T) {
	labels := map[string]string{
		"com.docker.compose.project": "composer",
	}
	assert.Equal(t, DeployCompose, DetectDeploymentType(labels))
}

func TestDetectDeploymentType_DockerRun(t *testing.T) {
	labels := map[string]string{"some.label": "value"}
	assert.Equal(t, DeployDockerRun, DetectDeploymentType(labels))
}

func TestDetectDeploymentType_Unknown(t *testing.T) {
	assert.Equal(t, DeployUnknown, DetectDeploymentType(nil))
	assert.Equal(t, DeployUnknown, DetectDeploymentType(map[string]string{}))
}

func TestSelfContainerID_EnvOverride(t *testing.T) {
	t.Setenv("COMPOSER_SELF_CONTAINER_ID", "deadbeef1234")
	assert.Equal(t, "deadbeef1234", SelfContainerID())
}

func TestValidateImage_AllowedPrefix(t *testing.T) {
	assert.NoError(t, validateImage("ghcr.io/erfianugrah/composer:latest"))
	assert.NoError(t, validateImage("ghcr.io/erfianugrah/composer:v0.16.0"))
}

func TestValidateImage_DisallowedPrefix(t *testing.T) {
	err := validateImage("docker.io/evil/image:latest")
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrInvalidImage)
}

func TestRunSpecMarshalRoundtrip(t *testing.T) {
	original := &RunSpec{
		Name:          "test-container",
		Image:         "test:latest",
		Env:           []string{"A=1", "B=2"},
		Labels:        map[string]string{"key": "val"},
		Binds:         []string{"/host:/container:rw"},
		NetworkMode:   "host",
		RestartPolicy: "unless-stopped",
		CapAdd:        []string{"NET_ADMIN"},
		CapDrop:       []string{"ALL"},
		SecurityOpt:   []string{"no-new-privileges:true"},
		Privileged:    false,
	}

	// Build JSON that matches the Docker inspect format for nat.PortMap.
	// PortBindings is map[string][]json.RawMessage in the raw inspect, but
	// ReconstructRunSpec uses nat.PortMap which decodes to map[nat.Port][]nat.PortBinding.
	// Use the actual Docker SDK types to match.
	type inspectHostConfig struct {
		Binds         []string                     `json:"Binds"`
		PortBindings  map[string]json.RawMessage    `json:"PortBindings"`
		NetworkMode   string                       `json:"NetworkMode"`
		RestartPolicy struct {
			Name string `json:"Name"`
		} `json:"RestartPolicy"`
		CapAdd      []string `json:"CapAdd"`
		CapDrop     []string `json:"CapDrop"`
		SecurityOpt []string `json:"SecurityOpt"`
		Privileged  bool     `json:"Privileged"`
	}
	hc := inspectHostConfig{
		Binds:       original.Binds,
		NetworkMode: original.NetworkMode,
		CapAdd:      original.CapAdd,
		CapDrop:     original.CapDrop,
		SecurityOpt: original.SecurityOpt,
		Privileged:  original.Privileged,
	}
	hc.RestartPolicy.Name = original.RestartPolicy
	// PortBindings: Docker inspect uses a map of port key -> []PortBinding
	// where PortBinding is {HostIp, HostPort}. Encode as raw JSON.
	hc.PortBindings = map[string]json.RawMessage{
		"8080/tcp": json.RawMessage(`[{"HostIp":"","HostPort":"8080"}]`),
	}

	raw := struct {
		Name   string `json:"Name"`
		Config struct {
			Image  string            `json:"Image"`
			Env    []string          `json:"Env"`
			Labels map[string]string `json:"Labels"`
		} `json:"Config"`
		HostConfig inspectHostConfig `json:"HostConfig"`
	}{
		Name: "/" + original.Name,
	}
	raw.Config.Image = original.Image
	raw.Config.Env = original.Env
	raw.Config.Labels = original.Labels
	raw.HostConfig = hc

	data, err := json.Marshal(raw)
	require.NoError(t, err)

	spec, err := ReconstructRunSpec(data)
	require.NoError(t, err)

	assert.Equal(t, original.Name, spec.Name)
	assert.Equal(t, original.Image, spec.Image)
	assert.Equal(t, original.Binds, spec.Binds)
	assert.Equal(t, original.NetworkMode, spec.NetworkMode)
	assert.Equal(t, original.RestartPolicy, spec.RestartPolicy)
	require.Len(t, spec.PortBindings, 1)
	assert.Equal(t, "8080", spec.PortBindings["8080/tcp"][0].HostPort)
}
