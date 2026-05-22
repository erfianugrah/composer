package api

import (
	"os"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"

	composer "github.com/erfianugrah/composer"
	"github.com/erfianugrah/composer/internal/api/handler"
	authmw "github.com/erfianugrah/composer/internal/api/middleware"
	"github.com/erfianugrah/composer/internal/api/ws"
	"github.com/erfianugrah/composer/internal/app"
	"github.com/erfianugrah/composer/internal/domain/auth"
	"github.com/erfianugrah/composer/internal/domain/event"
	"github.com/erfianugrah/composer/internal/infra/docker"
	"github.com/erfianugrah/composer/internal/infra/store"
)

// Server is the HTTP API server.
type Server struct {
	Router chi.Router
	API    huma.API
}

// Deps holds all dependencies needed by the API server.
type Deps struct {
	AuthService     *app.AuthService
	StackService    *app.StackService      // nil if Docker not available
	GitService      *app.GitService        // nil disables git operations
	PipelineService *app.PipelineService   // nil disables pipeline operations
	RegistryService *app.RegistryService   // nil disables registry credential endpoints
	UserRepo        auth.UserRepository    // nil disables user management
	SessionRepo     auth.SessionRepository // needed for OAuth session persistence
	WebhookRepo     *store.WebhookRepo     // nil disables webhook receiver
	AuditRepo       *store.AuditRepo       // nil disables audit logging
	EventBus        event.Bus              // nil disables SSE events endpoint
	DockerClient    *docker.Client         // nil disables container/SSE/terminal endpoints
	Compose         *docker.Compose        // nil disables docker exec
	Jobs            *app.JobManager        // background job tracker
	DataDir         string                 // COMPOSER_DATA_DIR for config lookups
}

// NewServer creates a new API server with all routes registered.
func NewServer(deps Deps) *Server {
	router := chi.NewMux()

	// Global middleware
	router.Use(chimiddleware.RequestID)
	// Only trust X-Real-IP/X-Forwarded-For when behind a reverse proxy.
	// Without this, clients can spoof their IP to bypass rate limiting and audit logs.
	if os.Getenv("COMPOSER_TRUSTED_PROXIES") != "" {
		router.Use(chimiddleware.RealIP)
	}
	router.Use(chimiddleware.Recoverer)
	router.Use(authmw.StoreRemoteIP)       // Store client IP for Huma handlers
	router.Use(authmw.ExtendWriteDeadline) // Disable write timeout for SSE/WS paths
	router.Use(authmw.SecurityHeaders)
	router.Use(authmw.RateLimit(authmw.GeneralRateLimit()))

	// Auth middleware
	router.Use(authmw.Auth(deps.AuthService))

	// CSRF protection (X-Requested-With on cookie-based mutations)
	router.Use(authmw.CSRF)

	// Audit middleware (logs mutating API requests)
	if deps.AuditRepo != nil {
		router.Use(authmw.Audit(deps.AuditRepo))
	}

	// Huma API — config + handler registration live in openapi.go so the
	// runtime spec and the build-time dump (cmd/dumpopenapi) stay in sync.
	api := humachi.New(router, HumaConfig(composer.Version))
	RegisterHumaHandlers(api, deps, false /* register conditionally on deps */)

	// Document raw chi routes (OAuth, webhook receiver) in the OpenAPI spec.
	// These can't be registered via Huma because they need raw http access
	// (goth state machine, raw body for signature validation) — but adding
	// them to the spec via AddOperation keeps the API surface discoverable.
	DocumentRawRoutes(api)

	// /docs is served by the embedded frontend (web/dist/docs/index.html)
	// Stoplight Elements bundled locally -- no CDN dependency

	// WebSocket terminal (raw HTTP handler with RBAC -- operator+)
	if deps.DockerClient != nil {
		termHandler := ws.NewTerminalHandler(deps.DockerClient)
		router.With(authmw.RequireRole(auth.RoleOperator)).
			Get("/api/v1/ws/terminal/{id}", termHandler.ServeHTTP)
	}

	// WebSocket compose action streaming (PTY output for pull/deploy progress)
	if deps.StackService != nil {
		composeWS := ws.NewComposeHandler(deps.StackService)
		router.With(authmw.RequireRole(auth.RoleOperator)).
			Get("/api/v1/ws/stacks/{name}/action", composeWS.ServeHTTP)
	}

	// OAuth/OIDC (raw chi handlers -- goth needs raw http)
	if deps.UserRepo != nil && deps.SessionRepo != nil {
		oauthHandler := handler.NewOAuthHandler(deps.AuthService, deps.UserRepo, deps.SessionRepo)
		if oauthHandler.Setup() {
			oauthHandler.RegisterRaw(router)
		}
	}

	// Webhook receiver (raw chi handler -- validates signature, not session)
	if deps.GitService != nil && deps.WebhookRepo != nil {
		webhookHandler := handler.NewWebhookHandler(deps.GitService, deps.WebhookRepo, deps.Jobs, deps.PipelineService)
		webhookHandler.RegisterRaw(router)
	}

	return &Server{Router: router, API: api}
}
