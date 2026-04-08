package event

import "time"

// Event is the base interface for all domain events.
type Event interface {
	EventType() string
	EventTime() time.Time
}

// --- Stack Events ---

type StackCreated struct {
	Name      string    `json:"name"`
	Timestamp time.Time `json:"ts"`
}

func (e StackCreated) EventType() string    { return "stack.created" }
func (e StackCreated) EventTime() time.Time { return e.Timestamp }

type StackDeployed struct {
	Name      string    `json:"name"`
	Timestamp time.Time `json:"ts"`
}

func (e StackDeployed) EventType() string    { return "stack.deployed" }
func (e StackDeployed) EventTime() time.Time { return e.Timestamp }

type StackStopped struct {
	Name      string    `json:"name"`
	Timestamp time.Time `json:"ts"`
}

func (e StackStopped) EventType() string    { return "stack.stopped" }
func (e StackStopped) EventTime() time.Time { return e.Timestamp }

type StackUpdated struct {
	Name      string    `json:"name"`
	Timestamp time.Time `json:"ts"`
}

func (e StackUpdated) EventType() string    { return "stack.updated" }
func (e StackUpdated) EventTime() time.Time { return e.Timestamp }

type StackDeleted struct {
	Name      string    `json:"name"`
	Timestamp time.Time `json:"ts"`
}

func (e StackDeleted) EventType() string    { return "stack.deleted" }
func (e StackDeleted) EventTime() time.Time { return e.Timestamp }

type StackError struct {
	Name      string    `json:"name"`
	Error     string    `json:"error"`
	Timestamp time.Time `json:"ts"`
}

func (e StackError) EventType() string    { return "stack.error" }
func (e StackError) EventTime() time.Time { return e.Timestamp }

// --- Container Events ---

type ContainerStateChanged struct {
	ContainerID string    `json:"container_id"`
	StackName   string    `json:"stack"`
	OldStatus   string    `json:"old"`
	NewStatus   string    `json:"new"`
	Timestamp   time.Time `json:"ts"`
}

func (e ContainerStateChanged) EventType() string    { return "container.state" }
func (e ContainerStateChanged) EventTime() time.Time { return e.Timestamp }

type ContainerHealthChanged struct {
	ContainerID string    `json:"container_id"`
	StackName   string    `json:"stack"`
	OldHealth   string    `json:"old"`
	NewHealth   string    `json:"new"`
	Timestamp   time.Time `json:"ts"`
}

func (e ContainerHealthChanged) EventType() string    { return "container.health" }
func (e ContainerHealthChanged) EventTime() time.Time { return e.Timestamp }

// --- Log Event (for SSE streaming) ---

type LogEntry struct {
	ContainerID string    `json:"container_id"`
	Stream      string    `json:"stream"` // "stdout" or "stderr"
	Message     string    `json:"message"`
	Timestamp   time.Time `json:"ts"`
}

func (e LogEntry) EventType() string    { return "log" }
func (e LogEntry) EventTime() time.Time { return e.Timestamp }
