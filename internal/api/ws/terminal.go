package ws

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"github.com/coder/websocket"

	"github.com/erfianugrah/composer/internal/infra/docker"
)

// TerminalHandler handles WebSocket connections for interactive container terminals.
type TerminalHandler struct {
	dockerClient *docker.Client
}

func NewTerminalHandler(dockerClient *docker.Client) *TerminalHandler {
	return &TerminalHandler{dockerClient: dockerClient}
}

// resizeMsg is sent from the client to resize the terminal.
type resizeMsg struct {
	Type string `json:"type"` // "resize"
	Cols uint   `json:"cols"`
	Rows uint   `json:"rows"`
}

// ServeHTTP upgrades to WebSocket and bridges stdin/stdout to a Docker exec session.
// Query params: ?shell=/bin/sh&cols=80&rows=24
func (h *TerminalHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	containerID := r.PathValue("id")
	if containerID == "" {
		http.Error(w, "container ID required", http.StatusBadRequest)
		return
	}

	shell := r.URL.Query().Get("shell")
	if shell == "" {
		shell = "/bin/sh"
	}

	// Accept WebSocket
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		// Allow any origin in dev; tighten in production
		InsecureSkipVerify: true,
	})
	if err != nil {
		return // Accept already wrote the error response
	}
	defer conn.CloseNow()

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	// Create exec session
	exec, err := h.dockerClient.ExecAttach(ctx, containerID, []string{shell}, true)
	if err != nil {
		conn.Close(websocket.StatusInternalError, "exec failed: "+err.Error())
		return
	}
	defer exec.Conn.Close()

	var wg sync.WaitGroup

	// Keepalive pings
	go h.Ping(ctx, conn)

	// Docker stdout -> WebSocket (binary messages)
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer cancel()

		buf := make([]byte, 4096)
		for {
			n, err := exec.Conn.Read(buf)
			if n > 0 {
				writeCtx, writeCancel := context.WithTimeout(ctx, 5*time.Second)
				werr := conn.Write(writeCtx, websocket.MessageBinary, buf[:n])
				writeCancel()
				if werr != nil {
					return
				}
			}
			if err != nil {
				return
			}
		}
	}()

	// WebSocket -> Docker stdin (handle both text control messages and binary data)
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer cancel()

		for {
			msgType, data, err := conn.Read(ctx)
			if err != nil {
				return
			}

			// Text messages are control commands (resize)
			if msgType == websocket.MessageText {
				var msg resizeMsg
				if json.Unmarshal(data, &msg) == nil && msg.Type == "resize" {
					h.dockerClient.ExecResize(ctx, exec.ExecID, msg.Rows, msg.Cols)
				}
				continue
			}

			// Binary messages are raw stdin
			if _, err := exec.Conn.Write(data); err != nil {
				return
			}
		}
	}()

	wg.Wait()
	conn.Close(websocket.StatusNormalClosure, "session ended")
}

// Ping sends periodic pings to keep the WebSocket alive.
func (h *TerminalHandler) Ping(ctx context.Context, conn *websocket.Conn) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			if err := conn.Ping(ctx); err != nil {
				return
			}
		case <-ctx.Done():
			return
		}
	}
}
