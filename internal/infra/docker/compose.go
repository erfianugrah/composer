package docker

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// ComposeResult holds the output of a docker compose command.
type ComposeResult struct {
	Stdout   string
	Stderr   string
	ExitCode int
}

// Compose wraps the `docker compose` CLI for stack operations.
type Compose struct {
	dockerHost string // passed as DOCKER_HOST env var
}

// NewCompose creates a new Compose CLI wrapper.
func NewCompose(dockerHost string) *Compose {
	return &Compose{dockerHost: dockerHost}
}

// Up runs `docker compose up -d` in the given stack directory.
func (c *Compose) Up(ctx context.Context, stackDir string, services ...string) (*ComposeResult, error) {
	args := []string{"up", "-d", "--remove-orphans"}
	args = append(args, services...)
	return c.run(ctx, stackDir, args...)
}

// Down runs `docker compose down` in the given stack directory.
func (c *Compose) Down(ctx context.Context, stackDir string, removeVolumes bool) (*ComposeResult, error) {
	args := []string{"down", "--remove-orphans"}
	if removeVolumes {
		args = append(args, "--volumes")
	}
	return c.run(ctx, stackDir, args...)
}

// Pull runs `docker compose pull` in the given stack directory.
func (c *Compose) Pull(ctx context.Context, stackDir string, services ...string) (*ComposeResult, error) {
	args := []string{"pull"}
	args = append(args, services...)
	return c.run(ctx, stackDir, args...)
}

// Restart runs `docker compose restart` in the given stack directory.
func (c *Compose) Restart(ctx context.Context, stackDir string, services ...string) (*ComposeResult, error) {
	args := []string{"restart"}
	args = append(args, services...)
	return c.run(ctx, stackDir, args...)
}

// Ps runs `docker compose ps --format json` and returns the raw output.
func (c *Compose) Ps(ctx context.Context, stackDir string) (*ComposeResult, error) {
	return c.run(ctx, stackDir, "ps", "--format", "json")
}

// Validate runs `docker compose config --quiet` to validate a compose file.
func (c *Compose) Validate(ctx context.Context, stackDir string) (*ComposeResult, error) {
	return c.run(ctx, stackDir, "config", "--quiet")
}

// Config runs `docker compose config` and returns the normalized compose YAML.
func (c *Compose) Config(ctx context.Context, stackDir string) (*ComposeResult, error) {
	return c.run(ctx, stackDir, "config")
}

// Exec runs an arbitrary docker compose subcommand in the given stack directory.
// This is for the stack console -- operators can run any compose command.
// Only compose subcommands are allowed (not arbitrary shell commands).
func (c *Compose) Exec(ctx context.Context, stackDir string, composeArgs []string) (*ComposeResult, error) {
	return c.run(ctx, stackDir, composeArgs...)
}

// run executes a docker compose command in the given working directory.
func (c *Compose) run(ctx context.Context, workDir string, args ...string) (*ComposeResult, error) {
	fullArgs := append([]string{"compose"}, args...)
	cmd := exec.CommandContext(ctx, "docker", fullArgs...)
	cmd.Dir = workDir

	// Set DOCKER_HOST if using a non-default socket
	if c.dockerHost != "" {
		cmd.Env = append(cmd.Environ(), "DOCKER_HOST="+c.dockerHost)
	}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()

	result := &ComposeResult{
		Stdout: strings.TrimSpace(stdout.String()),
		Stderr: strings.TrimSpace(stderr.String()),
	}

	if exitErr, ok := err.(*exec.ExitError); ok {
		result.ExitCode = exitErr.ExitCode()
		return result, fmt.Errorf("docker compose %s failed (exit %d): %s",
			strings.Join(args, " "), result.ExitCode, result.Stderr)
	}
	if err != nil {
		return result, fmt.Errorf("running docker compose: %w", err)
	}

	return result, nil
}
