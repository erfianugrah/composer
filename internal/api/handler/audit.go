package handler

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	authmw "github.com/erfianugrah/composer/internal/api/middleware"
	"github.com/erfianugrah/composer/internal/domain/auth"
	"github.com/erfianugrah/composer/internal/infra/store"
)

// AuditHandler exposes the audit log.
type AuditHandler struct {
	repo *store.AuditRepo
}

func NewAuditHandler(repo *store.AuditRepo) *AuditHandler {
	return &AuditHandler{repo: repo}
}

func (h *AuditHandler) Register(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "listAuditLog", Method: http.MethodGet,
		Path: "/api/v1/audit", Summary: "List recent audit log entries", Tags: []string{"audit"},
	}, h.List)
}

type AuditListInput struct {
	Limit  int    `query:"limit" default:"50" minimum:"1" maximum:"200" doc:"Max entries to return"`
	Action string `query:"action" doc:"Filter by action (e.g. stack.deploy, stack.up)"`
	Stack  string `query:"stack" doc:"Filter by stack name (matches in resource path)"`
}

type AuditEntryDTO struct {
	ID        string         `json:"id"`
	UserID    string         `json:"user_id"`
	Action    string         `json:"action"`
	Resource  string         `json:"resource"`
	Detail    map[string]any `json:"detail,omitempty"`
	IPAddress string         `json:"ip_address"`
	CreatedAt string         `json:"created_at"`
}

type AuditListOutput struct {
	Body struct {
		Entries []AuditEntryDTO `json:"entries"`
	}
}

func (h *AuditHandler) List(ctx context.Context, input *AuditListInput) (*AuditListOutput, error) {
	if err := authmw.CheckRole(ctx, auth.RoleAdmin); err != nil {
		return nil, err
	}

	entries, err := h.repo.RecentFiltered(ctx, input.Limit, input.Action, input.Stack)
	if err != nil {
		return nil, serverError(err)
	}

	out := &AuditListOutput{}
	out.Body.Entries = make([]AuditEntryDTO, 0, len(entries))
	for _, e := range entries {
		out.Body.Entries = append(out.Body.Entries, AuditEntryDTO{
			ID:        e.ID,
			UserID:    e.UserID,
			Action:    e.Action,
			Resource:  e.Resource,
			Detail:    e.Detail,
			IPAddress: e.IPAddress,
			CreatedAt: e.CreatedAt.Format("2006-01-02T15:04:05Z"),
		})
	}
	return out, nil
}
