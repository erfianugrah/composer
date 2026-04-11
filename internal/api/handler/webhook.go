package handler

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/erfianugrah/composer/internal/app"
	infraGit "github.com/erfianugrah/composer/internal/infra/git"
	"github.com/erfianugrah/composer/internal/infra/store"
)

// WebhookHandler handles inbound webhook deliveries.
type WebhookHandler struct {
	gitSvc      *app.GitService
	webhookRepo *store.WebhookRepo
	jobs        *app.JobManager
	pipelineSvc *app.PipelineService
}

func NewWebhookHandler(gitSvc *app.GitService, webhookRepo *store.WebhookRepo, jobs *app.JobManager, pipelineSvc *app.PipelineService) *WebhookHandler {
	return &WebhookHandler{gitSvc: gitSvc, webhookRepo: webhookRepo, jobs: jobs, pipelineSvc: pipelineSvc}
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

	// Create delivery record
	var dlvBuf [8]byte
	rand.Read(dlvBuf[:])
	dlvID := fmt.Sprintf("dlv_%s", hex.EncodeToString(dlvBuf[:]))
	delivery := &store.WebhookDelivery{
		ID: dlvID, WebhookID: webhookID, Event: payload.Event,
		Branch: payload.Branch, CommitSHA: payload.CommitSHA, Status: "received",
	}
	h.webhookRepo.CreateDelivery(r.Context(), delivery)

	// Branch filter
	if webhook.BranchFilter != "" && payload.Branch != webhook.BranchFilter {
		h.webhookRepo.UpdateDeliveryStatus(r.Context(), dlvID, "skipped", "branch_mismatch", "")
		jsonResponse(w, http.StatusOK, map[string]string{
			"status": "skipped", "reason": "branch filter mismatch",
		})
		return
	}

	// GitOps: sync + redeploy (runs async so we don't timeout waiting for deploy)
	h.webhookRepo.UpdateDeliveryStatus(r.Context(), dlvID, "processing", "", "")

	var jobID string
	if h.jobs != nil {
		job := h.jobs.Create("sync_redeploy", webhook.StackName)
		jobID = job.ID
		h.jobs.Start(job.ID)
	}

	stackName := webhook.StackName
	go func() {
		// P17: 10-minute timeout prevents goroutine from running forever
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()
		action, err := h.gitSvc.SyncAndRedeploy(ctx, stackName)
		if err != nil {
			h.webhookRepo.UpdateDeliveryStatus(ctx, dlvID, "failed", "", err.Error())
			if h.jobs != nil && jobID != "" {
				h.jobs.Fail(jobID, err.Error())
			}
			return
		}
		h.webhookRepo.UpdateDeliveryStatus(ctx, dlvID, "success", action, "")
		if h.jobs != nil && jobID != "" {
			h.jobs.Complete(jobID, action, "")
		}
	}()

	// Dispatch any pipelines triggered by this webhook
	if h.pipelineSvc != nil {
		go h.pipelineSvc.RunByWebhookTrigger(context.Background(), stackName, payload.Branch)
	}

	resp := map[string]string{
		"status": "accepted", "stack": stackName, "delivery_id": dlvID,
	}
	if jobID != "" {
		resp["job_id"] = jobID
	}
	jsonResponse(w, http.StatusOK, resp)
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
