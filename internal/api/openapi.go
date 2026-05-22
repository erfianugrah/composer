package api

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	composer "github.com/erfianugrah/composer"
	"github.com/erfianugrah/composer/internal/api/dto"
	"github.com/erfianugrah/composer/internal/api/handler"
)

// HumaConfig returns the canonical Huma configuration for the Composer API.
//
// This is the single source of truth for OpenAPI metadata (info, servers,
// security schemes, tags). Both the live server (internal/api/server.go) and
// the build-time spec dumper (cmd/dumpopenapi) call this so the runtime
// /openapi.json and the committed web/src/lib/api/openapi.json never drift.
//
// Pass composer.Version as the version. Accepting it as a parameter keeps
// this package free of package-init ordering surprises.
func HumaConfig(version string) huma.Config {
	cfg := huma.DefaultConfig("Composer", version)

	cfg.Info.Description = "A lightweight, self-hosted Docker Compose management platform with GitOps, pipelines, and RBAC."
	cfg.Info.License = &huma.License{Name: "MIT", Identifier: "MIT"}
	cfg.Info.Contact = &huma.Contact{
		Name: "Composer",
		URL:  "https://github.com/erfianugrah/composer",
	}

	cfg.OpenAPI.Servers = []*huma.Server{
		{URL: "/", Description: "Current host"},
	}

	// Three accepted auth schemes: session cookie (browser), API key header
	// (CLI/CI), Bearer (alt form for tools that won't send custom headers).
	cfg.OpenAPI.Components.SecuritySchemes = map[string]*huma.SecurityScheme{
		"cookieAuth": {
			Type:        "apiKey",
			In:          "cookie",
			Name:        "composer_session",
			Description: "Session cookie set by POST /api/v1/auth/login",
		},
		"apiKeyAuth": {
			Type:        "apiKey",
			In:          "header",
			Name:        "X-API-Key",
			Description: "API key created via POST /api/v1/keys",
		},
		"bearerAuth": {
			Type:         "http",
			Scheme:       "bearer",
			BearerFormat: "composer-api-key",
			Description:  "API key passed as `Authorization: Bearer <key>`",
		},
	}
	// Any registered scheme satisfies the default security requirement.
	cfg.OpenAPI.Security = []map[string][]string{
		{"cookieAuth": {}},
		{"apiKeyAuth": {}},
		{"bearerAuth": {}},
	}

	cfg.OpenAPI.Tags = []*huma.Tag{
		{Name: "system", Description: "Server info, version, and global configuration"},
		{Name: "auth", Description: "Login, logout, session, and bootstrap"},
		{Name: "users", Description: "User management (admin only)"},
		{Name: "keys", Description: "API key management (plaintext shown once at creation)"},
		{Name: "registries", Description: "Docker registry credentials (global + per-stack, encrypted at rest)"},
		{Name: "stacks", Description: "Docker Compose stack lifecycle"},
		{Name: "git", Description: "Git-backed stack sync, rollback, and deploy pipeline"},
		{Name: "containers", Description: "Container inspection and lifecycle"},
		{Name: "networks", Description: "Docker network management"},
		{Name: "volumes", Description: "Docker volume management"},
		{Name: "images", Description: "Docker image management"},
		{Name: "docker", Description: "Daemon-wide operations (events, prune, exec)"},
		{Name: "pipelines", Description: "Multi-step automation pipelines with triggers"},
		{Name: "webhooks", Description: "Inbound git webhook configuration and delivery history"},
		{Name: "jobs", Description: "Background job status for async compose operations"},
		{Name: "audit", Description: "Audit log of mutating operations (admin only)"},
		{Name: "templates", Description: "Built-in stack template catalog"},
		{Name: "sse", Description: "Server-Sent Events streams (logs, stats, events, pipeline output)"},
		{Name: "oauth", Description: "OAuth/OIDC login flow (raw chi handlers, documented here for discoverability)"},
	}

	return cfg
}

