package docker

import (
	"context"
	"fmt"
	"io"

	"github.com/docker/docker/api/types/container"
)

// ExecSession represents an interactive exec session attached to a container.
type ExecSession struct {
	Conn   io.ReadWriteCloser // hijacked connection (stdin + stdout/stderr muxed)
	ExecID string
}

// ExecAttach creates an interactive exec session in a container and attaches to it.
// Returns a bidirectional connection: write = stdin, read = stdout+stderr.
func (c *Client) ExecAttach(ctx context.Context, containerID string, cmd []string, tty bool) (*ExecSession, error) {
	execCfg := container.ExecOptions{
		Cmd:          cmd,
		AttachStdin:  true,
		AttachStdout: true,
		AttachStderr: true,
		Tty:          tty,
	}

	exec, err := c.cli.ContainerExecCreate(ctx, containerID, execCfg)
	if err != nil {
		return nil, fmt.Errorf("creating exec: %w", err)
	}

	attach, err := c.cli.ContainerExecAttach(ctx, exec.ID, container.ExecAttachOptions{Tty: tty})
	if err != nil {
		return nil, fmt.Errorf("attaching to exec: %w", err)
	}

	return &ExecSession{
		Conn:   attach.Conn,
		ExecID: exec.ID,
	}, nil
}

// ExecResize resizes the TTY of an exec session.
func (c *Client) ExecResize(ctx context.Context, execID string, height, width uint) error {
	return c.cli.ContainerExecResize(ctx, execID, container.ResizeOptions{
		Height: height,
		Width:  width,
	})
}
