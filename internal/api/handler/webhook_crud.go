package handler

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/http"
	"time"

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
		Path: "/api/v1/webhooks", Summary: "List all webhooks", Tags: []string{"webhooks"},
	}, h.List)

	huma.Register(api, huma.Operation{
		OperationID: "createWebhook", Method: http.MethodPost,
		Path: "/api/v1/webhooks", Summary: "Create a webhook for a stack", Tags: []string{"webhooks"},
	}, h.Create)

	huma.Register(api, huma.Operation{
		OperationID: "getWebhook", Method: http.MethodGet,
		Path: "/api/v1/webhooks/{id}", Summary: "Get webhook details", Tags: []string{"webhooks"},
	}, h.Get)

	huma.Register(api, huma.Operation{
		OperationID: "updateWebhook", Method: http.MethodPut,
		Path: "/api/v1/webhooks/{id}", Summary: "Update webhook settings", Tags: []string{"webhooks"},
	}, h.Update)

	huma.Register(api, huma.Operation{
		OperationID: "deleteWebhook", Method: http.MethodDelete,
		Path: "/api/v1/webhooks/{id}", Summary: "Delete a webhook", Tags: []string{"webhooks"},
	}, h.Delete)

	huma.Register(api, huma.Operation{
		OperationID: "listWebhookDeliveries", Method: http.MethodGet,
		Path: "/api/v1/webhooks/{id}/deliveries", Summary: "List webhook delivery history", Tags: []string{"webhooks"},
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
		return nil, internalError()
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

func (h *WebhookCRUDHandler) Create(ctx context.Context, input *dto.CreateWebhookInput) (*dto.WebhookOutput, error) {
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
		return nil, internalError()
	}

	out := &dto.WebhookOutput{}
	out.Body.ID = id
	out.Body.StackName = webhook.StackName
	out.Body.Provider = webhook.Provider
	out.Body.Secret = secret
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
		return nil, internalError()
	}
	if w == nil {
		return nil, huma.Error404NotFound("webhook not found")
	}

	out := &dto.WebhookOutput{}
	out.Body.ID = w.ID
	out.Body.StackName = w.StackName
	out.Body.Provider = w.Provider
	out.Body.Secret = w.Secret
	out.Body.URL = fmt.Sprintf("/api/v1/hooks/%s", w.ID)
	out.Body.BranchFilter = w.BranchFilter
	out.Body.AutoRedeploy = w.AutoRedeploy
	return out, nil
}

type UpdateWebhookInput struct {
	ID   string `path:"id" doc:"Webhook ID"`
	Body struct {
		BranchFilter string `json:"branch_filter,omitempty" doc:"Branch filter"`
		AutoRedeploy *bool  `json:"auto_redeploy,omitempty" doc:"Auto-redeploy on push"`
		Provider     string `json:"provider,omitempty" doc:"Webhook provider"`
	}
}

func (h *WebhookCRUDHandler) Update(ctx context.Context, input *UpdateWebhookInput) (*dto.WebhookOutput, error) {
	if err := authmw.CheckRole(ctx, auth.RoleOperator); err != nil {
		return nil, err
	}

	w, err := h.webhooks.GetByID(ctx, input.ID)
	if err != nil {
		return nil, internalError()
	}
	if w == nil {
		return nil, huma.Error404NotFound("webhook not found")
	}

	if input.Body.BranchFilter != "" {
		w.BranchFilter = input.Body.BranchFilter
	}
	if input.Body.AutoRedeploy != nil {
		w.AutoRedeploy = *input.Body.AutoRedeploy
	}
	if input.Body.Provider != "" {
		w.Provider = input.Body.Provider
	}

	if err := h.webhooks.Update(ctx, w); err != nil {
		return nil, internalError()
	}

	out := &dto.WebhookOutput{}
	out.Body.ID = w.ID
	out.Body.StackName = w.StackName
	out.Body.Provider = w.Provider
	out.Body.Secret = w.Secret
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
		return nil, internalError()
	}
	return nil, nil
}

type DeliveryListOutput struct {
	Body struct {
		Deliveries []DeliverySummary `json:"deliveries"`
	}
}

type DeliverySummary struct {
	ID        string `json:"id"`
	Event     string `json:"event"`
	Branch    string `json:"branch"`
	CommitSHA string `json:"commit_sha"`
	Status    string `json:"status"`
	Action    string `json:"action"`
	Error     string `json:"error,omitempty"`
	CreatedAt string `json:"created_at"`
}

func (h *WebhookCRUDHandler) ListDeliveries(ctx context.Context, input *dto.WebhookIDInput) (*DeliveryListOutput, error) {
	if err := authmw.CheckRole(ctx, auth.RoleOperator); err != nil {
		return nil, err
	}

	deliveries, err := h.webhooks.ListDeliveries(ctx, input.ID, 50)
	if err != nil {
		return nil, internalError()
	}

	out := &DeliveryListOutput{}
	out.Body.Deliveries = make([]DeliverySummary, 0, len(deliveries))
	for _, d := range deliveries {
		out.Body.Deliveries = append(out.Body.Deliveries, DeliverySummary{
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

// Ensure time import is used (for future audit log timestamps)
var _ = time.Now
