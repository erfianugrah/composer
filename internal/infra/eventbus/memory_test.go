package eventbus_test

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/erfianugrah/composer/internal/domain/event"
	"github.com/erfianugrah/composer/internal/infra/eventbus"
)

func TestMemoryBus_PublishSubscribe(t *testing.T) {
	bus := eventbus.NewMemoryBus(16)
	defer bus.Close()

	var received []event.Event
	var mu sync.Mutex
	done := make(chan struct{})

	bus.Subscribe(func(evt event.Event) bool {
		mu.Lock()
		received = append(received, evt)
		mu.Unlock()
		if len(received) == 3 {
			close(done)
		}
		return true
	})

	bus.Publish(event.StackDeployed{Name: "web", Timestamp: time.Now()})
	bus.Publish(event.StackStopped{Name: "web", Timestamp: time.Now()})
	bus.Publish(event.ContainerStateChanged{
		ContainerID: "abc123",
		StackName:   "web",
		OldStatus:   "running",
		NewStatus:   "exited",
		Timestamp:   time.Now(),
	})

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for events")
	}

	mu.Lock()
	defer mu.Unlock()
	require.Len(t, received, 3)
	assert.Equal(t, "stack.deployed", received[0].EventType())
	assert.Equal(t, "stack.stopped", received[1].EventType())
	assert.Equal(t, "container.state", received[2].EventType())
}

func TestMemoryBus_MultipleSubscribers(t *testing.T) {
	bus := eventbus.NewMemoryBus(16)
	defer bus.Close()

	var wg sync.WaitGroup
	wg.Add(4) // 2 subscribers * 2 events each

	bus.Subscribe(func(evt event.Event) bool {
		wg.Done()
		return true
	})
	bus.Subscribe(func(evt event.Event) bool {
		wg.Done()
		return true
	})

	bus.Publish(event.StackDeployed{Name: "a", Timestamp: time.Now()})
	bus.Publish(event.StackDeployed{Name: "b", Timestamp: time.Now()})

	// Wait for all 4 deliveries (2 events * 2 subscribers)
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()

	select {
	case <-done:
		// success
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for all subscribers to receive events")
	}
}

func TestMemoryBus_Unsubscribe(t *testing.T) {
	bus := eventbus.NewMemoryBus(16)
	defer bus.Close()

	assert.Equal(t, 0, bus.SubscriberCount())

	unsub := bus.Subscribe(func(evt event.Event) bool { return true })
	// Give goroutine time to start
	time.Sleep(10 * time.Millisecond)
	assert.Equal(t, 1, bus.SubscriberCount())

	unsub()
	time.Sleep(10 * time.Millisecond)
	assert.Equal(t, 0, bus.SubscriberCount())
}

func TestMemoryBus_SubscriberReturnsFalse(t *testing.T) {
	bus := eventbus.NewMemoryBus(16)
	defer bus.Close()

	callCount := 0
	bus.Subscribe(func(evt event.Event) bool {
		callCount++
		return false // unsubscribe after first event
	})

	bus.Publish(event.StackDeployed{Name: "a", Timestamp: time.Now()})
	time.Sleep(50 * time.Millisecond)
	bus.Publish(event.StackDeployed{Name: "b", Timestamp: time.Now()})
	time.Sleep(50 * time.Millisecond)

	assert.Equal(t, 1, callCount, "subscriber should only receive 1 event")
	assert.Equal(t, 0, bus.SubscriberCount())
}

func TestMemoryBus_Close(t *testing.T) {
	bus := eventbus.NewMemoryBus(16)

	bus.Subscribe(func(evt event.Event) bool { return true })
	bus.Subscribe(func(evt event.Event) bool { return true })
	time.Sleep(10 * time.Millisecond)
	assert.Equal(t, 2, bus.SubscriberCount())

	bus.Close()
	assert.Equal(t, 0, bus.SubscriberCount())

	// Publish after close should not panic
	bus.Publish(event.StackDeployed{Name: "x", Timestamp: time.Now()})
}
