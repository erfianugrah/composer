package handler

import (
	"context"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/erfianugrah/composer/internal/api/dto"
	authmw "github.com/erfianugrah/composer/internal/api/middleware"
	"github.com/erfianugrah/composer/internal/app"
	"github.com/erfianugrah/composer/internal/domain/auth"
)

// KeyHandler registers API key management endpoints.
type KeyHandler struct {
	auth *app.AuthService
}

func NewKeyHandler(auth *app.AuthService) *KeyHandler {
	return &KeyHandler{auth: auth}
}

func (h *KeyHandler) Register(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "listKeys", Method: http.MethodGet,
		Path:        "/api/v1/keys",
		Summary:     "List API keys (redacted)",
		Description: "Returns every API key's ID, name, role, last-used and expiry timestamps. Plaintext values are never returned here — they're only shown once at creation.",
		Tags:        []string{"keys"},
		Errors:      errsViewer,
	}, h.List)

	huma.Register(api, huma.Operation{
		OperationID: "createKey", Method: http.MethodPost,
		Path:        "/api/v1/keys",
		Summary:     "Create API key (plaintext shown once)",
		Description: "Generates a new API key with the given name and role. The plaintext key is returned ONCE in the response body — save it immediately. Operators can only create keys at their role level or below.",
		Tags:        []string{"keys"},
		Errors:      errsOperatorMutation,
	}, h.Create)

	huma.Register(api, huma.Operation{
		OperationID: "getKey", Method: http.MethodGet,
		Path:        "/api/v1/keys/{id}",
		Summary:     "Get API key details",
		Description: "Returns metadata about a single API key (no plaintext).",
		Tags:        []string{"keys"},
		Errors:      errsViewerNotFound,
	}, h.Get)

	huma.Register(api, huma.Operation{
		OperationID: "deleteKey", Method: http.MethodDelete,
		Path:        "/api/v1/keys/{id}",
		Summary:     "Revoke API key",
		Description: "Permanently revokes the API key. Requests using the key will immediately start returning 401.",
		Tags:        []string{"keys"},
		Errors:      errsOperatorMutation,
	}, h.Delete)
}

func (h *KeyHandler) List(ctx context.Context, input *struct{}) (*dto.KeyListOutput, error) {
	if err := authmw.CheckRole(ctx, auth.RoleOperator); err != nil {
		return nil, err
	}

	keys, err := h.auth.ListAPIKeys(ctx)
	if err != nil {
		return nil, serverError(ctx, err)
	}

	out := &dto.KeyListOutput{}
	out.Body.Keys = make([]dto.KeySummary, 0, len(keys))
	for _, k := range keys {
		out.Body.Keys = append(out.Body.Keys, dto.KeySummary{
			ID: k.ID, Name: k.Name, Role: string(k.Role),
			LastUsedAt: k.LastUsedAt, ExpiresAt: k.ExpiresAt, CreatedAt: k.CreatedAt,
		})
	}
	return out, nil
}

func (h *KeyHandler) Get(ctx context.Context, input *dto.KeyIDInput) (*dto.KeyDetailOutput, error) {
	if err := authmw.CheckRole(ctx, auth.RoleOperator); err != nil {
		return nil, err
	}

	keys, err := h.auth.ListAPIKeys(ctx)
	if err != nil {
		return nil, serverError(ctx, err)
	}

	for _, k := range keys {
		if k.ID == input.ID {
			out := &dto.KeyDetailOutput{}
			out.Body.ID = k.ID
			out.Body.Name = k.Name
			out.Body.Role = string(k.Role)
			out.Body.LastUsedAt = k.LastUsedAt
			out.Body.ExpiresAt = k.ExpiresAt
			out.Body.CreatedAt = k.CreatedAt
			return out, nil
		}
	}

	return nil, huma.Error404NotFound("API key not found")
}

func (h *KeyHandler) Create(ctx context.Context, input *dto.CreateKeyInput) (*dto.KeyCreatedOutput, error) {
	if err := authmw.CheckRole(ctx, auth.RoleOperator); err != nil {
		return nil, err
	}

	role, err := auth.ParseRole(input.Body.Role)
	if err != nil {
		return nil, huma.Error422UnprocessableEntity(err.Error())
	}

	// Prevent privilege escalation: caller can only create keys at their level or below
	callerRole := authmw.RoleFromContext(ctx)
	if !callerRole.AtLeast(role) {
		return nil, huma.Error403Forbidden("cannot create API key with higher role than your own")
	}

	callerID := authmw.UserIDFromContext(ctx)

	var expiresAt *time.Time
	if input.Body.ExpiresAt != nil {
		t, err := time.Parse(time.RFC3339, *input.Body.ExpiresAt)
		if err != nil {
			return nil, huma.Error422UnprocessableEntity("invalid expires_at: must be RFC3339 format")
		}
		expiresAt = &t
	}

	result, err := h.auth.CreateAPIKey(ctx, input.Body.Name, role, callerID, expiresAt)
	if err != nil {
		return nil, serverError(ctx, err)
	}

	out := &dto.KeyCreatedOutput{}
	out.Body.ID = result.ID
	out.Body.Name = result.Name
	out.Body.Role = string(result.Role)
	out.Body.PlaintextKey = result.PlaintextKey
	out.Body.ExpiresAt = result.ExpiresAt
	out.Body.CreatedAt = result.CreatedAt
	return out, nil
}

func (h *KeyHandler) Delete(ctx context.Context, input *dto.KeyIDInput) (*struct{}, error) {
	if err := authmw.CheckRole(ctx, auth.RoleOperator); err != nil {
		return nil, err
	}

	if err := h.auth.DeleteAPIKey(ctx, input.ID); err != nil {
		return nil, serverError(ctx, err)
	}
	return nil, nil
}
