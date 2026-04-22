package handler

import (
	"context"
	"net/http"
	"strings"

	"github.com/danielgtaylor/huma/v2"

	"github.com/erfianugrah/composer/internal/api/dto"
	authmw "github.com/erfianugrah/composer/internal/api/middleware"
	"github.com/erfianugrah/composer/internal/domain/auth"
	dockerinfra "github.com/erfianugrah/composer/internal/infra/docker"
)

// DockerExecHandler provides a global docker command runner.
type DockerExecHandler struct {
	compose *dockerinfra.Compose
}

func NewDockerExecHandler(compose *dockerinfra.Compose) *DockerExecHandler {
	return &DockerExecHandler{compose: compose}
}

func (h *DockerExecHandler) Register(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "execDockerCommand",
		Method:      http.MethodPost,
		Path:        "/api/v1/docker/exec",
		Summary:     "Run a docker command (admin only)",
		Description: "Executes a whitelisted `docker <subcommand>` on the host. Intended as an SSH-free debugging console. Allowlisted to read-only subcommands (ps, images, network, volume, system, info, version, inspect, logs, stats, top, port, diff, history, search, tag). Admin only.",
		Tags:        []string{"docker"},
		Errors:      errsAdminMutation,
	}, h.Exec)
}

func (h *DockerExecHandler) Exec(ctx context.Context, input *dto.DockerExecInput) (*dto.DockerExecOutput, error) {
	if err := authmw.CheckRole(ctx, auth.RoleAdmin); err != nil {
		return nil, err
	}

	args, err := dockerinfra.ShellSplit(input.Body.Command)
	if err != nil {
		return nil, huma.Error422UnprocessableEntity(err.Error())
	}
	if len(args) == 0 {
		return nil, huma.Error422UnprocessableEntity("command is empty")
	}

	ok, permitted := dockerinfra.DockerAllowed(args[0])
	if !ok {
		return nil, huma.Error422UnprocessableEntity(
			"command '" + args[0] + "' is not allowed; permitted: " + strings.Join(permitted, ", "),
		)
	}

	result, err := h.compose.RunDocker(ctx, args)

	out := &dto.DockerExecOutput{}
	if result != nil {
		out.Body.Stdout = result.Stdout
		out.Body.Stderr = result.Stderr
		out.Body.ExitCode = result.ExitCode
	}
	if err != nil {
		if result != nil {
			return out, nil
		}
		return nil, serverError(ctx, err)
	}
	return out, nil
}
