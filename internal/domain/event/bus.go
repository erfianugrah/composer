package event

// Subscriber receives events. Return false to unsubscribe.
type Subscriber func(Event) bool

// Bus is the interface for publishing and subscribing to domain events.
type Bus interface {
	// Publish sends an event to all subscribers.
	Publish(event Event)

	// Subscribe registers a subscriber that receives all events.
	// Returns an unsubscribe function.
	Subscribe(sub Subscriber) (unsubscribe func())

	// Close shuts down the bus and all subscribers.
	Close()
}
