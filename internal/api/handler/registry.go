package handler

import (
	"context"
	"errors"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/erfianugrah/composer/internal/api/dto"
	authmw "github.com/erfianugrah/composer/internal/api/middleware"
	"github.com/erfianugrah/composer/internal/app"
	"github.com/erfianugrah/composer/internal/domain/auth"
	"github.com/erfianugrah/composer/internal/domain/registry"
)

// RegistryHandler registers /api/v1/registries endpoints for Docker registry
// credentials. All mutations require admin; reads require viewer.
type RegistryHandler struct {
	svc *app.RegistryService
}

func NewRegistryHandler(svc *app.RegistryService) *RegistryHandler {
	return &RegistryHandler{svc: svc}
}

func (h *RegistryHandler) Register(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "listRegistryCredentials", Method: http.MethodGet,
		Path:        "/api/v1/registries",
		Summary:     "List Docker registry credentials",
		Description: "Returns global + per-stack registry credentials. Pass `?stack=<name>` to filter to per-stack rows for one stack. Secrets are redacted in every response.",
		Tags:        []string{"registries"},
		Errors:      errsViewer,
	}, h.List)

	huma.Register(api, huma.Operation{
		OperationID: "getRegistryCredential", Method: http.MethodGet,
		Path:        "/api/v1/registries/{id}",
		Summary:     "Get a registry credential",
		Tags:        []string{"registries"},
		Errors:      errsViewerNotFound,
	}, h.Get)

	huma.Register(api, huma.Operation{
		OperationID: "createRegistryCredential", Method: http.MethodPost,
		Path:        "/api/v1/registries",
		Summary:     "Create a Docker registry credential",
		Description: "Adds a registry auth entry. `stack_name` empty = global (applied to every stack). `stack_name` set = per-stack override (wins over global for the same registry). Supports multiple registries: add one row per registry — composer merges them into DOCKER_CONFIG before pull/up.",
		Tags:        []string{"registries"},
		Errors:      errsAdminMutation,
	}, h.Create)

	huma.Register(api, huma.Operation{
		OperationID: "updateRegistryCredential", Method: http.MethodPut,
		Path:        "/api/v1/registries/{id}",
		Summary:     "Update a registry credential",
		Description: "Replaces the credential atomically. Leave `secret` empty to keep the existing secret unchanged.",
		Tags:        []string{"registries"},
		Errors:      errsAdminMutation,
	}, h.Update)

	huma.Register(api, huma.Operation{
		OperationID: "deleteRegistryCredential", Method: http.MethodDelete,
		Path:        "/api/v1/registries/{id}",
		Summary:     "Delete a registry credential",
		Tags:        []string{"registries"},
		Errors:      errsAdminMutation,
	}, h.Delete)
}

func toRegistryOutput(c *registry.Credential) dto.RegistryCredentialOutput {
	preview := ""
	if n := len(c.Secret); n >= 4 {
		preview = "…" + c.Secret[n-4:]
	}
	return dto.RegistryCredentialOutput{
		ID:            c.ID,
		Registry:      c.Registry,
		Username:      c.Username,
		SecretSet:     c.Secret != "",
		SecretPreview: preview,
		Email:         c.Email,
		StackName:     c.StackName,
		IsGlobal:      c.IsGlobal(),
		CreatedAt:     c.CreatedAt,
		UpdatedAt:     c.UpdatedAt,
	}
}

func (h *RegistryHandler) List(ctx context.Context, input *dto.ListRegistryCredentialsInput) (*dto.ListRegistryCredentialsOutput, error) {
	if err := authmw.CheckRole(ctx, auth.RoleViewer); err != nil {
		return nil, err
	}
	var creds []*registry.Credential
	var err error
	if input.Stack != "" {
		creds, err = h.svc.ListForStack(ctx, input.Stack)
	} else {
		creds, err = h.svc.List(ctx)
	}
	if err != nil {
		return nil, serverError(ctx, err)
	}
	out := &dto.ListRegistryCredentialsOutput{}
	out.Body.Credentials = make([]dto.RegistryCredentialOutput, 0, len(creds))
	for _, c := range creds {
		out.Body.Credentials = append(out.Body.Credentials, toRegistryOutput(c))
	}
	return out, nil
}

func (h *RegistryHandler) Get(ctx context.Context, input *dto.GetRegistryCredentialInput) (*dto.GetRegistryCredentialOutput, error) {
	if err := authmw.CheckRole(ctx, auth.RoleViewer); err != nil {
		return nil, err
	}
	c, err := h.svc.Get(ctx, input.ID)
	if err != nil {
		if errors.Is(err, app.ErrNotFound) {
			return nil, huma.Error404NotFound("registry credential not found")
		}
		return nil, serverError(ctx, err)
	}
	return &dto.GetRegistryCredentialOutput{Body: toRegistryOutput(c)}, nil
}

func (h *RegistryHandler) Create(ctx context.Context, input *dto.CreateRegistryCredentialInput) (*dto.GetRegistryCredentialOutput, error) {
	if err := authmw.CheckRole(ctx, auth.RoleAdmin); err != nil {
		return nil, err
	}
	c := &registry.Credential{
		Registry:  input.Body.Registry,
		Username:  input.Body.Username,
		Secret:    input.Body.Secret,
		Email:     input.Body.Email,
		StackName: input.Body.StackName,
	}
	if err := h.svc.Upsert(ctx, c); err != nil {
		return nil, huma.Error422UnprocessableEntity(err.Error())
	}
	return &dto.GetRegistryCredentialOutput{Body: toRegistryOutput(c)}, nil
}

func (h *RegistryHandler) Update(ctx context.Context, input *dto.UpdateRegistryCredentialInput) (*dto.GetRegistryCredentialOutput, error) {
	if err := authmw.CheckRole(ctx, auth.RoleAdmin); err != nil {
		return nil, err
	}
	existing, err := h.svc.Get(ctx, input.ID)
	if err != nil {
		if errors.Is(err, app.ErrNotFound) {
			return nil, huma.Error404NotFound("registry credential not found")
		}
		return nil, serverError(ctx, err)
	}
	existing.Registry = input.Body.Registry
	existing.Username = input.Body.Username
	if input.Body.Secret != "" {
		existing.Secret = input.Body.Secret
	}
	existing.Email = input.Body.Email
	existing.StackName = input.Body.StackName
	if err := h.svc.Upsert(ctx, existing); err != nil {
		return nil, huma.Error422UnprocessableEntity(err.Error())
	}
	return &dto.GetRegistryCredentialOutput{Body: toRegistryOutput(existing)}, nil
}

func (h *RegistryHandler) Delete(ctx context.Context, input *dto.DeleteRegistryCredentialInput) (*struct{}, error) {
	if err := authmw.CheckRole(ctx, auth.RoleAdmin); err != nil {
		return nil, err
	}
	if err := h.svc.Delete(ctx, input.ID); err != nil {
		return nil, serverError(ctx, err)
	}
	return nil, nil
}
