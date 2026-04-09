package container

import "time"

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
	Image         string
	Status        ContainerStatus
	Health        HealthStatus
	ExitCode      int    // exit code (only meaningful when Status == exited)
	RestartPolicy string // "no", "always", "on-failure", "unless-stopped"
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

// IsCompletedOneOff returns true if this container exited successfully (code 0)
// and is not configured to restart. These are init containers, migration runners,
// config generators, etc. that are expected to exit.
func (c *Container) IsCompletedOneOff() bool {
	return c.Status == StatusExited && c.ExitCode == 0 &&
		(c.RestartPolicy == "no" || c.RestartPolicy == "" || c.RestartPolicy == "on-failure")
}