// RegisterHumaHandlers registers every Huma-typed handler against `api`.
//
// When `registerAll` is true (build-time spec dumper), every handler is
// registered regardless of whether deps are wired — Huma only reflects on
// Input/Output types at registration time, so handler methods are never
// invoked and nil deps are safe. This guarantees the dumped spec includes
// every endpoint the codebase declares.
//
// When `registerAll` is false (runtime), each handler is gated on the deps
// it actually needs, so degraded-mode boots (e.g. Docker socket missing)
// still serve a coherent subset of endpoints.
//
// Raw chi handlers (OAuth, webhook receiver) are NOT registered here — they
// don't go through Huma. Use DocumentRawRoutes to make them visible in the
// OpenAPI spec.
func RegisterHumaHandlers(api huma.API, deps Deps, registerAll bool) {
	register := func(cond bool, fn func()) {
		if registerAll || cond {
			fn()
		}
	}

	// Health check is intentionally registered inline (rather than via a
	// handler struct) because the implementation has no dependencies and
	// the response is trivial. Kept here so the spec dumper and the runtime
	// see it identically.
	huma.Register(api, huma.Operation{
		OperationID: "healthCheck",
		Method:      http.MethodGet,
		Path:        "/api/v1/system/health",
		Summary:     "Health check",
		Description: "Public liveness probe. Returns {status, version}. No authentication required.",
		Tags:        []string{"system"},
		Security:    []map[string][]string{}, // public
	}, func(ctx context.Context, _ *struct{}) (*dto.HealthCheckOutput, error) {
		resp := &dto.HealthCheckOutput{}
		resp.Body.Status = "healthy"
		resp.Body.Version = composer.Version
		return resp, nil
	})

	// Always-on handlers (no deps required for registration).
	register(true, func() { handler.NewSystemHandler(deps.DockerClient, deps.DataDir).Register(api) })
	register(true, func() { handler.NewTemplateHandler().Register(api) })
	register(true, func() { handler.NewAuthHandler(deps.AuthService).Register(api) })
	register(true, func() { handler.NewKeyHandler(deps.AuthService).Register(api) })

	// User management (admin only).
	register(deps.UserRepo != nil, func() {
		uh := handler.NewUserHandler(deps.UserRepo)
		if deps.SessionRepo != nil {
			uh.SetSessionRepo(deps.SessionRepo)
		}
		uh.Register(api)
	})

	register(deps.RegistryService != nil, func() {
		handler.NewRegistryHandler(deps.RegistryService).Register(api)
	})
	register(deps.StackService != nil, func() {
		handler.NewStackHandler(deps.StackService, deps.Jobs).Register(api)
	})
	register(deps.DockerClient != nil, func() {
		handler.NewContainerHandler(deps.DockerClient).Register(api)
	})
	register(deps.EventBus != nil || deps.DockerClient != nil, func() {
		handler.NewSSEHandler(deps.EventBus, deps.DockerClient).Register(api)
	})
	register(deps.GitService != nil, func() {
		handler.NewGitHandler(deps.GitService, deps.Jobs).Register(api)
	})
	register(deps.PipelineService != nil, func() {
		handler.NewPipelineHandler(deps.PipelineService).Register(api)
	})
	register(deps.WebhookRepo != nil, func() {
		handler.NewWebhookCRUDHandler(deps.WebhookRepo).Register(api)
	})
	register(deps.AuditRepo != nil, func() {
		handler.NewAuditHandler(deps.AuditRepo).Register(api)
	})
	register(deps.Jobs != nil, func() {
		handler.NewJobHandler(deps.Jobs).Register(api)
	})
	register(deps.DockerClient != nil, func() {
		handler.NewResourceHandler(deps.DockerClient, deps.Jobs).Register(api)
	})
	register(deps.Compose != nil, func() {
		handler.NewDockerExecHandler(deps.Compose).Register(api)
	})
}

