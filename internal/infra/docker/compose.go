package docker

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"go.uber.org/zap"
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
	log        *zap.Logger
}

// NewCompose creates a new Compose CLI wrapper.
func NewCompose(dockerHost string, log *zap.Logger) *Compose {
	if log == nil {
		log = zap.NewNop()
	}
	return &Compose{dockerHost: dockerHost, log: log}
}

// Up runs `docker compose up -d --no-build` in the given stack directory.
// The --no-build flag prevents auto-building when a Dockerfile is present.
// Use BuildAndUp for explicit builds.
// composeFile is optional -- if set, passes `-f <file>` to use a specific compose file
// instead of letting docker compose auto-discover (which may merge multiple files).
func (c *Compose) Up(ctx context.Context, stackDir string, composeFile string, services ...string) (*ComposeResult, error) {
	args := []string{"up", "-d", "--no-build", "--remove-orphans"}
	args = append(args, services...)
	return c.run(ctx, stackDir, composeFile, args...)
}

// BuildAndUp runs `docker compose up -d --build` -- explicitly builds images
// from Dockerfiles before starting containers.
func (c *Compose) BuildAndUp(ctx context.Context, stackDir string, composeFile string) (*ComposeResult, error) {
	return c.run(ctx, stackDir, composeFile, "up", "-d", "--build", "--remove-orphans")
}

// Build runs `docker compose build` without starting containers.
func (c *Compose) Build(ctx context.Context, stackDir string, composeFile string) (*ComposeResult, error) {
	return c.run(ctx, stackDir, composeFile, "build")
}

// Down runs `docker compose down` in the given stack directory.
func (c *Compose) Down(ctx context.Context, stackDir string, composeFile string, removeVolumes bool) (*ComposeResult, error) {
	args := []string{"down", "--remove-orphans"}
	if removeVolumes {
		args = append(args, "--volumes")
	}
	return c.run(ctx, stackDir, composeFile, args...)
}

// Pull runs `docker compose pull` in the given stack directory.
func (c *Compose) Pull(ctx context.Context, stackDir string, composeFile string, services ...string) (*ComposeResult, error) {
	args := []string{"pull"}
	args = append(args, services...)
	return c.run(ctx, stackDir, composeFile, args...)
}

// Restart runs `docker compose restart` in the given stack directory.
func (c *Compose) Restart(ctx context.Context, stackDir string, composeFile string, services ...string) (*ComposeResult, error) {
	args := []string{"restart"}
	args = append(args, services...)
	return c.run(ctx, stackDir, composeFile, args...)
}

// Ps runs `docker compose ps --format json` and returns the raw output.
func (c *Compose) Ps(ctx context.Context, stackDir string) (*ComposeResult, error) {
	return c.run(ctx, stackDir, "", "ps", "--format", "json")
}

// Validate runs `docker compose config --quiet` to validate a compose file.
func (c *Compose) Validate(ctx context.Context, stackDir string) (*ComposeResult, error) {
	return c.run(ctx, stackDir, "", "config", "--quiet")
}

// Config runs `docker compose config` and returns the normalized compose YAML.
func (c *Compose) Config(ctx context.Context, stackDir string) (*ComposeResult, error) {
	return c.run(ctx, stackDir, "", "config")
}

// Exec runs an arbitrary docker compose subcommand in the given stack directory.
func (c *Compose) Exec(ctx context.Context, stackDir string, composeArgs []string) (*ComposeResult, error) {
	return c.run(ctx, stackDir, "", composeArgs...)
}

