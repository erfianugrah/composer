package handler

import (
	"context"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/erfianugrah/composer/internal/api/dto"
	"github.com/erfianugrah/composer/internal/app"
	"github.com/erfianugrah/composer/internal/domain/host"
)

// HostHandler registers /api/v1/hosts endpoints for docker host management.
// List/Get require viewer; Create/Update/Delete require admin.
type HostHandler struct {
	svc *app.HostService
}

func NewHostHandler(svc *app.HostService) *HostHandler {
	return &HostHandler{svc: svc}
}

func (h *HostHandler) Register(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "listHosts",
		Method:      http.MethodGet,
		Path:        "/api/v1/hosts",
		Summary:     "List docker hosts",
		Tags:        []string{"hosts"},
		Errors:      errsViewer,
	}, h.List)

	huma.Register(api, huma.Operation{
		OperationID: "createHost",
		Method:      http.MethodPost,
		Path:        "/api/v1/hosts",
		Summary:     "Create a docker host",
		Tags:        []string{"hosts"},
		Errors:      errsAdminMutation,
	}, h.Create)

	huma.Register(api, huma.Operation{
		OperationID: "getHost",
		Method:      http.MethodGet,
		Path:        "/api/v1/hosts/{id}",
		Summary:     "Get a docker host",
		Tags:        []string{"hosts"},
		Errors:      errsViewerNotFound,
	}, h.Get)

	huma.Register(api, huma.Operation{
		OperationID: "updateHost",
		Method:      http.MethodPut,
		Path:        "/api/v1/hosts/{id}",
		Summary:     "Update a docker host",
		Tags:        []string{"hosts"},
		Errors:      errsAdminMutation,
	}, h.Update)

	huma.Register(api, huma.Operation{
		OperationID: "deleteHost",
		Method:      http.MethodDelete,
		Path:        "/api/v1/hosts/{id}",
		Summary:     "Delete a docker host",
		Tags:        []string{"hosts"},
		Errors:      errsAdminMutation,
	}, h.Delete)
}

// --- Handler methods ---

func (h *HostHandler) List(ctx context.Context, _ *struct{}) (*dto.ListHostsOutput, error) {
	hosts, err := h.svc.List(ctx)
	if err != nil {
		return nil, serverError(ctx, err)
	}

	resp := &dto.ListHostsOutput{}
	resp.Body.Hosts = make([]dto.DockerHostOutput, len(hosts))
	for i, host := range hosts {
		resp.Body.Hosts[i] = toHostOutput(host)
	}
	return resp, nil
}

func (h *HostHandler) Create(ctx context.Context, input *dto.CreateHostInput) (*dto.CreateHostOutput, error) {
	host := &host.Host{
		Name:     input.Body.Name,
		Endpoint: input.Body.Endpoint,
		CertDir:  input.Body.CertDir,
	}
	if err := h.svc.Create(ctx, host); err != nil {
		return nil, huma.Error422UnprocessableEntity(err.Error())
	}

	resp := &dto.CreateHostOutput{}
	resp.Body.Host = toHostOutput(host)
	return resp, nil
}

func (h *HostHandler) Get(ctx context.Context, input *dto.HostIDInput) (*dto.GetHostOutput, error) {
	host, err := h.svc.Get(ctx, input.ID)
	if err != nil {
		return nil, serverError(ctx, err)
	}
	if host == nil {
		return nil, huma.Error404NotFound("host not found")
	}

	resp := &dto.GetHostOutput{}
	resp.Body.Host = toHostOutput(host)
	return resp, nil
}

func (h *HostHandler) Update(ctx context.Context, input *dto.UpdateHostInput) (*dto.GetHostOutput, error) {
	host, err := h.svc.Get(ctx, input.ID)
	if err != nil {
		return nil, serverError(ctx, err)
	}
	if host == nil {
		return nil, huma.Error404NotFound("host not found")
	}

	host.Name = input.Body.Name
	host.Endpoint = input.Body.Endpoint
	host.CertDir = input.Body.CertDir

	if err := h.svc.Update(ctx, host); err != nil {
		return nil, huma.Error422UnprocessableEntity(err.Error())
	}

	resp := &dto.GetHostOutput{}
	resp.Body.Host = toHostOutput(host)
	return resp, nil
}

func (h *HostHandler) Delete(ctx context.Context, input *dto.HostIDInput) (*struct{}, error) {
	_, err := h.svc.Get(ctx, input.ID)
	if err != nil {
		return nil, serverError(ctx, err)
	}

	if err := h.svc.Delete(ctx, input.ID); err != nil {
		return nil, huma.Error422UnprocessableEntity(err.Error())
	}
	return nil, nil
}

// --- Helpers ---

func toHostOutput(h *host.Host) dto.DockerHostOutput {
	return dto.DockerHostOutput{
		ID:        h.ID,
		Name:      h.Name,
		Endpoint:  h.Endpoint,
		CertDir:   h.CertDir,
		TLS:       h.CertDir != "",
		CreatedAt: h.CreatedAt.Format(time.RFC3339),
		UpdatedAt: h.UpdatedAt.Format(time.RFC3339),
	}
}
