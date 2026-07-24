package app

import "sync"

// StackLocks provides per-stack mutual exclusion for compose operations.
// Shared across all services (StackService, GitService, PipelineExecutor)
// to prevent concurrent docker compose calls on the same stack.
//
// Entries are reference-counted: the per-stack mutex stays alive as long as
// any goroutine holds or is waiting for it, and is removed from the map only
// once the last holder releases it. This closes the race where Delete (or an
// Unlock that dropped the entry) could remove a mutex that another goroutine
// had already read and was about to lock/unlock -- which previously let a new
// Lock create a *different* mutex for the same name, allowing two compose
// operations to run concurrently on one stack.
type StackLocks struct {
	mu    sync.Mutex
	locks map[string]*lockEntry
}

type lockEntry struct {
	mu   sync.Mutex
	refs int // holders + waiters, guarded by StackLocks.mu
}

// NewStackLocks creates a new shared lock manager.
func NewStackLocks() *StackLocks {
	return &StackLocks{locks: make(map[string]*lockEntry)}
}

// Lock acquires the mutex for the named stack. Blocks if already held.
func (l *StackLocks) Lock(name string) {
	l.mu.Lock()
	e, ok := l.locks[name]
	if !ok {
		e = &lockEntry{}
		l.locks[name] = e
	}
	e.refs++
	l.mu.Unlock()
	e.mu.Lock()
}

// Unlock releases the mutex for the named stack. The entry is dropped from
// the map once no goroutine references it, so the map does not grow without
// bound and Delete becomes unnecessary for cleanup.
func (l *StackLocks) Unlock(name string) {
	l.mu.Lock()
	e, ok := l.locks[name]
	if !ok {
		l.mu.Unlock()
		return
	}
	e.refs--
	if e.refs <= 0 {
		delete(l.locks, name)
	}
	l.mu.Unlock()
	e.mu.Unlock()
}

// Delete removes the lock entry for a stack (used on stack deletion). It is a
// no-op when the lock is currently referenced -- the entry is then cleaned up
// by the final Unlock -- so it can never orphan an in-use mutex.
func (l *StackLocks) Delete(name string) {
	l.mu.Lock()
	if e, ok := l.locks[name]; ok && e.refs <= 0 {
		delete(l.locks, name)
	}
	l.mu.Unlock()
}
