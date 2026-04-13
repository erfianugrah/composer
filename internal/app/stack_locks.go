package app

import "sync"

// StackLocks provides per-stack mutual exclusion for compose operations.
// Shared across all services (StackService, GitService, PipelineExecutor)
// to prevent concurrent docker compose calls on the same stack.
type StackLocks struct {
	mu    sync.Mutex
	locks map[string]*sync.Mutex
}

// NewStackLocks creates a new shared lock manager.
func NewStackLocks() *StackLocks {
	return &StackLocks{locks: make(map[string]*sync.Mutex)}
}

// Lock acquires the mutex for the named stack. Blocks if already held.
func (l *StackLocks) Lock(name string) {
	l.mu.Lock()
	m, ok := l.locks[name]
	if !ok {
		m = &sync.Mutex{}
		l.locks[name] = m
	}
	l.mu.Unlock()
	m.Lock()
}

// Unlock releases the mutex for the named stack.
func (l *StackLocks) Unlock(name string) {
	l.mu.Lock()
	m := l.locks[name]
	l.mu.Unlock()
	if m != nil {
		m.Unlock()
	}
}

// Delete removes the lock entry for a stack (used on stack deletion).
func (l *StackLocks) Delete(name string) {
	l.mu.Lock()
	delete(l.locks, name)
	l.mu.Unlock()
}
