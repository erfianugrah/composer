package handler

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/erfianugrah/composer/internal/api/dto"
	authmw "github.com/erfianugrah/composer/internal/api/middleware"
	"github.com/erfianugrah/composer/internal/domain/auth"
	"github.com/erfianugrah/composer/internal/infra/store"
)

// WebhookCRUDHandler manages webhook configurations.
type WebhookCRUDHandler struct {
	webhooks *store.WebhookRepo
}

func NewWebhookCRUDHandler(webhooks *store.WebhookRepo) *WebhookCRUDHandler {
	return &WebhookCRUDHandler{webhooks: webhooks}
}

func (h *WebhookCRUDHandler) Register(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "listWebhooks", Method: http.MethodGet,
		Path:        "/api/v1/webhooks",
		Summary:     "List all webhooks",
		Description: "Returns every configured webhook (secret redacted). Operator+.",
		Tags:        []string{"webhooks"},
		Errors:      errsViewer,
	}, h.List)

	huma.Register(api, huma.Operation{
		OperationID: "createWebhook", Method: http.MethodPost,
		Path:        "/api/v1/webhooks",
		Summary:     "Create a webhook for a stack",
		Description: "Creates a webhook receiver URL and HMAC secret for a stack. The plaintext secret is returned ONCE and must be configured in your git provider; subsequent reads return the redacted form.",
		Tags:        []string{"webhooks"},
		Errors:      errsOperatorMutation,
	}, h.Create)

	huma.Register(api, huma.Operation{
		OperationID: "getWebhook", Method: http.MethodGet,
		Path:        "/api/v1/webhooks/{id}",
		Summary:     "Get webhook details",
		Description: "Returns webhook metadata with a redacted secret (last 4 chars only).",
		Tags:        []string{"webhooks"},
		Errors:      errsViewerNotFound,
	}, h.Get)

	huma.Register(api, huma.Operation{
		OperationID: "updateWebhook", Method: http.MethodPut,
		Path:        "/api/v1/webhooks/{id}",
		Summary:     "Update webhook settings",
		Description: "Updates branch filter, auto-redeploy flag, and/or provider. Use pointer fields to distinguish 'keep current' (omit) from 'clear' (empty string).",
		Tags:        []string{"webhooks"},
		Errors:      errsOperatorMutation,
	}, h.Update)

	huma.Register(api, huma.Operation{
		OperationID: "deleteWebhook", Method: http.MethodDelete,
		Path:        "/api/v1/webhooks/{id}",
		Summary:     "Delete a webhook",
		Description: "Removes the webhook. Subsequent deliveries to its URL will 404.",
		Tags:        []string{"webhooks"},
		Errors:      errsOperatorMutation,
	}, h.Delete)

	huma.Register(api, huma.Operation{
		OperationID: "listWebhookDeliveries", Method: http.MethodGet,
		Path:        "/api/v1/webhooks/{id}/deliveries",
		Summary:     "List webhook delivery history",
		Description: "Returns up to 50 recent deliveries for this webhook (status, event, branch, commit, action taken). Older entries are pruned after 30 days.",
		Tags:        []string{"webhooks"},
		Errors:      errsViewerNotFound,
	}, h.ListDeliveries)
}

func (h *WebhookCRUDHandler) List(ctx context.Context, input *struct{}) (*dto.WebhookListOutput, error) {
	if err := authmw.CheckRole(ctx, auth.RoleOperator); err != nil {
		return nil, err
	}

	// List all webhooks (no stack filter for admin view)
	// For now, return all -- could add stack filter query param later
	all, err := h.webhooks.ListAll(ctx)
	if err != nil {
		return nil, serverError(ctx, err)
	}

	out := &dto.WebhookListOutput{}
	out.Body.Webhooks = make([]dto.WebhookSummary, 0, len(all))
	for _, w := range all {
		out.Body.Webhooks = append(out.Body.Webhooks, dto.WebhookSummary{
			ID: w.ID, StackName: w.StackName, Provider: w.Provider,
			BranchFilter: w.BranchFilter, AutoRedeploy: w.AutoRedeploy,
			URL: fmt.Sprintf("/api/v1/hooks/%s", w.ID),
		})
	}
	return out, nil
}

func (h *WebhookCRUDHandler) Create(ctx context.Context, input *dto.CreateWebhookInput) (*dto.WebhookCreatedOutput, error) {
	if err := authmw.CheckRole(ctx, auth.RoleOperator); err != nil {
		return nil, err
	}

	// Generate webhook ID and secret
	id := generateWebhookID()
	secret := generateWebhookSecret()
	callerID := authmw.UserIDFromContext(ctx)

	webhook := &store.Webhook{
		ID:           id,
		StackName:    input.Body.StackName,
		Provider:     input.Body.Provider,
		Secret:       secret,
		BranchFilter: input.Body.BranchFilter,
		AutoRedeploy: input.Body.AutoRedeploy,
		CreatedBy:    callerID,
	}

	if err := h.webhooks.Create(ctx, webhook); err != nil {
		return nil, serverError(ctx, err)
	}

	out := &dto.WebhookCreatedOutput{}
	out.Body.ID = id
	out.Body.StackName = webhook.StackName
	out.Body.Provider = webhook.Provider
	out.Body.Secret = secret // plaintext — shown only on creation
	out.Body.URL = fmt.Sprintf("/api/v1/hooks/%s", id)
	out.Body.BranchFilter = webhook.BranchFilter
	out.Body.AutoRedeploy = webhook.AutoRedeploy
	return out, nil
}

