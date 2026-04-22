// dumpopenapi writes the full OpenAPI 3.1 spec for the Composer API to stdout.
//
// Used at build time to regenerate the TypeScript client bindings without
// standing up Postgres, Docker, etc. — all handlers are registered with nil
// deps (Huma only reflects on the Input/Output types at registration, so
// stub handlers never get invoked).
//
// Writes JSON to stdout by default; pass `-yaml` to emit YAML.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"os"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/go-chi/chi/v5"
	"gopkg.in/yaml.v3"

	composer "github.com/erfianugrah/composer"
	"github.com/erfianugrah/composer/internal/api/dto"
	"github.com/erfianugrah/composer/internal/api/handler"
)

func main() {
	asYAML := flag.Bool("yaml", false, "Emit YAML instead of JSON")
	flag.Parse()

	router := chi.NewMux()

	config := huma.DefaultConfig("Composer", composer.Version)
	config.Info.Description = "A lightweight, self-hosted Docker Compose management platform with GitOps, pipelines, and RBAC."
	config.Info.License = &huma.License{Name: "MIT", Identifier: "MIT"}
	config.Info.Contact = &huma.Contact{Name: "Composer", URL: "https://github.com/erfianugrah/composer"}
	config.OpenAPI.Servers = []*huma.Server{{URL: "/", Description: "Current host"}}
	config.OpenAPI.Components.SecuritySchemes = map[string]*huma.SecurityScheme{
		"cookieAuth": {Type: "apiKey", In: "cookie", Name: "composer_session", Description: "Session cookie set by POST /api/v1/auth/login"},
		"apiKeyAuth": {Type: "apiKey", In: "header", Name: "X-API-Key", Description: "API key created via POST /api/v1/keys"},
		"bearerAuth": {Type: "http", Scheme: "bearer", BearerFormat: "composer-api-key", Description: "API key passed as `Authorization: Bearer <key>`"},
	}
	config.OpenAPI.Security = []map[string][]string{
		{"cookieAuth": {}}, {"apiKeyAuth": {}}, {"bearerAuth": {}},
	}
	config.OpenAPI.Tags = []*huma.Tag{
		{Name: "system", Description: "Server info, version, and global configuration"},
		{Name: "auth", Description: "Login, logout, session, and bootstrap"},
		{Name: "users", Description: "User management (admin only)"},
		{Name: "keys", Description: "API key management (plaintext shown once at creation)"},
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
	}
	api := humachi.New(router, config)

	// Register the health check inline — it's defined directly in server.go
	// in the real server, not via a handler struct.
	huma.Register(api, huma.Operation{
		OperationID: "healthCheck",
		Method:      http.MethodGet,
		Path:        "/api/v1/system/health",
		Summary:     "Health check",
		Description: "Public liveness probe. Returns {status, version}. No authentication required.",
		Tags:        []string{"system"},
		Security:    []map[string][]string{},
	}, func(ctx context.Context, input *struct{}) (*dto.HealthCheckOutput, error) { return nil, nil })

	// Register every handler with nil deps. Huma only reflects on Input/Output
	// types at registration, so handler methods are never invoked here.
	handler.NewSystemHandler(nil, "").Register(api)
	handler.NewTemplateHandler().Register(api)
	handler.NewAuthHandler(nil).Register(api)
	handler.NewUserHandler(nil).Register(api)
	handler.NewKeyHandler(nil).Register(api)
	handler.NewStackHandler(nil, nil).Register(api)
	handler.NewContainerHandler(nil).Register(api)
	handler.NewSSEHandler(nil, nil).Register(api)
	handler.NewGitHandler(nil, nil).Register(api)
	handler.NewPipelineHandler(nil).Register(api)
	handler.NewWebhookCRUDHandler(nil).Register(api)
	handler.NewAuditHandler(nil).Register(api)
	handler.NewJobHandler(nil).Register(api)
	handler.NewResourceHandler(nil).Register(api)
	handler.NewDockerExecHandler(nil).Register(api)

	spec := api.OpenAPI()

	if *asYAML {
		data, err := yaml.Marshal(spec)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		os.Stdout.Write(data)
		return
	}

	data, err := json.MarshalIndent(spec, "", "  ")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	os.Stdout.Write(data)
}
