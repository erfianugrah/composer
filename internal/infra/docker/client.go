package docker

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/events"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/system"
	dockerclient "github.com/docker/docker/client"

	domcontainer "github.com/erfianugrah/composer/internal/domain/container"
)

// Client wraps the Docker Engine SDK.
type Client struct {
	cli     *dockerclient.Client
	runtime string // "docker" or "podman"
}

// NewClient creates a Docker/Podman client with auto-detection.
func NewClient(explicitHost string) (*Client, error) {
	host := explicitHost
	if host == "" {
		host = detectSocket()
	}

	opts := []dockerclient.Opt{
		dockerclient.WithHost(host),
		dockerclient.WithAPIVersionNegotiation(),
	}

	cli, err := dockerclient.NewClientWithOpts(opts...)
	if err != nil {
		return nil, fmt.Errorf("creating docker client: %w", err)
	}

	c := &Client{cli: cli}

	// Detect runtime
	info, err := cli.Info(context.Background())
	if err == nil {
		if strings.Contains(strings.ToLower(info.OperatingSystem), "podman") ||
			strings.Contains(strings.ToLower(info.Name), "podman") {
			c.runtime = "podman"
		} else {
			c.runtime = "docker"
		}
	} else {
		c.runtime = "unknown"
	}

	return c, nil
}

// Close closes the Docker client.
func (c *Client) Close() error {
	return c.cli.Close()
}

// Runtime returns "docker", "podman", or "unknown".
func (c *Client) Runtime() string {
	return c.runtime
}

// Host returns the Docker host URL.
func (c *Client) Host() string {
	return c.cli.DaemonHost()
}

// Info returns Docker system info.
func (c *Client) Info(ctx context.Context) (system.Info, error) {
	return c.cli.Info(ctx)
}

// Ping checks connectivity to the Docker daemon.
func (c *Client) Ping(ctx context.Context) error {
	_, err := c.cli.Ping(ctx)
	return err
}

// ListContainers returns all containers, optionally filtered by compose project label.
func (c *Client) ListContainers(ctx context.Context, stackName string) ([]domcontainer.Container, error) {
	opts := container.ListOptions{All: true}
	if stackName != "" {
		opts.Filters = filters.NewArgs(
			filters.Arg("label", "com.docker.compose.project="+stackName),
		)
	}

	containers, err := c.cli.ContainerList(ctx, opts)
	if err != nil {
		return nil, fmt.Errorf("listing containers: %w", err)
	}

	result := make([]domcontainer.Container, 0, len(containers))
	for _, ctr := range containers {
		result = append(result, toDomainContainer(ctr))
	}
	return result, nil
}

// InspectContainer returns detailed info about a container.
func (c *Client) InspectContainer(ctx context.Context, id string) (*domcontainer.Container, error) {
	ctr, err := c.cli.ContainerInspect(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("inspecting container %s: %w", id, err)
	}

	dc := inspectToDomain(ctr)
	return &dc, nil
}

// ContainerLogs returns a reader for container logs.
func (c *Client) ContainerLogs(ctx context.Context, id string, follow bool, tail string, since string) (io.ReadCloser, error) {
	opts := container.LogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Follow:     follow,
		Timestamps: true,
		Tail:       tail,
		Since:      since,
	}
	return c.cli.ContainerLogs(ctx, id, opts)
}

// ContainerStats returns a streaming reader for container stats.
func (c *Client) ContainerStats(ctx context.Context, id string, stream bool) (io.ReadCloser, error) {
	stats, err := c.cli.ContainerStats(ctx, id, stream)
	if err != nil {
		return nil, fmt.Errorf("getting stats for %s: %w", id, err)
	}
	return stats.Body, nil
}

// StartContainer starts a stopped container.
func (c *Client) StartContainer(ctx context.Context, id string) error {
	return c.cli.ContainerStart(ctx, id, container.StartOptions{})
}

// StopContainer stops a running container.
func (c *Client) StopContainer(ctx context.Context, id string) error {
	return c.cli.ContainerStop(ctx, id, container.StopOptions{})
}

