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
	ID          string // short 12-char Docker ID
	Name        string
	StackName   string // compose project name
	ServiceName string // compose service name
	Image       string
	Status      ContainerStatus
	Health      HealthStatus
	Ports       []PortBinding
	CreatedAt   time.Time
	StartedAt   time.Time
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
