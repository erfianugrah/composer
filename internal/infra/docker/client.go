package docker

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/events"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/system"
	"github.com/docker/docker/api/types/volume"
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

	// Detect runtime (P19: timeout so startup doesn't block indefinitely)
	initCtx, initCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer initCancel()
	info, err := cli.Info(initCtx)
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

// EventsSince returns Docker events since the given Unix timestamp, plus future events.
func (c *Client) EventsSince(ctx context.Context, sinceUnix int64) (<-chan events.Message, <-chan error) {
	return c.cli.Events(ctx, events.ListOptions{
		Since: fmt.Sprintf("%d", sinceUnix),
	})
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

// exitCodeRe matches "Exited (0)" or "Exited (137)" in Docker status strings.
var exitCodeRe = regexp.MustCompile(`Exited \((\d+)\)`)

func toDomainContainer(c container.Summary) domcontainer.Container {
	dc := domcontainer.Container{
		ID:          c.ID[:12],
		Name:        strings.TrimPrefix(firstOrEmpty(c.Names), "/"),
		Image:       c.Image,
		Status:      mapStatus(c.State),
		StackName:   c.Labels["com.docker.compose.project"],
		ServiceName: c.Labels["com.docker.compose.service"],
	}

	// Extract exit code from status string (e.g., "Exited (0) 5 minutes ago")
	if matches := exitCodeRe.FindStringSubmatch(c.Status); len(matches) == 2 {
		dc.ExitCode, _ = strconv.Atoi(matches[1])
	}

	// Restart policy from compose label (set by docker compose)
	// Values: "no", "always", "on-failure", "unless-stopped"
	dc.RestartPolicy = c.Labels["com.docker.compose.restart"]

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
		ID:            c.ID[:12],
		Name:          strings.TrimPrefix(c.Name, "/"),
		Image:         c.Config.Image,
		Status:        mapStatus(c.State.Status),
		ExitCode:      c.State.ExitCode,
		RestartPolicy: string(c.HostConfig.RestartPolicy.Name),
		StackName:     c.Config.Labels["com.docker.compose.project"],
		ServiceName:   c.Config.Labels["com.docker.compose.service"],
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

// --- Network Management ---

type NetworkInfo struct {
	ID         string            `json:"id"`
	Name       string            `json:"name"`
	Driver     string            `json:"driver"`
	Scope      string            `json:"scope"`
	Internal   bool              `json:"internal"`
	Containers int               `json:"containers"`
	Labels     map[string]string `json:"labels,omitempty"`
}

func (c *Client) ListNetworks(ctx context.Context) ([]NetworkInfo, error) {
	nets, err := c.cli.NetworkList(ctx, network.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("listing networks: %w", err)
	}
	result := make([]NetworkInfo, 0, len(nets))
	for _, n := range nets {
		result = append(result, NetworkInfo{
			ID: n.ID[:12], Name: n.Name, Driver: n.Driver,
			Scope: n.Scope, Internal: n.Internal,
			Containers: len(n.Containers), Labels: n.Labels,
		})
	}
	return result, nil
}

func (c *Client) CreateNetwork(ctx context.Context, name, driver string) error {
	_, err := c.cli.NetworkCreate(ctx, name, network.CreateOptions{Driver: driver})
	return err
}

func (c *Client) RemoveNetwork(ctx context.Context, id string) error {
	return c.cli.NetworkRemove(ctx, id)
}

// InspectNetwork returns the full raw inspect data for a network.
func (c *Client) InspectNetwork(ctx context.Context, id string) (network.Inspect, error) {
	return c.cli.NetworkInspect(ctx, id, network.InspectOptions{Verbose: true})
}

// --- Volume Management ---

type VolumeInfo struct {
	Name       string            `json:"name"`
	Driver     string            `json:"driver"`
	Mountpoint string            `json:"mountpoint"`
	Labels     map[string]string `json:"labels,omitempty"`
	CreatedAt  string            `json:"created_at"`
}

func (c *Client) ListVolumes(ctx context.Context) ([]VolumeInfo, error) {
	resp, err := c.cli.VolumeList(ctx, volume.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("listing volumes: %w", err)
	}
	result := make([]VolumeInfo, 0, len(resp.Volumes))
	for _, v := range resp.Volumes {
		result = append(result, VolumeInfo{
			Name: v.Name, Driver: v.Driver, Mountpoint: v.Mountpoint,
			Labels: v.Labels, CreatedAt: v.CreatedAt,
		})
	}
	return result, nil
}

func (c *Client) CreateVolume(ctx context.Context, name, driver string) error {
	_, err := c.cli.VolumeCreate(ctx, volume.CreateOptions{Name: name, Driver: driver})
	return err
}

func (c *Client) RemoveVolume(ctx context.Context, name string) error {
	return c.cli.VolumeRemove(ctx, name, false)
}

// InspectVolume returns the full raw inspect data for a volume.
func (c *Client) InspectVolume(ctx context.Context, name string) (volume.Volume, error) {
	return c.cli.VolumeInspect(ctx, name)
}

func (c *Client) PruneVolumes(ctx context.Context) (uint64, error) {
	report, err := c.cli.VolumesPrune(ctx, filters.Args{})
	if err != nil {
		return 0, err
	}
	return report.SpaceReclaimed, nil
}

// --- Image Management ---

type ImageInfo struct {
	ID      string   `json:"id"`
	Tags    []string `json:"tags"`
	Size    int64    `json:"size"`
	Created int64    `json:"created"`
}

func (c *Client) ListImages(ctx context.Context) ([]ImageInfo, error) {
	imgs, err := c.cli.ImageList(ctx, image.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("listing images: %w", err)
	}
	result := make([]ImageInfo, 0, len(imgs))
	for _, img := range imgs {
		id := img.ID
		if strings.HasPrefix(id, "sha256:") {
			id = id[7:19] // short hash
		}
		result = append(result, ImageInfo{
			ID: id, Tags: img.RepoTags, Size: img.Size, Created: img.Created,
		})
	}
	return result, nil
}

func (c *Client) PullImage(ctx context.Context, ref string) error {
	reader, err := c.cli.ImagePull(ctx, ref, image.PullOptions{})
	if err != nil {
		return fmt.Errorf("pulling image: %w", err)
	}
	defer reader.Close()
	// Drain the reader to complete the pull
	io.Copy(io.Discard, reader)
	return nil
}

func (c *Client) RemoveImage(ctx context.Context, id string) error {
	_, err := c.cli.ImageRemove(ctx, id, image.RemoveOptions{Force: true, PruneChildren: true})
	return err
}

// PruneImages removes unused images. If all is true, removes all unused images
// (not just dangling/untagged). This is the critical distinction — dangling-only
// misses old tagged images from previous pulls.
func (c *Client) PruneImages(ctx context.Context, all bool) (uint64, error) {
	f := filters.NewArgs()
	if all {
		f.Add("dangling", "false")
	}
	report, err := c.cli.ImagesPrune(ctx, f)
	if err != nil {
		return 0, err
	}
	return report.SpaceReclaimed, nil
}

func (c *Client) PruneContainers(ctx context.Context) (uint64, error) {
	report, err := c.cli.ContainersPrune(ctx, filters.Args{})
	if err != nil {
		return 0, err
	}
	return report.SpaceReclaimed, nil
}

func (c *Client) PruneNetworks(ctx context.Context) ([]string, error) {
	report, err := c.cli.NetworksPrune(ctx, filters.Args{})
	if err != nil {
		return nil, err
	}
	return report.NetworksDeleted, nil
}

func (c *Client) PruneBuildCache(ctx context.Context) (uint64, error) {
	report, err := c.cli.BuildCachePrune(ctx, types.BuildCachePruneOptions{All: true})
	if err != nil {
		return 0, err
	}
	return report.SpaceReclaimed, nil
}
