package handler

import (
	"context"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/sse"

	authmw "github.com/erfianugrah/composer/internal/api/middleware"
	"github.com/erfianugrah/composer/internal/domain/auth"
	"github.com/erfianugrah/composer/internal/domain/event"
	"github.com/erfianugrah/composer/internal/infra/docker"
)

// SSEHandler registers Server-Sent Events streaming endpoints.
type SSEHandler struct {
	bus          event.Bus
	dockerClient *docker.Client
}

func NewSSEHandler(bus event.Bus, dockerClient *docker.Client) *SSEHandler {
	return &SSEHandler{bus: bus, dockerClient: dockerClient}
}

func (h *SSEHandler) Register(api huma.API) {
	// Global Docker events stream
	sse.Register(api, huma.Operation{
		OperationID: "streamEvents",
		Method:      http.MethodGet,
		Path:        "/api/v1/sse/events",
		Summary:     "Stream Docker events in real-time",
		Tags:        []string{"sse"},
	}, map[string]any{
		"stack.deployed":   event.StackDeployed{},
		"stack.stopped":    event.StackStopped{},
		"stack.updated":    event.StackUpdated{},
		"stack.deleted":    event.StackDeleted{},
		"stack.error":      event.StackError{},
		"container.state":  event.ContainerStateChanged{},
		"container.health": event.ContainerHealthChanged{},
	}, h.StreamEvents)

	// Container log stream
	sse.Register(api, huma.Operation{
		OperationID: "streamContainerLogs",
		Method:      http.MethodGet,
		Path:        "/api/v1/sse/containers/{id}/logs",
		Summary:     "Stream container logs in real-time",
		Tags:        []string{"sse"},
	}, map[string]any{
		"log": event.LogEntry{},
	}, h.StreamContainerLogs)
}

// StreamEvents streams all domain events to the client via SSE. Requires viewer+ role.
func (h *SSEHandler) StreamEvents(ctx context.Context, input *struct{}, send sse.Sender) {
	if err := authmw.CheckRole(ctx, auth.RoleViewer); err != nil {
		return
	}
	eventCh := make(chan event.Event, 64)

	unsub := h.bus.Subscribe(func(evt event.Event) bool {
		select {
		case eventCh <- evt:
		default:
			// Drop if client is slow
		}
		return true
	})
	defer unsub()

	for {
		select {
		case evt := <-eventCh:
			if err := send.Data(evt); err != nil {
				return
			}
		case <-ctx.Done():
			return
		}
	}
}

// ContainerLogInput defines the path/query params for log streaming.
type ContainerLogInput struct {
	ID    string `path:"id" doc:"Container ID"`
	Tail  string `query:"tail" default:"100" doc:"Number of lines from the end"`
	Since string `query:"since" default:"" doc:"Show logs since timestamp or relative (e.g. 5m)"`
}

// StreamContainerLogs streams container logs via SSE. Requires viewer+ role.
func (h *SSEHandler) StreamContainerLogs(ctx context.Context, input *ContainerLogInput, send sse.Sender) {
	if err := authmw.CheckRole(ctx, auth.RoleViewer); err != nil {
		return
	}
	reader, err := h.dockerClient.ContainerLogs(ctx, input.ID, true, input.Tail, input.Since)
	if err != nil {
		send.Data(event.LogEntry{
			ContainerID: input.ID,
			Stream:      "stderr",
			Message:     "error: " + err.Error(),
			Timestamp:   time.Now(),
		})
		return
	}
	defer reader.Close()

	buf := make([]byte, 8192)
	for {
		n, err := reader.Read(buf)
		if n > 0 {
			// Docker multiplexed stream: first 8 bytes are header
			// For simplicity, strip header and send raw text
			data := buf[:n]
			stream := "stdout"

			// Docker stream header: byte 0 = stream type (1=stdout, 2=stderr)
			if len(data) > 8 {
				if data[0] == 2 {
					stream = "stderr"
				}
				data = data[8:]
			}

			lines := strings.Split(strings.TrimSpace(string(data)), "\n")
			for _, line := range lines {
				if line == "" {
					continue
				}
				if sendErr := send.Data(event.LogEntry{
					ContainerID: input.ID,
					Stream:      stream,
					Message:     line,
					Timestamp:   time.Now(),
				}); sendErr != nil {
					return
				}
			}
		}
		if err == io.EOF || err == context.Canceled {
			return
		}
		if err != nil {
			return
		}
	}
}
