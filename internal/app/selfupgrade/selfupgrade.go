// Package selfupgrade implements the composer self-upgrade feature specified
// in SELF_UPGRADE_PLAN.md (rev 3).
//
// It provides pure functions for detecting the deployment environment (compose
// vs docker-run), self-container identification, and a SelfUpgradeService that
// orchestrates the upgrade via a detached helper container.
package selfupgrade

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/docker/go-connections/nat"
)

// ParseCgroupContainerID extracts a 64-hex-char container ID from
// /proc/self/cgroup content. Returns "" if no container ID is found.
//
// Supports cgroup v1 (docker/, containerd-*.scope suffix), cgroup v2
// (0::/docker/..., 0::/kubepods/...), and non-container paths.
func ParseCgroupContainerID(content string) string {
	lines := strings.Split(strings.TrimSpace(content), "\n")
	// Try cgroup v2 first (simpler format).
	for _, line := range lines {
		if strings.HasPrefix(line, "0::") {
			if id := extractContainerID(line); id != "" {
				return id
			}
		}
	}
	// Fall back to cgroup v1.
	for _, line := range lines {
		if id := extractContainerID(line); id != "" {
			return id
		}
	}
	return ""
}

func extractContainerID(line string) string {
	// cgroup v2: 0::/docker/<id>
	// cgroup v1: 12:devices:/docker/<id>
	// cgroup v1 containerd: 10:memory:/system.slice/docker-<id>.scope
	// cgroup v2 kubepods: 0::/kubepods/.../<id>

	// Try containerd-style suffix first.
	if i := strings.Index(line, "docker-"); i >= 0 {
		rest := line[i+7:] // after "docker-"
		if j := strings.Index(rest, ".scope"); j >= 0 {
			candidate := rest[:j]
			if isContainerID(candidate) {
				return candidate
			}
		}
	}

	// Try docker/ or /docker/ prefix.
	idx := strings.Index(line, "/docker/")
	if idx < 0 {
		idx = strings.Index(line, "docker/")
		if idx >= 0 && idx > 0 && line[idx-1] != '/' {
			idx = -1
		}
	}
	if idx >= 0 {
		rest := line[idx:]
		rest = strings.TrimPrefix(rest, "/")
		rest = strings.TrimPrefix(rest, "docker/")
		// Take until end of line or next '/'.
		if slash := strings.Index(rest, "/"); slash >= 0 {
			rest = rest[:slash]
		}
		if isContainerID(rest) {
			return rest
		}
	}

	// Try kubepods path.
	if kIdx := strings.Index(line, "/kubepods/"); kIdx >= 0 {
		rest := line[kIdx+len("/kubepods/"):]
		parts := strings.Split(rest, "/")
		if len(parts) >= 2 {
			candidate := parts[len(parts)-1]
			if isContainerID(candidate) {
				return candidate
			}
		}
	}

	return ""
}

func isContainerID(s string) bool {
	if len(s) < 12 || len(s) > 64 {
		return false
	}
	for _, c := range s {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
			return false
		}
	}
	return true
}

// --- Compose deployment detection ---

// ComposeProject holds compose label metadata extracted from container labels.
type ComposeProject struct {
	Name            string   // com.docker.compose.project
	WorkingDir      string   // com.docker.compose.project.working_dir
	ConfigFiles     []string // com.docker.compose.project.config_files (comma-separated, split)
	EnvironmentFile string   // com.docker.compose.project.environment_file (may be "")
}

// ParseComposeProject extracts compose project info from container labels.
// The bool reports whether the labels describe a compose deployment
// (i.e. com.docker.compose.project is set).
func ParseComposeProject(labels map[string]string) (ComposeProject, bool) {
	name := labels["com.docker.compose.project"]
	if name == "" {
		return ComposeProject{}, false
	}

	cf := labels["com.docker.compose.project.config_files"]
	var configFiles []string
	if cf != "" {
		configFiles = strings.Split(cf, ",")
	}

	return ComposeProject{
		Name:            name,
		WorkingDir:      labels["com.docker.compose.project.working_dir"],
		ConfigFiles:     configFiles,
		EnvironmentFile: labels["com.docker.compose.project.environment_file"],
	}, true
}

// --- docker-run reconstruction ---

// RunSpec holds the reconstructed docker run configuration extracted from a
// container inspect payload. Fields are not pinned by the conformance suite
// but the New-inspect constructor and DockerRunArgs method are.
type RunSpec struct {
	Name          string
	Image         string
	Env           []string
	Labels        map[string]string
	Binds         []string
	PortBindings  map[string][]PortBinding
	NetworkMode   string
	RestartPolicy string
	CapAdd        []string
	CapDrop       []string
	SecurityOpt   []string
	Privileged    bool
}

// PortBinding mirrors a single host→container port mapping.
type PortBinding struct {
	HostIP        string
	HostPort      string
	ContainerPort string
	Protocol      string
}