// DocumentRawRoutes adds OpenAPI entries for routes served by raw chi handlers
// (OAuth begin/callback, inbound webhook receiver). These routes can't go
// through Huma because they need direct net/http access (goth state machine,
// raw request body signature validation).
//
// Calling this is safe in both the runtime server and the spec dumper —
// AddOperation only mutates api.OpenAPI().Paths, it does not register any
// chi routes. The real chi handlers (registered separately in server.go)
// continue to serve traffic.
func DocumentRawRoutes(api huma.API) {
	spec := api.OpenAPI()

	// OAuth begin — redirects browser to the IdP authorise URL.
	spec.AddOperation(&huma.Operation{
		OperationID: "oauthBegin",
		Method:      http.MethodGet,
		Path:        "/api/v1/auth/oauth/{provider}",
		Summary:     "Begin OAuth/OIDC login",
		Description: "Redirects the browser to the configured identity provider's authorise URL. " +
			"Served by a raw chi handler (gothic state machine); not callable from typed API clients. " +
			"Documented here so consumers can discover the endpoint.",
		Tags:     []string{"oauth", "auth"},
		Security: []map[string][]string{}, // public — user is not yet authenticated
		Parameters: []*huma.Param{{
			Name:        "provider",
			In:          "path",
			Required:    true,
			Description: "OAuth provider name (e.g. `google`, `github`, `generic`)",
			Schema:      &huma.Schema{Type: "string"},
		}},
		Responses: map[string]*huma.Response{
			"302": {Description: "Redirect to provider authorise URL"},
			"404": {Description: "Provider not configured"},
		},
	})

	spec.AddOperation(&huma.Operation{
		OperationID: "oauthCallback",
		Method:      http.MethodGet,
		Path:        "/api/v1/auth/oauth/{provider}/callback",
		Summary:     "OAuth/OIDC callback",
		Description: "Receives the authorisation code from the identity provider, exchanges it for tokens, " +
			"creates or links the local user, and sets the `composer_session` cookie. " +
			"Served by a raw chi handler; not directly callable from typed API clients.",
		Tags:     []string{"oauth", "auth"},
		Security: []map[string][]string{}, // public — completing auth
		Parameters: []*huma.Param{{
			Name:     "provider",
			In:       "path",
			Required: true,
			Schema:   &huma.Schema{Type: "string"},
		}},
		Responses: map[string]*huma.Response{
			"302": {Description: "Redirect to the frontend after successful login"},
			"401": {Description: "Authentication failed"},
		},
	})

	// Inbound git webhook receiver — validates signature, enqueues sync/deploy.
	spec.AddOperation(&huma.Operation{
		OperationID: "receiveWebhook",
		Method:      http.MethodPost,
		Path:        "/api/v1/hooks/{id}",
		Summary:     "Inbound git webhook receiver",
		Description: "Receives push events from a configured git host (GitHub, Gitea, Forgejo). " +
			"Validates the HMAC signature against the per-webhook secret and enqueues the configured " +
			"sync / deploy / pipeline action. Served by a raw chi handler so the raw request body is " +
			"available for signature validation; not callable from typed API clients.",
			// No body schema — signature validation requires raw bytes, and
			// each git host uses a different envelope shape.
		Tags:     []string{"webhooks"},
		Security: []map[string][]string{}, // signature-authenticated, not session-authenticated
		Parameters: []*huma.Param{{
			Name:        "id",
			In:          "path",
			Required:    true,
			Description: "Webhook ID created via POST /api/v1/webhooks",
			Schema:      &huma.Schema{Type: "string", Format: "uuid"},
		}},
		Responses: map[string]*huma.Response{
			"202": {Description: "Webhook accepted; job enqueued"},
			"400": {Description: "Malformed payload"},
			"401": {Description: "Signature validation failed"},
			"404": {Description: "Unknown webhook ID"},
		},
	})
}
