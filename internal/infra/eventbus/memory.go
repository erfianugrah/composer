package eventbus

import (
	"sync"

	"github.com/erfianugrah/composer/internal/domain/event"
)

// MemoryBus is an in-process event bus using channels.
// Each subscriber gets a buffered channel; slow consumers are dropped.
type MemoryBus struct {
	mu      sync.RWMutex
	subs    map[uint64]chan event.Event
	nextID  uint64
	closed  bool
	bufSize int
}

// NewMemoryBus creates a new in-process event bus.
// bufSize is the channel buffer per subscriber (e.g. 256).
func NewMemoryBus(bufSize int) *MemoryBus {
	if bufSize < 1 {
		bufSize = 256
	}
	return &MemoryBus{
		subs:    make(map[uint64]chan event.Event),
		bufSize: bufSize,
	}
}

func (b *MemoryBus) Publish(evt event.Event) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	if b.closed {
		return
	}

	for id, ch := range b.subs {
		select {
		case ch <- evt:
		default:
			// Subscriber is slow -- drop event to avoid blocking
			_ = id
		}
	}
}

func (b *MemoryBus) Subscribe(sub event.Subscriber) func() {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.closed {
		return func() {}
	}

	id := b.nextID
	b.nextID++
	ch := make(chan event.Event, b.bufSize)
	b.subs[id] = ch

	// Goroutine to feed subscriber
	go func() {
		for evt := range ch {
			if !sub(evt) {
				b.unsubscribe(id)
				return
			}
		}
	}()

	return func() { b.unsubscribe(id) }
}

func (b *MemoryBus) Close() {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.closed = true
	for id, ch := range b.subs {
		close(ch)
		delete(b.subs, id)
	}
}

func (b *MemoryBus) unsubscribe(id uint64) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if ch, ok := b.subs[id]; ok {
		close(ch)
		delete(b.subs, id)
	}
}

// SubscriberCount returns the number of active subscribers (for testing).
func (b *MemoryBus) SubscriberCount() int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return len(b.subs)
}