func (h *WebhookCRUDHandler) Get(ctx context.Context, input *dto.WebhookIDInput) (*dto.WebhookOutput, error) {
	if err := authmw.CheckRole(ctx, auth.RoleOperator); err != nil {
		return nil, err
	}

	w, err := h.webhooks.GetByID(ctx, input.ID)
	if err != nil {
		return nil, serverError(ctx, err)
	}
	if w == nil {
		return nil, huma.Error404NotFound("webhook not found")
	}

	out := &dto.WebhookOutput{}
	out.Body.ID = w.ID
	out.Body.StackName = w.StackName
	out.Body.Provider = w.Provider
	if len(w.Secret) >= 4 {
		out.Body.Secret = "****" + w.Secret[len(w.Secret)-4:]
	} else {
		out.Body.Secret = "****"
	}
	out.Body.URL = fmt.Sprintf("/api/v1/hooks/%s", w.ID)
	out.Body.BranchFilter = w.BranchFilter
	out.Body.AutoRedeploy = w.AutoRedeploy
	return out, nil
}

func (h *WebhookCRUDHandler) Update(ctx context.Context, input *dto.UpdateWebhookInput) (*dto.WebhookOutput, error) {
	if err := authmw.CheckRole(ctx, auth.RoleOperator); err != nil {
		return nil, err
	}

	w, err := h.webhooks.GetByID(ctx, input.ID)
	if err != nil {
		return nil, serverError(ctx, err)
	}
	if w == nil {
		return nil, huma.Error404NotFound("webhook not found")
	}

	// Pointer semantics: nil = keep existing, non-nil = replace (including "" to clear)
	if input.Body.BranchFilter != nil {
		w.BranchFilter = *input.Body.BranchFilter
	}
	if input.Body.AutoRedeploy != nil {
		w.AutoRedeploy = *input.Body.AutoRedeploy
	}
	if input.Body.Provider != nil {
		w.Provider = *input.Body.Provider
	}

	if err := h.webhooks.Update(ctx, w); err != nil {
		return nil, serverError(ctx, err)
	}

	out := &dto.WebhookOutput{}
	out.Body.ID = w.ID
	out.Body.StackName = w.StackName
	out.Body.Provider = w.Provider
	if len(w.Secret) >= 4 {
		out.Body.Secret = "****" + w.Secret[len(w.Secret)-4:]
	} else {
		out.Body.Secret = "****"
	}
	out.Body.URL = fmt.Sprintf("/api/v1/hooks/%s", w.ID)
	out.Body.BranchFilter = w.BranchFilter
	out.Body.AutoRedeploy = w.AutoRedeploy
	return out, nil
}

func (h *WebhookCRUDHandler) Delete(ctx context.Context, input *dto.WebhookIDInput) (*struct{}, error) {
	if err := authmw.CheckRole(ctx, auth.RoleOperator); err != nil {
		return nil, err
	}

	if err := h.webhooks.Delete(ctx, input.ID); err != nil {
		return nil, serverError(ctx, err)
	}
	return nil, nil
}

func (h *WebhookCRUDHandler) ListDeliveries(ctx context.Context, input *dto.WebhookIDInput) (*dto.DeliveryListOutput, error) {
	if err := authmw.CheckRole(ctx, auth.RoleOperator); err != nil {
		return nil, err
	}

	deliveries, err := h.webhooks.ListDeliveries(ctx, input.ID, 50)
	if err != nil {
		return nil, serverError(ctx, err)
	}

	out := &dto.DeliveryListOutput{}
	out.Body.Deliveries = make([]dto.DeliverySummary, 0, len(deliveries))
	for _, d := range deliveries {
		out.Body.Deliveries = append(out.Body.Deliveries, dto.DeliverySummary{
			ID: d.ID, Event: d.Event, Branch: d.Branch, CommitSHA: d.CommitSHA,
			Status: d.Status, Action: d.Action, Error: d.Error, CreatedAt: d.CreatedAt,
		})
	}
	return out, nil
}

func generateWebhookID() string {
	b := make([]byte, 16)
	rand.Read(b)
	return fmt.Sprintf("wh_%s", hex.EncodeToString(b))
}

func generateWebhookSecret() string {
	b := make([]byte, 32)
	rand.Read(b)
	return hex.EncodeToString(b)
}
