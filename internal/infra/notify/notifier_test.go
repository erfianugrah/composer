package notify_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	domevent "github.com/erfianugrah/composer/internal/domain/event"
	"github.com/erfianugrah/composer/internal/infra/eventbus"
	"github.com/erfianugrah/composer/internal/infra/notify"
)

func TestNotifier_WebhookDelivery(t *testing.T) {
	var received []map[string]any
	var mu sync.Mutex

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		mu.Lock()
		received = append(received, body)
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	logger, _ := zap.NewDevelopment()
	bus := eventbus.NewMemoryBus(16)
	defer bus.Close()

	notifier := notify.NewNotifier([]notify.Config{
		{Type: "webhook", URL: server.URL, Enabled: true},
	}, logger)
	notifier.Subscribe(bus)

	// Publish a stack deployed event
	bus.Publish(domevent.StackDeployed{Name: "web-app", Timestamp: time.Now()})

	// Wait for async delivery
	time.Sleep(200 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	require.Len(t, received, 1)
	assert.Equal(t, "stack.deployed", received[0]["event"])
}

func TestNotifier_SlackFormat(t *testing.T) {
	var received []map[string]any
	var mu sync.Mutex

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		mu.Lock()
		received = append(received, body)
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	logger, _ := zap.NewDevelopment()
	bus := eventbus.NewMemoryBus(16)
	defer bus.Close()

	notifier := notify.NewNotifier([]notify.Config{
		{Type: "slack", URL: server.URL, Enabled: true},
	}, logger)
	notifier.Subscribe(bus)

	bus.Publish(domevent.StackError{Name: "broken", Error: "container crashed", Timestamp: time.Now()})

	time.Sleep(200 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	require.Len(t, received, 1)
	attachments, ok := received[0]["attachments"].([]any)
	require.True(t, ok)
	require.Len(t, attachments, 1)
	attachment := attachments[0].(map[string]any)
	assert.Equal(t, "#f37e96", attachment["color"]) // red for errors
}

func TestNotifier_DisabledConfig(t *testing.T) {
	var called bool

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	logger, _ := zap.NewDevelopment()
	bus := eventbus.NewMemoryBus(16)
	defer bus.Close()

	notifier := notify.NewNotifier([]notify.Config{
		{Type: "webhook", URL: server.URL, Enabled: false}, // disabled
	}, logger)
	notifier.Subscribe(bus)

	bus.Publish(domevent.StackDeployed{Name: "test", Timestamp: time.Now()})

	time.Sleep(200 * time.Millisecond)
	assert.False(t, called, "disabled config should not send")
}

func TestNotifier_OnlySignificantEvents(t *testing.T) {
	var count int
	var mu sync.Mutex

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		count++
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	logger, _ := zap.NewDevelopment()
	bus := eventbus.NewMemoryBus(16)
	defer bus.Close()

	notifier := notify.NewNotifier([]notify.Config{
		{Type: "webhook", URL: server.URL, Enabled: true},
	}, logger)
	notifier.Subscribe(bus)

	// Significant events -- should trigger
	bus.Publish(domevent.StackDeployed{Name: "a", Timestamp: time.Now()})
	bus.Publish(domevent.StackStopped{Name: "b", Timestamp: time.Now()})
	bus.Publish(domevent.StackError{Name: "c", Error: "err", Timestamp: time.Now()})

	// Non-significant events -- should NOT trigger
	bus.Publish(domevent.StackUpdated{Name: "d", Timestamp: time.Now()})
	bus.Publish(domevent.StackCreated{Name: "e", Timestamp: time.Now()})
	bus.Publish(domevent.ContainerStateChanged{ContainerID: "x", Timestamp: time.Now()})

	time.Sleep(300 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, 3, count, "only significant events should trigger notifications")
}

func TestNotifier_MultipleTargets(t *testing.T) {
	var webhookCount, slackCount int
	var mu sync.Mutex

	webhookServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		webhookCount++
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer webhookServer.Close()

	slackServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		slackCount++
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer slackServer.Close()

	logger, _ := zap.NewDevelopment()
	bus := eventbus.NewMemoryBus(16)
	defer bus.Close()

	notifier := notify.NewNotifier([]notify.Config{
		{Type: "webhook", URL: webhookServer.URL, Enabled: true},
		{Type: "slack", URL: slackServer.URL, Enabled: true},
	}, logger)
	notifier.Subscribe(bus)

	bus.Publish(domevent.StackDeployed{Name: "test", Timestamp: time.Now()})

	time.Sleep(300 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, 1, webhookCount)
	assert.Equal(t, 1, slackCount)
}
