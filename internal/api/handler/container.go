package handler

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/danielgtaylor/huma/v2"

	"github.com/docker/docker/pkg/stdcopy"
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
		Path:        "/api/v1/containers",
		Summary:     "List all containers",
		Description: "Returns every container visible to the Docker daemon, including non-compose ones. For stack-scoped views call `getStack` instead.",
		Tags:        []string{"containers"},
		Errors:      errsViewer,
	}, h.List)

	huma.Register(api, huma.Operation{
		OperationID: "getContainer", Method: http.MethodGet,
		Path:        "/api/v1/containers/{id}",
		Summary:     "Get container detail",
		Description: "Returns a single container's state, image, service, health, and restart policy.",
		Tags:        []string{"containers"},
		Errors:      errsViewerNotFound,
	}, h.Get)

	huma.Register(api, huma.Operation{
		OperationID: "containerLogs", Method: http.MethodGet,
		Path:        "/api/v1/containers/{id}/logs",
		Summary:     "Get container logs (snapshot)",
		Description: "Returns a one-shot log snapshot. For live streaming use the SSE endpoint at `/api/v1/sse/containers/{id}/logs`. Output is capped at 1MB.",
		Tags:        []string{"containers"},
		Errors:      errsViewerNotFound,
	}, h.Logs)

	huma.Register(api, huma.Operation{
		OperationID: "startContainer", Method: http.MethodPost,
		Path:        "/api/v1/containers/{id}/start",
		Summary:     "Start container",
		Description: "Starts a stopped container. Restricted to compose-managed containers (has `com.docker.compose.project` label) to avoid operating on infrastructure containers.",
		Tags:        []string{"containers"},
		Errors:      errsOperatorMutation,
	}, h.Start)

	huma.Register(api, huma.Operation{
		OperationID: "stopContainer", Method: http.MethodPost,
		Path:        "/api/v1/containers/{id}/stop",
		Summary:     "Stop container",
		Description: "Gracefully stops a running container (SIGTERM then SIGKILL after 10s). Compose-managed only.",
		Tags:        []string{"containers"},
		Errors:      errsOperatorMutation,
	}, h.Stop)

	huma.Register(api, huma.Operation{
		OperationID: "restartContainer", Method: http.MethodPost,
		Path:        "/api/v1/containers/{id}/restart",
		Summary:     "Restart container",
		Description: "Stops then starts a container in one call. Compose-managed only.",
		Tags:        []string{"containers"},
		Errors:      errsOperatorMutation,
	}, h.Restart)
}

func (h *ContainerHandler) List(ctx context.Context, input *struct{}) (*dto.ContainerListOutput, error) {
	if err := authmw.CheckRole(ctx, auth.RoleViewer); err != nil {
		return nil, err
	}

	containers, err := h.docker.ListContainers(ctx, "")
	if err != nil {
		return nil, serverError(ctx, err)
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
		return nil, serverError(ctx, err)
	}

	out := &dto.ContainerDetailOutput{}
	out.Body = dto.ContainerOutput{
		ID: c.ID, Name: c.Name, ServiceName: c.ServiceName,
		Image: c.Image, Status: string(c.Status), Health: string(c.Health),
	}
	return out, nil
}

// validateContainerScope checks that a container is managed by Docker Compose
// (has the com.docker.compose.project label). Prevents operating on infrastructure
// containers like Composer itself, Postgres, Valkey, etc.
func (h *ContainerHandler) validateScope(ctx context.Context, id string) error {
	c, err := h.docker.InspectContainer(ctx, id)
	if err != nil {
		return fmt.Errorf("container not found")
	}
	if c.ServiceName == "" {
		return fmt.Errorf("container is not part of a Docker Compose stack")
	}
	return nil
}

func (h *ContainerHandler) Start(ctx context.Context, input *dto.ContainerIDInput) (*struct{}, error) {
	if err := authmw.CheckRole(ctx, auth.RoleOperator); err != nil {
		return nil, err
	}
	if err := h.validateScope(ctx, input.ID); err != nil {
		return nil, huma.Error403Forbidden(err.Error())
	}
	if err := h.docker.StartContainer(ctx, input.ID); err != nil {
		return nil, serverError(ctx, err)
	}
	return nil, nil
}

func (h *ContainerHandler) Stop(ctx context.Context, input *dto.ContainerIDInput) (*struct{}, error) {
	if err := authmw.CheckRole(ctx, auth.RoleOperator); err != nil {
		return nil, err
	}
	if err := h.validateScope(ctx, input.ID); err != nil {
		return nil, huma.Error403Forbidden(err.Error())
	}
	if err := h.docker.StopContainer(ctx, input.ID); err != nil {
		return nil, serverError(ctx, err)
	}
	return nil, nil
}

func (h *ContainerHandler) Restart(ctx context.Context, input *dto.ContainerIDInput) (*struct{}, error) {
	if err := authmw.CheckRole(ctx, auth.RoleOperator); err != nil {
		return nil, err
	}
	if err := h.validateScope(ctx, input.ID); err != nil {
		return nil, huma.Error403Forbidden(err.Error())
	}
	if err := h.docker.RestartContainer(ctx, input.ID); err != nil {
		return nil, serverError(ctx, err)
	}
	return nil, nil
}

func (h *ContainerHandler) Logs(ctx context.Context, input *dto.ContainerLogsInput) (*dto.ContainerLogsOutput, error) {
	if err := authmw.CheckRole(ctx, auth.RoleViewer); err != nil {
		return nil, err
	}

	reader, err := h.docker.ContainerLogs(ctx, input.ID, false, input.Tail, input.Since)
	if err != nil {
		return nil, serverError(ctx, err)
	}
	defer reader.Close()

	// Properly demux Docker's multiplexed stream using stdcopy.
	// For TTY containers, Docker doesn't add multiplex headers -- stdcopy
	// handles this gracefully by passing through raw data.
	var stdout, stderr strings.Builder
	_, err = stdcopy.StdCopy(&stdout, &stderr, io.LimitReader(reader, 1<<20))
	if err != nil && err != io.EOF {
		// Fallback: TTY mode containers don't use multiplex framing.
		// Re-read as raw text.
		reader2, err2 := h.docker.ContainerLogs(ctx, input.ID, false, input.Tail, input.Since)
		if err2 != nil {
			return nil, serverError(ctx, err)
		}
		defer reader2.Close()
		raw, _ := io.ReadAll(io.LimitReader(reader2, 1<<20))
		stdout.WriteString(string(raw))
	}

	combined := stripANSI(stdout.String() + stderr.String())
	var lines []string
	for _, line := range strings.Split(combined, "\n") {
		if strings.TrimSpace(line) != "" {
			lines = append(lines, line)
		}
	}

	out := &dto.ContainerLogsOutput{}
	out.Body.Lines = lines
	return out, nil
}
