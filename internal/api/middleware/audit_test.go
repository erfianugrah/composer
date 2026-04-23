package middleware

import (
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/erfianugrah/composer/internal/infra/store"
)

func TestDeriveAction(t *testing.T) {
	tests := []struct {
		method string
		path   string
		want   string
	}{
		// Stack operations
		{"POST", "/api/v1/stacks/web-app/up", "stack.up"},
		{"POST", "/api/v1/stacks/web-app/down", "stack.down"},
		{"POST", "/api/v1/stacks/web-app/restart", "stack.restart"},
		{"POST", "/api/v1/stacks/web-app/pull", "stack.pull"},
		{"POST", "/api/v1/stacks", "stack.create"},
		{"PUT", "/api/v1/stacks/web-app", "stack.update"},
		{"DELETE", "/api/v1/stacks/web-app", "stack.delete"},

		// Auth
		{"POST", "/api/v1/auth/login", "auth.login"},
		{"POST", "/api/v1/auth/bootstrap", "auth.bootstrap"},
		{"POST", "/api/v1/auth/logout", "auth.logout"},

		// Users
		{"POST", "/api/v1/users", "user.create"},
		{"PUT", "/api/v1/users/abc123", "user.update"},
		{"DELETE", "/api/v1/users/abc123", "user.delete"},
		{"PUT", "/api/v1/users/abc123/password", "user.password"},

		// Keys
		{"POST", "/api/v1/keys", "key.create"},
		{"DELETE", "/api/v1/keys/ck_abc", "key.delete"},

		// Pipelines
		{"POST", "/api/v1/pipelines", "pipeline.create"},
		{"POST", "/api/v1/pipelines/pl_123/run", "pipeline.run"},
		{"DELETE", "/api/v1/pipelines/pl_123", "pipeline.delete"},

		// Webhooks
		{"POST", "/api/v1/webhooks", "webhook.create"},
		{"DELETE", "/api/v1/webhooks/wh_123", "webhook.delete"},

		// Git
		{"POST", "/api/v1/stacks/infra/sync", "stack.sync"},

		// Containers
		{"POST", "/api/v1/containers/abc123/start", "container.start"},
		{"POST", "/api/v1/containers/abc123/stop", "container.stop"},
	}

	for _, tt := range tests {
		t.Run(tt.method+" "+tt.path, func(t *testing.T) {
			got := deriveAction(tt.method, tt.path)
			if got != tt.want {
				t.Errorf("deriveAction(%q, %q) = %q, want %q", tt.method, tt.path, got, tt.want)
			}
		})
	}
}

// TestAuditWorkerPool_DropsWhenFull guards against regression of the
// unbounded goroutine-per-mutation pattern. Filling the jobs channel past
// its capacity must result in drops (dropped counter increments, submit
// never blocks) rather than spawning unlimited goroutines or blocking
// the HTTP request.
func TestAuditWorkerPool_DropsWhenFull(t *testing.T) {
	// Construct a pool WITHOUT calling start() — we want to test the
	// submit-side overflow behavior in isolation, without real worker
	// goroutines that would drain the queue or panic on a nil repo.
	pool := &auditWorkerPool{
		jobs: make(chan auditJob, 4), // tiny capacity to force overflow
	}

	submitted := atomic.Int32{}
	done := make(chan struct{})
	go func() {
		defer close(done)
		// Submit 20 entries to a 4-slot queue. None are consumed (no workers).
		// Expect the first 4 to be buffered and the remaining 16 to be dropped.
		for i := 0; i < 20; i++ {
			pool.submit(store.AuditEntry{ID: "test", Action: "test.action"})
			submitted.Add(1)
		}
	}()

	// Submit loop must complete promptly — if submit() ever blocks on a full
	// channel, the test hangs past this deadline.
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("submit() blocked on full queue; expected drop-if-full semantics")
	}

	assert.Equal(t, int32(20), submitted.Load(), "all submits returned")

	pool.mu.Lock()
	dropped := pool.dropped
	pool.mu.Unlock()

	assert.Equal(t, uint64(16), dropped,
		"expected 20 submits - 4 buffered = 16 drops, got %d", dropped)
	assert.Equal(t, 4, len(pool.jobs), "buffered count")
}
