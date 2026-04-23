package middleware

import (
	"context"
	"crypto/rand"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/erfianugrah/composer/internal/infra/store"
)

// auditJob is the payload the middleware hands off to the worker pool.
type auditJob struct {
	entry store.AuditEntry
}

// auditWorkerPool bounds the number of in-flight audit-log writes so that
// sustained request load can't spawn unlimited goroutines. Drop-if-full
// semantics: when the queue is saturated, new entries are discarded with a
// counter increment rather than blocking the caller or queuing unboundedly.
type auditWorkerPool struct {
	jobs    chan auditJob
	repo    *store.AuditRepo
	once    sync.Once
	dropped uint64
	mu      sync.Mutex // guards dropped
}

// auditWorkers is the shared pool initialised once when Audit() is called.
// Declared at package scope because Audit() may be invoked multiple times
// during tests; the pool itself lives as long as the process.
var auditWorkers = &auditWorkerPool{}

// auditQueueSize is the backlog cap. A 1024-entry queue buffers ~10 s of
// audited mutations at 100 req/s — plenty of headroom for a self-hosted
// instance while still bounding memory.
const auditQueueSize = 1024

// auditWorkerCount is the number of concurrent writers. Each writer opens
// one DB connection for the duration of its Log call; 16 matches the
// typical default pool size for Postgres / sqlite.
const auditWorkerCount = 16

// start initialises the worker goroutines on first use.
func (p *auditWorkerPool) start(repo *store.AuditRepo) {
	p.once.Do(func() {
		p.repo = repo
		p.jobs = make(chan auditJob, auditQueueSize)
		for i := 0; i < auditWorkerCount; i++ {
			go p.worker()
		}
	})
}

func (p *auditWorkerPool) worker() {
	for job := range p.jobs {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		// store.AuditRepo.Log logs errors internally (repo has its own
		// zap.Logger wiring) and does not return, so there's nothing to
		// propagate here beyond running the write.
		p.repo.Log(ctx, job.entry)
		cancel()
	}
}

// submit enqueues the entry. Drop-if-full — never blocks the caller.
func (p *auditWorkerPool) submit(entry store.AuditEntry) {
	select {
	case p.jobs <- auditJob{entry: entry}:
	default:
		p.mu.Lock()
		p.dropped++
		d := p.dropped
		p.mu.Unlock()
		// Only warn on powers-of-two to avoid log spam under sustained overload
		if d&(d-1) == 0 {
			slog.Warn("audit log queue full, entry dropped",
				"action", entry.Action,
				"dropped_total", d)
		}
	}
}

// Audit returns middleware that logs mutating API operations.
//
// Writes are handed off to a bounded worker pool (auditWorkerCount workers,
// auditQueueSize-entry queue) so sustained request load can't spawn
// unlimited goroutines. If the queue is full the entry is dropped and a
// counter is incremented; the HTTP request is never blocked.
func Audit(repo *store.AuditRepo) func(http.Handler) http.Handler {
	auditWorkers.start(repo)
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Only audit mutating requests on API paths
			if !strings.HasPrefix(r.URL.Path, "/api/") {
				next.ServeHTTP(w, r)
				return
			}
			if r.Method == http.MethodGet || r.Method == http.MethodHead || r.Method == http.MethodOptions {
				next.ServeHTTP(w, r)
				return
			}

			// Capture response status
			sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}
			next.ServeHTTP(sw, r)

			userID := UserIDFromContext(r.Context())
			action := deriveAction(r.Method, r.URL.Path)
			// Use bare IP (no port) so audit rows key consistently by client.
			// RealIP middleware (when trusted proxy is configured) already
			// normalizes RemoteAddr to the real client IP.
			ip := ClientIP(r.RemoteAddr)
			if os.Getenv("COMPOSER_TRUSTED_PROXIES") != "" {
				if fwd := r.Header.Get("X-Real-IP"); fwd != "" {
					ip = ClientIP(fwd)
				}
			}

			var buf [8]byte
			rand.Read(buf[:])
			id := fmt.Sprintf("aud_%x", buf)

			auditWorkers.submit(store.AuditEntry{
				ID:        id,
				UserID:    userID,
				Action:    action,
				Resource:  r.URL.Path,
				Detail:    map[string]any{"method": r.Method, "status": sw.status},
				IPAddress: ip,
				CreatedAt: time.Now().UTC(),
			})
		})
	}
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

func deriveAction(method, path string) string {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	// /api/v1/stacks/mystack/up -> "stack.up"
	// /api/v1/auth/login -> "auth.login"
	if len(parts) >= 3 {
		resource := parts[2]                         // stacks, auth, users, etc.
		resource = strings.TrimSuffix(resource, "s") // stacks -> stack

		if len(parts) >= 5 {
			// /api/v1/stacks/mystack/up -> stack.up
			// /api/v1/users/abc/password -> user.password
			// /api/v1/containers/abc/start -> container.start
			return resource + "." + parts[len(parts)-1]
		}
		if len(parts) == 4 {
			lastPart := parts[3]
			// Check if last part is an action word (not an ID)
			// Auth paths: /api/v1/auth/login, /api/v1/auth/bootstrap, /api/v1/auth/logout
			// These have meaningful last segments, not IDs
			if resource == "auth" || resource == "hook" {
				return resource + "." + lastPart
			}
			switch method {
			case http.MethodPost:
				return resource + ".create"
			case http.MethodPut:
				return resource + ".update"
			case http.MethodDelete:
				return resource + ".delete"
			}
		}
		if len(parts) == 3 {
			return resource + ".create"
		}
	}
	return method + " " + path
}