// RestartContainer restarts a container.
func (c *Client) RestartContainer(ctx context.Context, id string) error {
	return c.cli.ContainerRestart(ctx, id, container.StopOptions{})
}

// Events returns a channel of Docker events.
func (c *Client) Events(ctx context.Context) (<-chan events.Message, <-chan error) {
	return c.cli.Events(ctx, events.ListOptions{})
}

// --- Helpers ---

func detectSocket() string {
	if host := os.Getenv("COMPOSER_DOCKER_HOST"); host != "" {
		return host
	}
	if host := os.Getenv("DOCKER_HOST"); host != "" {
		return host
	}
	if _, err := os.Stat("/var/run/docker.sock"); err == nil {
		return "unix:///var/run/docker.sock"
	}
	if _, err := os.Stat("/run/podman/podman.sock"); err == nil {
		return "unix:///run/podman/podman.sock"
	}
	if xdg := os.Getenv("XDG_RUNTIME_DIR"); xdg != "" {
		sock := filepath.Join(xdg, "podman", "podman.sock")
		if _, err := os.Stat(sock); err == nil {
			return "unix://" + sock
		}
	}
	return dockerclient.DefaultDockerHost
}

func toDomainContainer(c container.Summary) domcontainer.Container {
	dc := domcontainer.Container{
		ID:          c.ID[:12],
		Name:        strings.TrimPrefix(firstOrEmpty(c.Names), "/"),
		Image:       c.Image,
		Status:      mapStatus(c.State),
		StackName:   c.Labels["com.docker.compose.project"],
		ServiceName: c.Labels["com.docker.compose.service"],
	}

	for _, p := range c.Ports {
		dc.Ports = append(dc.Ports, domcontainer.PortBinding{
			HostIP:        p.IP,
			HostPort:      int(p.PublicPort),
			ContainerPort: int(p.PrivatePort),
			Protocol:      p.Type,
		})
	}

	// Health from status string
	if strings.Contains(c.Status, "(healthy)") {
		dc.Health = domcontainer.HealthHealthy
	} else if strings.Contains(c.Status, "(unhealthy)") {
		dc.Health = domcontainer.HealthUnhealthy
	} else if strings.Contains(c.Status, "(health:") {
		dc.Health = domcontainer.HealthStarting
	} else {
		dc.Health = domcontainer.HealthNone
	}

	return dc
}

func inspectToDomain(c container.InspectResponse) domcontainer.Container {
	dc := domcontainer.Container{
		ID:          c.ID[:12],
		Name:        strings.TrimPrefix(c.Name, "/"),
		Image:       c.Config.Image,
		Status:      mapStatus(c.State.Status),
		StackName:   c.Config.Labels["com.docker.compose.project"],
		ServiceName: c.Config.Labels["com.docker.compose.service"],
	}

	if c.State.Health != nil {
		switch c.State.Health.Status {
		case "healthy":
			dc.Health = domcontainer.HealthHealthy
		case "unhealthy":
			dc.Health = domcontainer.HealthUnhealthy
		case "starting":
			dc.Health = domcontainer.HealthStarting
		default:
			dc.Health = domcontainer.HealthNone
		}
	} else {
		dc.Health = domcontainer.HealthNone
	}

	return dc
}

func mapStatus(state string) domcontainer.ContainerStatus {
	switch state {
	case "running":
		return domcontainer.StatusRunning
	case "exited":
		return domcontainer.StatusExited
	case "created":
		return domcontainer.StatusCreated
	case "paused":
		return domcontainer.StatusPaused
	case "restarting":
		return domcontainer.StatusRestarting
	case "removing":
		return domcontainer.StatusRemoving
	case "dead":
		return domcontainer.StatusDead
	default:
		return domcontainer.ContainerStatus(state)
	}
}

func firstOrEmpty(s []string) string {
	if len(s) > 0 {
		return s[0]
	}
	return ""
}
