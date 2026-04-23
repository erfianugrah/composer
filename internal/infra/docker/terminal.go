package docker

import (
	"bytes"
	"context"
	"fmt"
	"io"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/pkg/stdcopy"
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

// ExecResult is the outcome of a non-interactive container exec.
type ExecResult struct {
	ExitCode int
	Stdout   string
	Stderr   string
	// Truncated is true when stdout or stderr hit ExecMaxOutput and
	// additional bytes were discarded.
	Truncated bool
}

// ExecMaxOutput caps the bytes captured per stream (stdout and stderr each).
// Containers that spew megabytes of output would otherwise balloon memory in
// the composer process — the hard cap trades completeness for safety.
const ExecMaxOutput = 1 << 20 // 1 MB per stream

// ExecRun runs a command inside an existing container non-interactively
// and returns captured stdout/stderr plus the exit code.
//
// Implementation notes:
//   - Uses Docker's multiplexed exec stream (Tty=false) and stdcopy to
//     demultiplex stdout/stderr into separate buffers.
//   - Captures at most ExecMaxOutput bytes per stream; sets Truncated=true
//     when either hits the cap. Additional bytes are drained to the bit
//     bucket so the container doesn't block on writes.
//   - On ctx.Done() we return promptly, but the exec process itself keeps
//     running inside the container — Docker's exec API has no cancel.
//     Use short timeouts for long-running commands.
//   - ExitCode is read via ContainerExecInspect after the stream drains.
//     If inspect reports Running=true (rare race), returns -1 as exit code
//     with no error; caller should treat that as "don't know".
func (c *Client) ExecRun(ctx context.Context, containerID string, cmd []string) (*ExecResult, error) {
	if len(cmd) == 0 {
		return nil, fmt.Errorf("exec: command is empty")
	}

	exec, err := c.cli.ContainerExecCreate(ctx, containerID, container.ExecOptions{
		Cmd:          cmd,
		AttachStdout: true,
		AttachStderr: true,
		Tty:          false, // multiplexed stream so stdcopy can demux
	})
	if err != nil {
		return nil, fmt.Errorf("creating exec: %w", err)
	}

	attach, err := c.cli.ContainerExecAttach(ctx, exec.ID, container.ExecAttachOptions{Tty: false})
	if err != nil {
		return nil, fmt.Errorf("attaching to exec: %w", err)
	}
	defer attach.Close()

	stdoutBuf := &CappedBuffer{Limit: ExecMaxOutput}
	stderrBuf := &CappedBuffer{Limit: ExecMaxOutput}

	done := make(chan error, 1)
	go func() {
		_, copyErr := stdcopy.StdCopy(stdoutBuf, stderrBuf, attach.Reader)
		done <- copyErr
	}()

	select {
	case copyErr := <-done:
		if copyErr != nil && copyErr != io.EOF {
			return nil, fmt.Errorf("reading exec output: %w", copyErr)
		}
	case <-ctx.Done():
		return nil, ctx.Err()
	}

	inspect, err := c.cli.ContainerExecInspect(ctx, exec.ID)
	if err != nil {
		return nil, fmt.Errorf("inspecting exec: %w", err)
	}

	exitCode := inspect.ExitCode
	if inspect.Running {
		exitCode = -1 // race: stream closed before daemon finalised status
	}

	return &ExecResult{
		ExitCode:  exitCode,
		Stdout:    stdoutBuf.String(),
		Stderr:    stderrBuf.String(),
		Truncated: stdoutBuf.Truncated || stderrBuf.Truncated,
	}, nil
}

// CappedBuffer is an io.Writer that accepts at most `Limit` bytes and
// discards the rest, setting Truncated=true. It pretends to accept the
// discarded bytes (returns len(p) with nil error) so upstream copy loops
// drain the source reader instead of blocking.
//
// Originally used for Docker exec stream capping; exported so other
// packages (notably internal/app pipeline executor's shell_command step)
// can share the same bounded-write pattern.
type CappedBuffer struct {
	buf       bytes.Buffer
	Limit     int
	Truncated bool
}

func (c *CappedBuffer) Write(p []byte) (int, error) {
	if c.buf.Len() >= c.Limit {
		c.Truncated = true
		return len(p), nil
	}
	remaining := c.Limit - c.buf.Len()
	if len(p) <= remaining {
		return c.buf.Write(p)
	}
	c.Truncated = true
	if _, err := c.buf.Write(p[:remaining]); err != nil {
		return 0, err
	}
	return len(p), nil
}

// String returns the captured bytes.
func (c *CappedBuffer) String() string { return c.buf.String() }

// Bytes returns a copy of the captured bytes.
func (c *CappedBuffer) Bytes() []byte { return c.buf.Bytes() }

// Len returns the number of captured bytes.
func (c *CappedBuffer) Len() int { return c.buf.Len() }
