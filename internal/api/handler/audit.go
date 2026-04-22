package handler

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/erfianugrah/composer/internal/api/dto"
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
		Path:        "/api/v1/audit",
		Summary:     "List recent audit log entries",
		Description: "Returns the most recent audit entries, newest first. Use `action`/`stack` to filter. Entries older than 30 days are automatically pruned. Admin only.",
		Tags:        []string{"audit"},
		Errors:      errsAdminMutation,
	}, h.List)
}

func (h *AuditHandler) List(ctx context.Context, input *dto.AuditListInput) (*dto.AuditListOutput, error) {
	if err := authmw.CheckRole(ctx, auth.RoleAdmin); err != nil {
		return nil, err
	}

	entries, err := h.repo.RecentFiltered(ctx, input.Limit, input.Action, input.Stack)
	if err != nil {
		return nil, serverError(ctx, err)
	}

	out := &dto.AuditListOutput{}
	out.Body.Entries = make([]dto.AuditEntryDTO, 0, len(entries))
	for _, e := range entries {
		out.Body.Entries = append(out.Body.Entries, dto.AuditEntryDTO{
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
