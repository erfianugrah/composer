package handler

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/erfianugrah/composer/internal/api/dto"
	authmw "github.com/erfianugrah/composer/internal/api/middleware"
	"github.com/erfianugrah/composer/internal/domain/auth"
	"github.com/erfianugrah/composer/internal/infra/docker"
)

// ContainerHandler registers container management endpoints.
type ContainerHandler struct {
	docker *docker.Client
}

func NewContainerHandler(docker *docker.Client) *ContainerHandler {
	return &ContainerHandler{docker: docker}
}

func (h *ContainerHandler) Register(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "listContainers", Method: http.MethodGet,
		Path: "/api/v1/containers", Summary: "List all containers", Tags: []string{"containers"},
	}, h.List)

	huma.Register(api, huma.Operation{
		OperationID: "getContainer", Method: http.MethodGet,
		Path: "/api/v1/containers/{id}", Summary: "Get container detail", Tags: []string{"containers"},
	}, h.Get)

	huma.Register(api, huma.Operation{
		OperationID: "startContainer", Method: http.MethodPost,
		Path: "/api/v1/containers/{id}/start", Summary: "Start container", Tags: []string{"containers"},
	}, h.Start)

	huma.Register(api, huma.Operation{
		OperationID: "stopContainer", Method: http.MethodPost,
		Path: "/api/v1/containers/{id}/stop", Summary: "Stop container", Tags: []string{"containers"},
	}, h.Stop)

	huma.Register(api, huma.Operation{
		OperationID: "restartContainer", Method: http.MethodPost,
		Path: "/api/v1/containers/{id}/restart", Summary: "Restart container", Tags: []string{"containers"},
	}, h.Restart)
}

func (h *ContainerHandler) List(ctx context.Context, input *struct{}) (*dto.ContainerListOutput, error) {
	if err := authmw.CheckRole(ctx, auth.RoleViewer); err != nil {
		return nil, err
	}

	containers, err := h.docker.ListContainers(ctx, "")
	if err != nil {
		return nil, internalError()
	}

	out := &dto.ContainerListOutput{}
	out.Body.Containers = make([]dto.ContainerOutput, 0, len(containers))
	for _, c := range containers {
		out.Body.Containers = append(out.Body.Containers, dto.ContainerOutput{
			ID: c.ID, Name: c.Name, ServiceName: c.ServiceName,
			Image: c.Image, Status: string(c.Status), Health: string(c.Health),
		})
	}
	return out, nil
}

func (h *ContainerHandler) Get(ctx context.Context, input *dto.ContainerIDInput) (*dto.ContainerDetailOutput, error) {
	if err := authmw.CheckRole(ctx, auth.RoleViewer); err != nil {
		return nil, err
	}

	c, err := h.docker.InspectContainer(ctx, input.ID)
	if err != nil {
		return nil, internalError()
	}

	out := &dto.ContainerDetailOutput{}
	out.Body = dto.ContainerOutput{
		ID: c.ID, Name: c.Name, ServiceName: c.ServiceName,
		Image: c.Image, Status: string(c.Status), Health: string(c.Health),
	}
	return out, nil
}

func (h *ContainerHandler) Start(ctx context.Context, input *dto.ContainerIDInput) (*struct{}, error) {
	if err := authmw.CheckRole(ctx, auth.RoleOperator); err != nil {
		return nil, err
	}
	if err := h.docker.StartContainer(ctx, input.ID); err != nil {
		return nil, internalError()
	}
	return nil, nil
}

func (h *ContainerHandler) Stop(ctx context.Context, input *dto.ContainerIDInput) (*struct{}, error) {
	if err := authmw.CheckRole(ctx, auth.RoleOperator); err != nil {
		return nil, err
	}
	if err := h.docker.StopContainer(ctx, input.ID); err != nil {
		return nil, internalError()
	}
	return nil, nil
}

func (h *ContainerHandler) Restart(ctx context.Context, input *dto.ContainerIDInput) (*struct{}, error) {
	if err := authmw.CheckRole(ctx, auth.RoleOperator); err != nil {
		return nil, err
	}
	if err := h.docker.RestartContainer(ctx, input.ID); err != nil {
		return nil, internalError()
	}
	return nil, nil
}
