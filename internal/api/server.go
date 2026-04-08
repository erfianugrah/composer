package api

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"

	"github.com/erfianugrah/composer/internal/api/handler"
	authmw "github.com/erfianugrah/composer/internal/api/middleware"
	"github.com/erfianugrah/composer/internal/api/ws"
	"github.com/erfianugrah/composer/internal/app"
	"github.com/erfianugrah/composer/internal/domain/auth"
	"github.com/erfianugrah/composer/internal/domain/event"
	"github.com/erfianugrah/composer/internal/infra/docker"
)

// Server is the HTTP API server.
type Server struct {
	Router chi.Router
	API    huma.API
}

// Deps holds all dependencies needed by the API server.
type Deps struct {
	AuthService  *app.AuthService
	StackService *app.StackService   // nil if Docker not available
	UserRepo     auth.UserRepository // nil disables user management
	EventBus     event.Bus           // nil disables SSE events endpoint
	DockerClient *docker.Client      // nil disables container/SSE/terminal endpoints
}

// NewServer creates a new API server with all routes registered.
func NewServer(deps Deps) *Server {
	router := chi.NewMux()

	// Global middleware
	router.Use(chimiddleware.RequestID)
	router.Use(chimiddleware.RealIP)
	router.Use(chimiddleware.Recoverer)
	router.Use(authmw.SecurityHeaders)
	router.Use(authmw.RateLimit(authmw.GeneralRateLimit()))

	// Auth middleware
	router.Use(authmw.Auth(deps.AuthService))

	// Huma API (auto-generates OpenAPI 3.1)
	config := huma.DefaultConfig("Composer", "0.1.0")
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
			Version string `json:"version" example:"0.1.0"`
		}
	}, error) {
		resp := &struct {
			Body struct {
				Status  string `json:"status" example:"healthy"`
				Version string `json:"version" example:"0.1.0"`
			}
		}{}
		resp.Body.Status = "healthy"
		resp.Body.Version = "0.1.0"
		return resp, nil
	})

	// Auth handlers (always registered)
	handler.NewAuthHandler(deps.AuthService).Register(api)

	// User management (admin only)
	if deps.UserRepo != nil {
		handler.NewUserHandler(deps.UserRepo).Register(api)
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

	// WebSocket terminal (raw HTTP handler with RBAC -- operator+)
	if deps.DockerClient != nil {
		termHandler := ws.NewTerminalHandler(deps.DockerClient)
		router.With(authmw.RequireRole(auth.RoleOperator)).
			Get("/api/v1/ws/terminal/{id}", termHandler.ServeHTTP)
	}

	return &Server{Router: router, API: api}
}
