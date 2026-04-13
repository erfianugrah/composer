package ws

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"

	"github.com/erfianugrah/composer/internal/app"
	"github.com/erfianugrah/composer/internal/infra/docker"
)

// ComposeHandler handles WebSocket connections for streaming compose actions.
// Runs docker compose commands with a PTY so the client gets full ANSI output
// (progress bars, colors, cursor movement) rendered via xterm.js.
type ComposeHandler struct {
	stacks  *app.StackService
	compose *docker.Compose
}

func NewComposeHandler(stacks *app.StackService) *ComposeHandler {
	return &ComposeHandler{
		stacks:  stacks,
		compose: stacks.GetCompose(),
	}
}

// composeControlMsg is a JSON message from the client.
type composeControlMsg struct {
	Type string `json:"type"` // "resize"
	Cols uint16 `json:"cols"`
	Rows uint16 `json:"rows"`
}

// composeStatusMsg is a JSON text message sent to the client.
type composeStatusMsg struct {
	Type     string `json:"type"`                // "phase", "done", "error"
	Phase    string `json:"phase,omitempty"`     // "pull", "up", "down", etc.
	Message  string `json:"message,omitempty"`   // human-readable
	ExitCode int    `json:"exit_code,omitempty"` // only for "done"
}

// Allowed actions and their compose args.
var composeActions = map[string][][]string{
	"update": {
		{"pull"},
		{"up", "-d", "--remove-orphans", "--no-build"},
	},
	"pull": {
		{"pull"},
	},
	"up": {
		{"up", "-d", "--remove-orphans", "--no-build"},
	},
	"build": {
		{"up", "-d", "--build", "--remove-orphans"},
	},
	"down": {
		{"down", "--remove-orphans"},
	},
	"restart": {
		{"restart"},
	},
}

