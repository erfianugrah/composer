package handler

import (
	"context"
	"encoding/json"
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

	// Container stats stream
	sse.Register(api, huma.Operation{
		OperationID: "streamContainerStats",
		Method:      http.MethodGet,
		Path:        "/api/v1/sse/containers/{id}/stats",
		Summary:     "Stream container CPU/memory/network stats",
		Tags:        []string{"sse"},
	}, map[string]any{
		"stats": event.ContainerStats{},
	}, h.StreamContainerStats)
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

// ContainerStatsInput defines the path params for stats streaming.
type ContainerStatsInput struct {
	ID string `path:"id" doc:"Container ID"`
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

// StreamContainerStats streams container resource stats via SSE (~1 event/sec).
func (h *SSEHandler) StreamContainerStats(ctx context.Context, input *ContainerStatsInput, send sse.Sender) {
	if err := authmw.CheckRole(ctx, auth.RoleViewer); err != nil {
		return
	}

	reader, err := h.dockerClient.ContainerStats(ctx, input.ID, true)
	if err != nil {
		return
	}
	defer reader.Close()

	decoder := json.NewDecoder(reader)
	for {
		var raw dockerStats
		if err := decoder.Decode(&raw); err != nil {
			return
		}

		stats := parseDockerStats(input.ID, &raw)
		if sendErr := send.Data(stats); sendErr != nil {
			return
		}

		select {
		case <-ctx.Done():
			return
		default:
		}
	}
}

// dockerStats is the JSON structure returned by the Docker stats API.
type dockerStats struct {
	CPUStats struct {
		CPUUsage struct {
			TotalUsage uint64 `json:"total_usage"`
		} `json:"cpu_usage"`
		SystemCPUUsage uint64 `json:"system_cpu_usage"`
		OnlineCPUs     uint64 `json:"online_cpus"`
	} `json:"cpu_stats"`
	PreCPUStats struct {
		CPUUsage struct {
			TotalUsage uint64 `json:"total_usage"`
		} `json:"cpu_usage"`
		SystemCPUUsage uint64 `json:"system_cpu_usage"`
	} `json:"precpu_stats"`
	MemoryStats struct {
		Usage uint64 `json:"usage"`
		Limit uint64 `json:"limit"`
	} `json:"memory_stats"`
	Networks map[string]struct {
		RxBytes uint64 `json:"rx_bytes"`
		TxBytes uint64 `json:"tx_bytes"`
	} `json:"networks"`
	BlkioStats struct {
		IOServiceBytesRecursive []struct {
			Op    string `json:"op"`
			Value uint64 `json:"value"`
		} `json:"io_service_bytes_recursive"`
	} `json:"blkio_stats"`
	PidsStats struct {
		Current uint64 `json:"current"`
	} `json:"pids_stats"`
}

func parseDockerStats(containerID string, raw *dockerStats) event.ContainerStats {
	// CPU percentage calculation
	cpuDelta := float64(raw.CPUStats.CPUUsage.TotalUsage - raw.PreCPUStats.CPUUsage.TotalUsage)
	systemDelta := float64(raw.CPUStats.SystemCPUUsage - raw.PreCPUStats.SystemCPUUsage)
	cpuPercent := 0.0
	if systemDelta > 0 && cpuDelta > 0 {
		cpuPercent = (cpuDelta / systemDelta) * float64(raw.CPUStats.OnlineCPUs) * 100.0
	}

	// Memory percentage
	memPercent := 0.0
	if raw.MemoryStats.Limit > 0 {
		memPercent = float64(raw.MemoryStats.Usage) / float64(raw.MemoryStats.Limit) * 100.0
	}

	// Network totals
	var netRx, netTx uint64
	for _, iface := range raw.Networks {
		netRx += iface.RxBytes
		netTx += iface.TxBytes
	}

	// Block I/O
	var blockRead, blockWrite uint64
	for _, entry := range raw.BlkioStats.IOServiceBytesRecursive {
		switch entry.Op {
		case "read", "Read":
			blockRead += entry.Value
		case "write", "Write":
			blockWrite += entry.Value
		}
	}

	return event.ContainerStats{
		ContainerID: containerID,
		CPUPercent:  cpuPercent,
		MemUsage:    raw.MemoryStats.Usage,
		MemLimit:    raw.MemoryStats.Limit,
		MemPercent:  memPercent,
		NetRx:       netRx,
		NetTx:       netTx,
		BlockRead:   blockRead,
		BlockWrite:  blockWrite,
		PIDs:        raw.PidsStats.Current,
		Timestamp:   time.Now(),
	}
}
