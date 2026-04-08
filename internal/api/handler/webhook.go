package handler

import (
	"io"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/erfianugrah/composer/internal/app"
	infraGit "github.com/erfianugrah/composer/internal/infra/git"
	"github.com/erfianugrah/composer/internal/infra/store/postgres"
)

// WebhookHandler handles inbound webhook deliveries.
// Registered as a raw chi handler (not huma) since it needs raw body access.
type WebhookHandler struct {
	gitSvc      *app.GitService
	webhookRepo *postgres.WebhookRepo
}

func NewWebhookHandler(gitSvc *app.GitService, webhookRepo *postgres.WebhookRepo) *WebhookHandler {
	return &WebhookHandler{gitSvc: gitSvc, webhookRepo: webhookRepo}
}

// RegisterRaw registers the webhook receiver as a raw chi route.
// This is NOT a huma endpoint -- it needs raw body + headers for signature validation.
func (h *WebhookHandler) RegisterRaw(router chi.Router) {
	router.Post("/api/v1/hooks/{id}", h.Receive)
}

// Receive handles an inbound webhook delivery.
func (h *WebhookHandler) Receive(w http.ResponseWriter, r *http.Request) {
	webhookID := r.PathValue("id")
	if webhookID == "" {
		http.Error(w, `{"status":400,"detail":"webhook ID required"}`, http.StatusBadRequest)
		return
	}

	// Read body
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20)) // 1MB max
	if err != nil {
		http.Error(w, `{"status":400,"detail":"failed to read body"}`, http.StatusBadRequest)
		return
	}

	// Look up webhook config from DB
	if h.webhookRepo == nil {
		http.Error(w, `{"status":404,"detail":"webhook not found"}`, http.StatusNotFound)
		return
	}

	webhook, err := h.webhookRepo.GetByID(r.Context(), webhookID)
	if err != nil || webhook == nil {
		http.Error(w, `{"status":404,"detail":"webhook not found"}`, http.StatusNotFound)
		return
	}

	// Extract headers (lowercase for consistent matching)
	headers := make(map[string]string)
	for key := range r.Header {
		headers[strings.ToLower(key)] = r.Header.Get(key)
	}

	// Validate signature
	provider := infraGit.WebhookProvider(webhook.Provider)
	if !infraGit.ValidateSignature(provider, webhook.Secret, headers, body) {
		http.Error(w, `{"status":401,"detail":"invalid signature"}`, http.StatusUnauthorized)
		return
	}

	// Parse payload
	payload, err := infraGit.ParsePayload(provider, headers, body)
	if err != nil {
		http.Error(w, `{"status":400,"detail":"failed to parse payload"}`, http.StatusBadRequest)
		return
	}

	// Check branch filter
	if webhook.BranchFilter != "" && payload.Branch != webhook.BranchFilter {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"skipped","reason":"branch filter mismatch"}`))
		return
	}

	// Trigger GitOps sync + redeploy
	action, err := h.gitSvc.SyncAndRedeploy(r.Context(), webhook.StackName)
	if err != nil {
		http.Error(w, `{"status":500,"detail":"`+err.Error()+`"}`, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status":"` + action + `","stack":"` + webhook.StackName + `"}`))
}
