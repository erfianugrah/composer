package app

import (
	"context"
	"os"
	"strconv"
	"sync"
	"time"

	"go.uber.org/zap"
)

// defaultStatusRefreshInterval is the snapshot refresh period when
// COMPOSER_STATUS_REFRESH_MS is unset or invalid.
const defaultStatusRefreshInterval = 15 * time.Second

// StackStatus is the snapshot entry for one compose project: container
// counts plus whether the project's docker host answered the last listing.
type StackStatus struct {
	Total     int
	Running   int
	Reachable bool
}

// StatusRefresher periodically lists containers on every docker host and
// keeps an in-memory snapshot (per-stack counts + per-host reachability) so
// read endpoints can serve current-looking data without touching a daemon.
// HostID 0 is the local/default host.
type StatusRefresher struct {
	svc      *StackService
	log      *zap.Logger
	interval time.Duration

	mu      sync.RWMutex
	byStack map[string]StackStatus
	hostOK  map[int64]bool
}

// NewStatusRefresher creates a StatusRefresher. The interval defaults to
// 15s; COMPOSER_STATUS_REFRESH_MS overrides it (milliseconds, parsed once
// here; invalid or <=0 falls back to the default).
func NewStatusRefresher(svc *StackService, log *zap.Logger) *StatusRefresher {
	if log == nil {
		log = zap.NewNop()
	}
	return &StatusRefresher{
		svc:      svc,
		log:      log.Named("statusrefresher"),
		interval: statusRefreshInterval(),
		byStack:  make(map[string]StackStatus),
		hostOK:   make(map[int64]bool),
	}
}

func statusRefreshInterval() time.Duration {
	if v := os.Getenv("COMPOSER_STATUS_REFRESH_MS"); v != "" {
		if ms, err := strconv.Atoi(v); err == nil && ms > 0 {
			return time.Duration(ms) * time.Millisecond
		}
	}
	return defaultStatusRefreshInterval
}

// Refresh lists containers on every host once and replaces the snapshot.
// Stacks pinned to a host that did not answer keep their last-known counts
// (marked unreachable) instead of dropping out of the snapshot.
func (r *StatusRefresher) Refresh(ctx context.Context) {
	hosts, err := r.svc.ListContainersByHost(ctx)
	if err != nil {
		r.log.Warn("status refresh failed", zap.Error(err))
		return
	}

	r.mu.RLock()
	prev := r.byStack
	r.mu.RUnlock()

	byStack := make(map[string]StackStatus, len(prev))
	hostOK := make(map[int64]bool)
	for _, h := range hosts {
		hostOK[h.HostID] = h.Reachable
		for _, c := range h.Containers {
			if c.StackName == "" {
				continue
			}
			b := byStack[c.StackName]
			b.Total++
			if c.IsRunning() {
				b.Running++
			}
			b.Reachable = h.Reachable
			byStack[c.StackName] = b
		}
	}
	// Carry over stale counts for stacks whose host failed this round.
	for name, st := range prev {
		if _, fresh := byStack[name]; fresh {
			continue
		}
		if hostID, pinned := stackHostID(name, hosts); pinned && !hostOK[hostID] {
			st.Reachable = false
			byStack[name] = st
		}
	}

	r.mu.Lock()
	r.byStack = byStack
	r.hostOK = hostOK
	r.mu.Unlock()
}

// stackHostID finds the host a stack is pinned to in the fan-out results.
func stackHostID(stackName string, hosts []HostContainers) (int64, bool) {
	for _, h := range hosts {
		for _, n := range h.Stacks {
			if n == stackName {
				return h.HostID, true
			}
		}
	}
	return 0, false
}

// Start runs one synchronous refresh, then refreshes on the interval until
// ctx is done. It blocks; call it in a goroutine.
func (r *StatusRefresher) Start(ctx context.Context) {
	r.Refresh(ctx)
	ticker := time.NewTicker(r.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r.Refresh(ctx)
		}
	}
}

// Snapshot returns copies of the per-stack status and per-host reachability
// maps. A host absent from the second map never answered a listing (no
// stack pinned to it, or no refresh has completed yet).
func (r *StatusRefresher) Snapshot() (map[string]StackStatus, map[int64]bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make(map[string]StackStatus, len(r.byStack))
	for k, v := range r.byStack {
		out[k] = v
	}
	hostOK := make(map[int64]bool, len(r.hostOK))
	for k, v := range r.hostOK {
		hostOK[k] = v
	}
	return out, hostOK
}
