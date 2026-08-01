package container

import (
	"strings"
	"time"
)

// ContainerStatus represents the runtime state of a container.
type ContainerStatus string

const (
	StatusCreated    ContainerStatus = "created"
	StatusRunning    ContainerStatus = "running"
	StatusPaused     ContainerStatus = "paused"
	StatusRestarting ContainerStatus = "restarting"
	StatusRemoving   ContainerStatus = "removing"
	StatusExited     ContainerStatus = "exited"
	StatusDead       ContainerStatus = "dead"
)

// HealthStatus represents the health check state of a container.
type HealthStatus string

const (
	HealthHealthy   HealthStatus = "healthy"
	HealthUnhealthy HealthStatus = "unhealthy"
	HealthStarting  HealthStatus = "starting"
	HealthNone      HealthStatus = "none"
)

// Container is a running (or stopped) Docker container within a stack.
type Container struct {
	ID            string // short 12-char Docker ID
	Name          string
	StackName     string // compose project name
	ServiceName   string // compose service name
	Image         string // human image reference (e.g. ghcr.io/foo:latest)
	ImageID       string // resolved local image digest (sha256:...) — changes when a mutable tag is repulled
	Status        ContainerStatus
	Health        HealthStatus
	ExitCode      int    // exit code (only meaningful when Status == exited)
	RestartPolicy string // "no", "always", "on-failure", "unless-stopped"
	// ComposeOneOff is docker compose's own `com.docker.compose.oneoff` label:
	// "True" for a `docker compose run` container, "False" for a service
	// container, "" when unlabelled (not compose-managed). Unlike RestartPolicy
	// this IS available from the container LIST API, which is what the stack
	// page reads.
	ComposeOneOff string
	Ports         []PortBinding
	CreatedAt     time.Time
	StartedAt     time.Time
}

// PortBinding maps a container port to a host port.
type PortBinding struct {
	HostIP        string
	HostPort      int
	ContainerPort int
	Protocol      string
}

// IsRunning returns true if the container is in the running state.
func (c *Container) IsRunning() bool {
	return c.Status == StatusRunning
}

// IsOneOff returns true if this container is a non-persistent task rather than
// a service - an init container, migration runner, restore job, or a
// `docker compose run` invocation.
//
// Compose's own label is authoritative when present. Otherwise fall back to the
// restart policy, which only the INSPECT path populates.
//
// An unknown classification must resolve to false. It previously resolved to
// true (via `RestartPolicy == ""`), and since the list path never populated
// RestartPolicy at all, EVERY exit-0 container in the stack view was reported
// as a completed one-off - so stopping a long-running service rendered it as
// "completed" instead of "exited". Guessing "task" from absent data turns a
// missing signal into an affirmative false claim about the container.
func (c *Container) IsOneOff() bool {
	if c.ComposeOneOff != "" {
		return strings.EqualFold(c.ComposeOneOff, "true")
	}
	return c.RestartPolicy == "no" || c.RestartPolicy == "on-failure"
}

// IsCompletedOneOff returns true if this container exited successfully (code 0)
// and is a one-off task. Used to display the "completed" badge in the UI.
func (c *Container) IsCompletedOneOff() bool {
	return c.Status == StatusExited && c.ExitCode == 0 && c.IsOneOff()
}
