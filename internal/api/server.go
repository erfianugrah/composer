package api

import (
	"context"
	"net/http"
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
	UserRepo        auth.UserRepository    // nil disables user management
	SessionRepo     auth.SessionRepository // needed for OAuth session persistence
	WebhookRepo     *store.WebhookRepo     // nil disables webhook receiver
	AuditRepo       *store.AuditRepo       // nil disables audit logging
	EventBus        event.Bus              // nil disables SSE events endpoint
	DockerClient    *docker.Client         // nil disables container/SSE/terminal endpoints
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

	// Audit log query (registered below after Huma API is created)

	// Huma API (auto-generates OpenAPI 3.1)
	config := huma.DefaultConfig("Composer", composer.Version)
	config.Info.Description = "A lightweight, self-hosted Docker Compose management platform with GitOps, pipelines, and RBAC."
	api := humachi.New(router, config)

	// Health check (bypasses auth)
	huma.Register(api, huma.Operation{
		OperationID: "healthCheck",
		Method:      http.MethodGet,
		Path:        "/api/v1/system/health",
		Summary:     "Health check",
		Tags:        []string{"system"},
	}, func(ctx context.Context, input *struct{}) (*struct {
		Body struct {
			Status  string `json:"status" example:"healthy"`
			Version string `json:"version" example:"0.3.0"`
		}
	}, error) {
		resp := &struct {
			Body struct {
				Status  string `json:"status" example:"healthy"`
				Version string `json:"version" example:"0.3.0"`
			}
		}{}
		resp.Body.Status = "healthy"
		resp.Body.Version = composer.Version
		return resp, nil
	})

	// System info/version
	handler.NewSystemHandler(deps.DockerClient).Register(api)

	// /docs -- Stoplight Elements API docs UI (serves inline HTML)
	router.Get("/docs", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte(`<!doctype html><html lang="en"><head><meta charset="utf-8"><title>Composer API</title>
<script src="https://unpkg.com/@stoplight/elements/web-components.min.js"></script>
<link rel="stylesheet" href="https://unpkg.com/@stoplight/elements/styles.min.css">
</head><body><elements-api apiDescriptionUrl="/openapi.json" router="hash" layout="sidebar"/></body></html>`))
	})

	// Templates (public, helps onboarding)
	handler.NewTemplateHandler().Register(api)

	// Auth handlers (always registered)
	handler.NewAuthHandler(deps.AuthService).Register(api)

	// User management (admin only)
	if deps.UserRepo != nil {
		userHandler := handler.NewUserHandler(deps.UserRepo)
		if deps.SessionRepo != nil {
			userHandler.SetSessionRepo(deps.SessionRepo)
		}
		userHandler.Register(api)
	}

	// API key management (operator+)
	handler.NewKeyHandler(deps.AuthService).Register(api)

	// Stack handlers (requires Docker)
	if deps.StackService != nil {
		handler.NewStackHandler(deps.StackService).Register(api)
	}

	// Container handlers (requires Docker)
	if deps.DockerClient != nil {
		handler.NewContainerHandler(deps.DockerClient).Register(api)
	}

	// SSE handlers (requires event bus and/or Docker)
	if deps.EventBus != nil || deps.DockerClient != nil {
		handler.NewSSEHandler(deps.EventBus, deps.DockerClient).Register(api)
	}

	// Git operation handlers (requires GitService)
	if deps.GitService != nil {
		handler.NewGitHandler(deps.GitService).Register(api)
	}

	// Pipeline handlers (requires PipelineService)
	if deps.PipelineService != nil {
		handler.NewPipelineHandler(deps.PipelineService).Register(api)
	}

	// Webhook CRUD (requires WebhookRepo)
	if deps.WebhookRepo != nil {
		handler.NewWebhookCRUDHandler(deps.WebhookRepo).Register(api)
	}

	// Audit log (requires AuditRepo)
	if deps.AuditRepo != nil {
		handler.NewAuditHandler(deps.AuditRepo).Register(api)
	}

	// WebSocket terminal (raw HTTP handler with RBAC -- operator+)
	if deps.DockerClient != nil {
		termHandler := ws.NewTerminalHandler(deps.DockerClient)
		router.With(authmw.RequireRole(auth.RoleOperator)).
			Get("/api/v1/ws/terminal/{id}", termHandler.ServeHTTP)
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
		webhookHandler := handler.NewWebhookHandler(deps.GitService, deps.WebhookRepo)
		webhookHandler.RegisterRaw(router)
	}

	return &Server{Router: router, API: api}
}