// ReconstructRunSpec parses a docker container inspect JSON payload into a
// RunSpec. Returns an error if the JSON is malformed.
func ReconstructRunSpec(inspectJSON []byte) (*RunSpec, error) {
	var raw struct {
		Name   string `json:"Name"`
		Config struct {
			Image  string            `json:"Image"`
			Env    []string          `json:"Env"`
			Labels map[string]string `json:"Labels"`
		} `json:"Config"`
		HostConfig struct {
			Binds         []string                     `json:"Binds"`
			PortBindings  nat.PortMap `json:"PortBindings"`
			NetworkMode   string                       `json:"NetworkMode"`
			RestartPolicy struct {
				Name              string `json:"Name"`
				MaximumRetryCount int    `json:"MaximumRetryCount"`
			} `json:"RestartPolicy"`
			CapAdd      []string `json:"CapAdd"`
			CapDrop     []string `json:"CapDrop"`
			SecurityOpt []string `json:"SecurityOpt"`
			Privileged  bool     `json:"Privileged"`
		} `json:"HostConfig"`
	}
	if err := json.Unmarshal(inspectJSON, &raw); err != nil {
		return nil, fmt.Errorf("parsing inspect JSON: %w", err)
	}

	spec := &RunSpec{
		Name:          strings.TrimPrefix(raw.Name, "/"),
		Image:         raw.Config.Image,
		Env:           raw.Config.Env,
		Labels:        raw.Config.Labels,
		Binds:         raw.HostConfig.Binds,
		NetworkMode:   raw.HostConfig.NetworkMode,
		RestartPolicy: raw.HostConfig.RestartPolicy.Name,
		CapAdd:        raw.HostConfig.CapAdd,
		CapDrop:       raw.HostConfig.CapDrop,
		SecurityOpt:   raw.HostConfig.SecurityOpt,
		Privileged:    raw.HostConfig.Privileged,
	}

	if raw.Config.Labels == nil {
		spec.Labels = map[string]string{}
	}

	spec.PortBindings = make(map[string][]PortBinding)
	for containerPort, bindings := range raw.HostConfig.PortBindings {
		pbs := make([]PortBinding, 0, len(bindings))
		for _, b := range bindings {
			pbs = append(pbs, PortBinding{
				HostIP:        b.HostIP,
				HostPort:      b.HostPort,
				ContainerPort: string(containerPort),
			})
		}
		spec.PortBindings[string(containerPort)] = pbs
	}

	return spec, nil
}

// DockerRunArgs renders the equivalent `docker run` argument vector, with the
// image replaced by newImage. The image is the final positional argument.
func (s *RunSpec) DockerRunArgs(newImage string) ([]string, error) {
	if newImage == "" {
		return nil, errors.New("newImage is required")
	}

	var args []string

	if s.Name != "" {
		args = append(args, "--name", s.Name)
	}

	// Environment variables.
	for _, env := range s.Env {
		args = append(args, "-e", env)
	}

	// Labels.
	for k, v := range s.Labels {
		args = append(args, "--label", k+"="+v)
	}

	// Volume binds.
	for _, bind := range s.Binds {
		args = append(args, "-v", bind)
	}

	// Published ports.
	for _, bindings := range s.PortBindings {
		for _, b := range bindings {
			publish := b.HostPort + ":" + stripProto(b.ContainerPort)
			if b.HostIP != "" {
				publish = b.HostIP + ":" + publish
			}
			args = append(args, "-p", publish)
		}
	}

	if s.NetworkMode != "" && s.NetworkMode != "default" {
		args = append(args, "--network", s.NetworkMode)
	}

	if s.RestartPolicy != "" && s.RestartPolicy != "no" {
		args = append(args, "--restart", s.RestartPolicy)
	}

	for _, cap := range s.CapAdd {
		args = append(args, "--cap-add", cap)
	}
	for _, cap := range s.CapDrop {
		args = append(args, "--cap-drop", cap)
	}
	for _, opt := range s.SecurityOpt {
		args = append(args, "--security-opt", opt)
	}

	if s.Privileged {
		args = append(args, "--privileged")
	}

	// Image is the final positional argument.
	args = append(args, newImage)

	return args, nil
}

// stripProto removes the protocol suffix from a container port string
// (e.g. "8080/tcp" → "8080").
func stripProto(s string) string {
	if i := strings.Index(s, "/"); i >= 0 {
		return s[:i]
	}
	return s
}

// SelfContainerID returns the ID of the container running this process.
// Checks COMPOSER_SELF_CONTAINER_ID env first, then parses /proc/self/cgroup,
// then falls back to hostname.
func SelfContainerID() string {
	if id := os.Getenv("COMPOSER_SELF_CONTAINER_ID"); id != "" {
		return id
	}

	data, err := os.ReadFile("/proc/self/cgroup")
	if err == nil {
		if id := ParseCgroupContainerID(string(data)); id != "" {
			return id
		}
	}

	// Fallback: use hostname.
	hostname, err := os.Hostname()
	if err == nil && len(hostname) == 12 {
		return hostname
	}
	return hostname
}

// DeploymentType describes how composer is deployed.
type DeploymentType string

const (
	DeployCompose   DeploymentType = "compose"
	DeployDockerRun DeploymentType = "docker_run"
	DeployUnknown   DeploymentType = "unknown"
)

// DetectDeploymentType inspects the container's labels and returns the
// deployment type. It must be called with self-container labels from the
// Docker engine (not from inside the container).
func DetectDeploymentType(labels map[string]string) DeploymentType {
	if _, ok := ParseComposeProject(labels); ok {
		return DeployCompose
	}
	// If there's a non-empty labels map but no compose project, assume docker run.
	if len(labels) > 0 {
		return DeployDockerRun
	}
	return DeployUnknown
}