// ServeHTTP upgrades to WebSocket and streams compose action output via PTY.
// Query params: ?action=update&cols=80&rows=24
func (h *ComposeHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	stackName := r.PathValue("name")
	if stackName == "" {
		http.Error(w, "stack name required", http.StatusBadRequest)
		return
	}

	action := r.URL.Query().Get("action")
	phases, ok := composeActions[action]
	if !ok {
		http.Error(w, "invalid action; allowed: update, pull, up, build, down, restart", http.StatusBadRequest)
		return
	}

	cols := parseUint16(r.URL.Query().Get("cols"), 120)
	rows := parseUint16(r.URL.Query().Get("rows"), 30)

	// Accept WebSocket
	origins := []string{r.Host}
	if allowed := os.Getenv("COMPOSER_ALLOWED_ORIGINS"); allowed != "" {
		origins = strings.Split(allowed, ",")
	}
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		OriginPatterns: origins,
	})
	if err != nil {
		return
	}
	defer conn.CloseNow()
	conn.SetReadLimit(64 * 1024)

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	// Prepare: lock stack, SOPS decrypt
	ac, err := h.stacks.PrepareAction(ctx, stackName)
	if err != nil {
		sendStatus(ctx, conn, composeStatusMsg{Type: "error", Message: err.Error()})
		conn.Close(websocket.StatusInternalError, "prepare failed")
		return
	}
	defer ac.Cleanup()

	// Listen for resize messages from client
	var currentCols, currentRows uint16 = cols, rows
	var sizeMu sync.Mutex
	var activeProc *docker.PTYProcess
	var procMu sync.Mutex

	go func() {
		for {
			msgType, data, err := conn.Read(ctx)
			if err != nil {
				cancel()
				return
			}
			if msgType == websocket.MessageText {
				var msg composeControlMsg
				if json.Unmarshal(data, &msg) == nil && msg.Type == "resize" {
					sizeMu.Lock()
					currentCols = msg.Cols
					currentRows = msg.Rows
					sizeMu.Unlock()
					procMu.Lock()
					if activeProc != nil {
						activeProc.Resize(msg.Cols, msg.Rows)
					}
					procMu.Unlock()
				}
			}
		}
	}()

	// Keepalive pings
	go func() {
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
	}()

	// Use a background context with timeout for compose commands so a WS
	// disconnect doesn't SIGKILL docker compose mid-operation (same pattern
	// as the non-streaming handlers in stack.go).
	opCtx, opCancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer opCancel()

	// If WS context is cancelled, propagate to compose after a grace period
	go func() {
		<-ctx.Done()
		// Give compose 5s to finish naturally before killing
		time.Sleep(5 * time.Second)
		opCancel()
	}()

	// ANSI sequence: clear screen + home cursor. Sent between phases so
	// docker compose's progress renderer starts with a clean terminal.
	// Previous output remains in xterm.js scrollback (scrollback: 5000).
	clearScreen := []byte("\033[2J\033[H")

	// Run each phase
	var lastErr error
	for i, args := range phases {
		phase := args[0] // "pull", "up", "down", etc.

		// Clear terminal between phases (not before the first one)
		if i > 0 {
			writeCtx, writeCancel := context.WithTimeout(ctx, 5*time.Second)
			conn.Write(writeCtx, websocket.MessageBinary, clearScreen)
			writeCancel()
		}

		// Send phase marker
		sendStatus(ctx, conn, composeStatusMsg{Type: "phase", Phase: phase, Message: fmt.Sprintf("Running: docker compose %s", strings.Join(args, " "))})

		// Get current terminal size
		sizeMu.Lock()
		c, r := currentCols, currentRows
		sizeMu.Unlock()

		// Start compose with PTY (uses opCtx so disconnect doesn't instant-kill)
		proc, err := h.compose.RunPTY(opCtx, ac.StackPath, ac.ComposeFile, c, r, args...)
		if err != nil {
			lastErr = err
			sendStatus(ctx, conn, composeStatusMsg{Type: "error", Message: fmt.Sprintf("Failed to start: %v", err)})
			break
		}

		procMu.Lock()
		activeProc = proc
		procMu.Unlock()

		// Stream PTY output → WebSocket binary messages
		streamDone := make(chan struct{})
		go func() {
			defer close(streamDone)
			buf := make([]byte, 4096)
			for {
				n, readErr := proc.PTY.Read(buf)
				if n > 0 {
					writeCtx, writeCancel := context.WithTimeout(ctx, 5*time.Second)
					werr := conn.Write(writeCtx, websocket.MessageBinary, buf[:n])
					writeCancel()
					if werr != nil {
						return
					}
				}
				if readErr != nil {
					return // EOF or EIO (child exited)
				}
			}
		}()

		// Wait for command to finish
		cmdErr := proc.Wait()
		<-streamDone // ensure all output is flushed
		proc.Close()

		procMu.Lock()
		activeProc = nil
		procMu.Unlock()

		if cmdErr != nil {
			lastErr = cmdErr
			// For "update", pull failure is non-fatal -- continue to deploy
			if action == "update" && phase == "pull" {
				sendStatus(ctx, conn, composeStatusMsg{
					Type:    "phase",
					Phase:   "pull",
					Message: fmt.Sprintf("Pull finished with warnings: %v (continuing to deploy)", cmdErr),
				})
				lastErr = nil
				continue
			}
			sendStatus(ctx, conn, composeStatusMsg{
				Type:    "error",
				Message: fmt.Sprintf("Command failed: %v", cmdErr),
			})
			break
		}
	}

	// Publish domain event
	h.stacks.PublishActionEvent(stackName, action, lastErr)

	// Send completion
	exitCode := 0
	if lastErr != nil {
		exitCode = 1
	}
	sendStatus(ctx, conn, composeStatusMsg{Type: "done", ExitCode: exitCode})
	conn.Close(websocket.StatusNormalClosure, "done")
}

func sendStatus(ctx context.Context, conn *websocket.Conn, msg composeStatusMsg) {
	data, _ := json.Marshal(msg)
	writeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	conn.Write(writeCtx, websocket.MessageText, data)
}

func parseUint16(s string, fallback uint16) uint16 {
	if s == "" {
		return fallback
	}
	v, err := strconv.ParseUint(s, 10, 16)
	if err != nil {
		return fallback
	}
	return uint16(v)
}
