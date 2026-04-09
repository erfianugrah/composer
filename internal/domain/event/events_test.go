package event

import (
	"testing"
	"time"
)

// TestAllEventsImplementInterface verifies every event type satisfies the Event interface
// and returns non-empty EventType strings.
func TestAllEventsImplementInterface(t *testing.T) {
	now := time.Now()

	events := []Event{
		StackCreated{Name: "s", Timestamp: now},
		StackDeployed{Name: "s", Timestamp: now},
		StackStopped{Name: "s", Timestamp: now},
		StackUpdated{Name: "s", Timestamp: now},
		StackDeleted{Name: "s", Timestamp: now},
		StackError{Name: "s", Error: "e", Timestamp: now},
		ContainerStateChanged{ContainerID: "c", Timestamp: now},
		ContainerHealthChanged{ContainerID: "c", Timestamp: now},
		PipelineRunStarted{PipelineID: "p", RunID: "r", Timestamp: now},
		PipelineStepStarted{PipelineID: "p", RunID: "r", StepID: "s", Timestamp: now},
		PipelineStepFinished{PipelineID: "p", RunID: "r", StepID: "s", Timestamp: now},
		PipelineRunFinished{PipelineID: "p", RunID: "r", Timestamp: now},
		ContainerStats{ContainerID: "c", Timestamp: now},
		LogEntry{ContainerID: "c", Stream: "stdout", Message: "hi", Timestamp: now},
	}

	seen := make(map[string]bool)
	for _, e := range events {
		typ := e.EventType()
		if typ == "" {
			t.Errorf("EventType() returned empty string for %T", e)
		}
		if seen[typ] {
			t.Errorf("duplicate EventType %q", typ)
		}
		seen[typ] = true

		if e.EventTime().IsZero() {
			t.Errorf("EventTime() returned zero for %T", e)
		}
		if !e.EventTime().Equal(now) {
			t.Errorf("EventTime() = %v, want %v for %T", e.EventTime(), now, e)
		}
	}

	if len(seen) != 14 {
		t.Errorf("expected 14 unique event types, got %d", len(seen))
	}
}
