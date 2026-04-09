package handler

import (
	"context"
	"io"
	"net/http"
	"strings"

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
		OperationID: "containerLogs", Method: http.MethodGet,
		Path: "/api/v1/containers/{id}/logs", Summary: "Get container logs (snapshot, not streaming)", Tags: []string{"containers"},
	}, h.Logs)

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

// ContainerLogsInput adds tail/since query params to the container ID.
type ContainerLogsInput struct {
	ID    string `path:"id" doc:"Container ID"`
	Tail  string `query:"tail" default:"100" doc:"Number of lines from the end"`
	Since string `query:"since" default:"" doc:"Show logs since (e.g. 5m, 2h, or RFC3339 timestamp)"`
}

type ContainerLogsOutput struct {
	Body struct {
		Lines []string `json:"lines"`
	}
}

func (h *ContainerHandler) Logs(ctx context.Context, input *ContainerLogsInput) (*ContainerLogsOutput, error) {
	if err := authmw.CheckRole(ctx, auth.RoleViewer); err != nil {
		return nil, err
	}

	reader, err := h.docker.ContainerLogs(ctx, input.ID, false, input.Tail, input.Since)
	if err != nil {
		return nil, internalError()
	}
	defer reader.Close()

	data, err := io.ReadAll(io.LimitReader(reader, 1<<20)) // 1MB max
	if err != nil && err != io.EOF {
		return nil, internalError()
	}

	// Strip Docker multiplex headers (8-byte prefix per frame)
	raw := string(data)
	var lines []string
	for _, line := range strings.Split(raw, "\n") {
		// Docker stream header: first 8 bytes are type+size
		if len(line) > 8 {
			line = line[8:]
		}
		if strings.TrimSpace(line) != "" {
			lines = append(lines, line)
		}
	}

	out := &ContainerLogsOutput{}
	out.Body.Lines = lines
	return out, nil
}
