//go:build integration

package ws_test

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/erfianugrah/composer/internal/api/ws"
	"github.com/erfianugrah/composer/internal/infra/docker"
)

func TestTerminal_ExecEcho(t *testing.T) {
	ctx := context.Background()

	// Start a real container to exec into
	ctr, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:      "alpine:3.21",
			Cmd:        []string{"sleep", "60"},
			WaitingFor: wait.ForLog("").WithStartupTimeout(10 * time.Second),
		},
		Started: true,
	})
	require.NoError(t, err)
	t.Cleanup(func() { ctr.Terminate(context.Background()) })

	containerID := ctr.GetContainerID()
	require.NotEmpty(t, containerID)

	// Create docker client + terminal handler
	dockerClient, err := docker.NewClient("")
	require.NoError(t, err)
	t.Cleanup(func() { dockerClient.Close() })

	termHandler := ws.NewTerminalHandler(dockerClient)

	// Set up chi router with the terminal endpoint
	router := chi.NewMux()
	router.Get("/api/v1/ws/terminal/{id}", termHandler.ServeHTTP)

	// Start test HTTP server
	server := httptest.NewServer(router)
	t.Cleanup(func() { server.Close() })

	// Connect WebSocket
	wsURL := strings.Replace(server.URL, "http://", "ws://", 1) +
		"/api/v1/ws/terminal/" + containerID + "?shell=/bin/sh"

	wsCtx, wsCancel := context.WithTimeout(ctx, 10*time.Second)
	defer wsCancel()

	conn, _, err := websocket.Dial(wsCtx, wsURL, nil)
	require.NoError(t, err)
	defer conn.CloseNow()

	// Small delay for exec to initialize
	time.Sleep(200 * time.Millisecond)

	// Send a command: echo hello
	err = conn.Write(wsCtx, websocket.MessageBinary, []byte("echo hello-from-terminal\n"))
	require.NoError(t, err)

	// Read output until we find our echo
	found := false
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		readCtx, readCancel := context.WithTimeout(ctx, 2*time.Second)
		_, data, err := conn.Read(readCtx)
		readCancel()
		if err != nil {
			break
		}
		if strings.Contains(string(data), "hello-from-terminal") {
			found = true
			break
		}
	}

	assert.True(t, found, "expected to receive 'hello-from-terminal' in terminal output")

	// Test resize (should not error)
	resizeMsg := `{"type":"resize","cols":120,"rows":40}`
	err = conn.Write(wsCtx, websocket.MessageText, []byte(resizeMsg))
	assert.NoError(t, err)

	conn.Close(websocket.StatusNormalClosure, "done")
}
