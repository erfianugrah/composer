package handler

import (
	"context"
	"errors"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/erfianugrah/composer/internal/api/dto"
	authmw "github.com/erfianugrah/composer/internal/api/middleware"
	"github.com/erfianugrah/composer/internal/app/selfupgrade"
	"github.com/erfianugrah/composer/internal/domain/auth"
	"github.com/erfianugrah/composer/internal/infra/store"
)

// UpgradeHandler registers self-upgrade API endpoints.
type UpgradeHandler struct {
	svc *selfupgrade.UpgradeService
}

// NewUpgradeHandler creates an UpgradeHandler.
func NewUpgradeHandler(svc *selfupgrade.UpgradeService) *UpgradeHandler {
	return &UpgradeHandler{svc: svc}
}

// Register registers upgrade endpoints on the Huma API.
func (h *UpgradeHandler) Register(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID:   "requestUpgrade",
		Method:        http.MethodPost,
		Path:          "/api/v1/system/upgrade",
		Summary:       "Request a self-upgrade",
		Description:   "Launches a detached helper container that upgrades composer to the given image. Admin only. Returns 202 with helper details; polls GET /api/v1/system/upgrade/status for progress.",
		Tags:          []string{"system"},
		DefaultStatus: http.StatusAccepted,
		Errors:        errsAdminMutation,
	}, h.RequestUpgrade)

	huma.Register(api, huma.Operation{
		OperationID: "upgradeStatus",
		Method:      http.MethodGet,
		Path:        "/api/v1/system/upgrade/status",
		Summary:     "Get upgrade status",
		Description: "Returns the current self-upgrade status. Public (no authentication required).",
		Tags:        []string{"system"},
		Security:    []map[string][]string{}, // public
	}, h.Status)
}

// RequestUpgrade handles POST /api/v1/system/upgrade.
func (h *UpgradeHandler) RequestUpgrade(ctx context.Context, input *dto.RequestUpgradeInput) (*dto.RequestUpgradeOutput, error) {
	if err := authmw.CheckRole(ctx, auth.RoleAdmin); err != nil {
		return nil, err
	}

	row, err := h.svc.Request(ctx, input.Body.Image, authmw.UserIDFromContext(ctx))
	if err != nil {
		if errors.Is(err, store.ErrUpgradeInFlight) {
			return nil, huma.Error409Conflict("an upgrade is already in progress")
		}
		if errors.Is(err, selfupgrade.ErrInvalidImage) {
			return nil, huma.Error422UnprocessableEntity(err.Error())
		}
		return nil, serverError(ctx, err)
	}

	out := &dto.RequestUpgradeOutput{}
	out.Body.HelperID = row.HelperID
	out.Body.FromVersion = row.FromVersion
	out.Body.TargetImage = row.TargetImage
	out.Body.DeploymentType = row.DeploymentType
	out.Body.StatusURL = "/api/v1/system/upgrade/status"
	return out, nil
}

// Status handles GET /api/v1/system/upgrade/status.
func (h *UpgradeHandler) Status(ctx context.Context, input *struct{}) (*dto.UpgradeStatusOutput, error) {
	row, err := h.svc.Status(ctx)
	if err != nil {
		return nil, serverError(ctx, err)
	}

	out := &dto.UpgradeStatusOutput{}
	if row != nil {
		out.Body.Status = row.Status
		out.Body.HelperID = row.HelperID
		out.Body.FromVersion = row.FromVersion
		out.Body.TargetImage = row.TargetImage
		out.Body.DeploymentType = row.DeploymentType
		out.Body.ErrorMessage = row.ErrorMessage
		out.Body.StartedBy = row.StartedBy
		out.Body.CreatedAt = row.CreatedAt
		out.Body.UpdatedAt = row.UpdatedAt
	} else {
		// No upgrade has ever been requested.
		out.Body.Status = "completed"
		out.Body.DeploymentType = "unknown"
	}
	return out, nil
}
