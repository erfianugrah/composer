package docker

import (
	"context"
	"time"

	"github.com/docker/docker/api/types/events"

	domevent "github.com/erfianugrah/composer/internal/domain/event"
)

// EventListener listens to Docker daemon events and translates them to domain events.
type EventListener struct {
	client *Client
	bus    domevent.Bus
	cancel context.CancelFunc
}

// NewEventListener creates a listener that bridges Docker events to the domain event bus.
func NewEventListener(client *Client, bus domevent.Bus) *EventListener {
	return &EventListener{client: client, bus: bus}
}

// Start begins listening for Docker events in a background goroutine.
func (l *EventListener) Start(ctx context.Context) {
	ctx, l.cancel = context.WithCancel(ctx)

	go func() {
		backoff := time.Second
		const maxBackoff = 60 * time.Second

		for {
			gotEvents := l.listen(ctx)

			select {
			case <-ctx.Done():
				return
			default:
				if gotEvents {
					backoff = time.Second // reset on successful connection
				}
				time.Sleep(backoff)
				backoff *= 2
				if backoff > maxBackoff {
					backoff = maxBackoff
				}
			}
		}
	}()
}

// Stop stops the event listener.
func (l *EventListener) Stop() {
	if l.cancel != nil {
		l.cancel()
	}
}

func (l *EventListener) listen(ctx context.Context) (gotEvents bool) {
	msgs, errs := l.client.Events(ctx)

	for {
		select {
		case msg, ok := <-msgs:
			if !ok {
				return
			}
			gotEvents = true
			l.handleEvent(msg)

		case _, ok := <-errs:
			if !ok {
				return
			}
			return // will reconnect

		case <-ctx.Done():
			return
		}
	}
}

func (l *EventListener) handleEvent(msg events.Message) {
	now := time.Now()
	stack := msg.Actor.Attributes["com.docker.compose.project"]

	switch msg.Type {
	case events.ContainerEventType:
		switch msg.Action {
		case events.ActionStart, events.ActionStop, events.ActionDie,
			events.ActionPause, events.ActionUnPause, events.ActionRestart:
			l.bus.Publish(domevent.ContainerStateChanged{
				ContainerID: msg.Actor.ID[:12],
				StackName:   stack,
				OldStatus:   "", // Docker events don't provide old state
				NewStatus:   string(msg.Action),
				Timestamp:   now,
			})

		case events.ActionHealthStatus:
			health := msg.Actor.Attributes["health_status"]
			l.bus.Publish(domevent.ContainerHealthChanged{
				ContainerID: msg.Actor.ID[:12],
				StackName:   stack,
				OldHealth:   "",
				NewHealth:   health,
				Timestamp:   now,
			})
		}
	}
}
