package handler

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/erfianugrah/composer/internal/app"
	infraGit "github.com/erfianugrah/composer/internal/infra/git"
	"github.com/erfianugrah/composer/internal/infra/store"
)

// WebhookHandler handles inbound webhook deliveries.
type WebhookHandler struct {
	gitSvc      *app.GitService
	webhookRepo *store.WebhookRepo
}

func NewWebhookHandler(gitSvc *app.GitService, webhookRepo *store.WebhookRepo) *WebhookHandler {
	return &WebhookHandler{gitSvc: gitSvc, webhookRepo: webhookRepo}
}

func (h *WebhookHandler) RegisterRaw(router chi.Router) {
	router.Post("/api/v1/hooks/{id}", h.Receive)
}

func (h *WebhookHandler) Receive(w http.ResponseWriter, r *http.Request) {
	webhookID := r.PathValue("id")
	if webhookID == "" {
		jsonError(w, http.StatusBadRequest, "webhook ID required")
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		jsonError(w, http.StatusBadRequest, "failed to read body")
		return
	}

	if h.webhookRepo == nil {
		jsonError(w, http.StatusNotFound, "webhook not found")
		return
	}

	webhook, err := h.webhookRepo.GetByID(r.Context(), webhookID)
	if err != nil || webhook == nil {
		jsonError(w, http.StatusNotFound, "webhook not found")
		return
	}

	// Extract headers (lowercase)
	headers := make(map[string]string)
	for key := range r.Header {
		headers[strings.ToLower(key)] = r.Header.Get(key)
	}

	// Validate signature
	provider := infraGit.WebhookProvider(webhook.Provider)
	if !infraGit.ValidateSignature(provider, webhook.Secret, headers, body) {
		jsonError(w, http.StatusUnauthorized, "invalid signature")
		return
	}

	// Parse payload
	payload, err := infraGit.ParsePayload(provider, headers, body)
	if err != nil {
		jsonError(w, http.StatusBadRequest, "failed to parse payload")
		return
	}

	// Branch filter
	if webhook.BranchFilter != "" && payload.Branch != webhook.BranchFilter {
		jsonResponse(w, http.StatusOK, map[string]string{
			"status": "skipped", "reason": "branch filter mismatch",
		})
		return
	}

	// GitOps: sync + redeploy
	action, err := h.gitSvc.SyncAndRedeploy(r.Context(), webhook.StackName)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "sync failed")
		return
	}

	jsonResponse(w, http.StatusOK, map[string]string{
		"status": action, "stack": webhook.StackName,
	})
}

func jsonError(w http.ResponseWriter, status int, detail string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]any{
		"status": status,
		"detail": detail,
	})
}

func jsonResponse(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}