// RunDocker executes a raw `docker` command (not compose-scoped).
func (c *Compose) RunDocker(ctx context.Context, args []string) (*ComposeResult, error) {
	c.log.Info("docker exec",
		zap.String("command", "docker "+strings.Join(args, " ")),
	)

	cmd := exec.CommandContext(ctx, "docker", args...)
	if c.dockerHost != "" {
		cmd.Env = append(cmd.Environ(), "DOCKER_HOST="+c.dockerHost)
	}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	start := time.Now()
	err := cmd.Run()
	duration := time.Since(start)

	result := &ComposeResult{
		Stdout: strings.TrimSpace(stdout.String()),
		Stderr: strings.TrimSpace(stderr.String()),
	}

	if exitErr, ok := err.(*exec.ExitError); ok {
		result.ExitCode = exitErr.ExitCode()
		c.log.Warn("docker exec non-zero exit",
			zap.String("command", "docker "+strings.Join(args, " ")),
			zap.Int("exit_code", result.ExitCode),
			zap.Duration("duration", duration),
			zap.String("stderr", truncate(result.Stderr, 500)),
		)
		return result, nil
	}
	if err != nil {
		c.log.Error("docker exec failed",
			zap.String("command", "docker "+strings.Join(args, " ")),
			zap.Duration("duration", duration),
			zap.Error(err),
		)
		return result, fmt.Errorf("running docker: %w", err)
	}

	c.log.Debug("docker exec completed",
		zap.String("command", "docker "+strings.Join(args, " ")),
		zap.Duration("duration", duration),
	)
	return result, nil
}

// run executes a docker compose command in the given working directory.
// If composeFile is non-empty, passes `-f <file>` so docker compose uses
// exactly that file instead of auto-discovering docker-compose.yml.
func (c *Compose) run(ctx context.Context, workDir string, composeFile string, args ...string) (*ComposeResult, error) {
	cmdStr := "docker compose " + strings.Join(args, " ")
	if composeFile != "" {
		cmdStr = "docker compose -f " + composeFile + " " + strings.Join(args, " ")
	}
	c.log.Info("compose exec",
		zap.String("command", cmdStr),
		zap.String("dir", workDir),
	)

	var fullArgs []string
	if composeFile != "" {
		fullArgs = append([]string{"compose", "-f", composeFile}, args...)
	} else {
		fullArgs = append([]string{"compose"}, args...)
	}
	cmd := exec.CommandContext(ctx, "docker", fullArgs...)
	cmd.Dir = workDir

	if c.dockerHost != "" {
		cmd.Env = append(cmd.Environ(), "DOCKER_HOST="+c.dockerHost)
	}

	// P16: limit to 1MB to prevent unbounded memory from verbose compose output
	stdout := &limitedBuffer{max: 1 << 20}
	stderr := &limitedBuffer{max: 1 << 20}
	cmd.Stdout = stdout
	cmd.Stderr = stderr

	start := time.Now()
	err := cmd.Run()
	duration := time.Since(start)

	result := &ComposeResult{
		Stdout: strings.TrimSpace(stdout.String()),
		Stderr: strings.TrimSpace(stderr.String()),
	}

	if exitErr, ok := err.(*exec.ExitError); ok {
		result.ExitCode = exitErr.ExitCode()
		c.log.Error("compose exec failed",
			zap.String("command", cmdStr),
			zap.String("dir", workDir),
			zap.Int("exit_code", result.ExitCode),
			zap.Duration("duration", duration),
			zap.String("stderr", truncate(result.Stderr, 500)),
		)
		return result, fmt.Errorf("docker compose %s failed (exit %d): %s",
			strings.Join(args, " "), result.ExitCode, result.Stderr)
	}
	if err != nil {
		c.log.Error("compose exec error",
			zap.String("command", cmdStr),
			zap.String("dir", workDir),
			zap.Duration("duration", duration),
			zap.Error(err),
		)
		return result, fmt.Errorf("running docker compose: %w", err)
	}

	c.log.Info("compose exec completed",
		zap.String("command", cmdStr),
		zap.String("dir", workDir),
		zap.Duration("duration", duration),
	)
	return result, nil
}

// limitedBuffer is a bytes.Buffer that stops accepting writes after max bytes.
type limitedBuffer struct {
	buf bytes.Buffer
	max int
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	remaining := b.max - b.buf.Len()
	if remaining <= 0 {
		return len(p), nil // discard silently
	}
	if len(p) > remaining {
		p = p[:remaining]
	}
	return b.buf.Write(p)
}

func (b *limitedBuffer) String() string { return b.buf.String() }

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}
