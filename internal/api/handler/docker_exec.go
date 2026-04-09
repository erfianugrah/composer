package handler

import (
	"context"
	"net/http"
	"strings"

	"github.com/danielgtaylor/huma/v2"

	authmw "github.com/erfianugrah/composer/internal/api/middleware"
	"github.com/erfianugrah/composer/internal/domain/auth"
	"github.com/erfianugrah/composer/internal/infra/docker"
)

// DockerExecHandler provides a global docker command runner.
type DockerExecHandler struct {
	compose *docker.Compose
}

func NewDockerExecHandler(compose *docker.Compose) *DockerExecHandler {
	return &DockerExecHandler{compose: compose}
}

func (h *DockerExecHandler) Register(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "execDockerCommand",
		Method:      http.MethodPost,
		Path:        "/api/v1/docker/exec",
		Summary:     "Run a docker command (admin only). No SSH needed.",
		Tags:        []string{"docker"},
	}, h.Exec)
}

type DockerExecInput struct {
	Body struct {
		Command string `json:"command" minLength:"1" doc:"Docker subcommand (e.g. 'ps', 'images', 'network ls', 'volume ls')"`
	}
}

type DockerExecOutput struct {
	Body struct {
		Stdout   string `json:"stdout"`
		Stderr   string `json:"stderr"`
		ExitCode int    `json:"exit_code"`
	}
}

func (h *DockerExecHandler) Exec(ctx context.Context, input *DockerExecInput) (*DockerExecOutput, error) {
	if err := authmw.CheckRole(ctx, auth.RoleAdmin); err != nil {
		return nil, err
	}

	args := strings.Fields(input.Body.Command)
	if len(args) == 0 {
		return nil, huma.Error422UnprocessableEntity("command is empty")
	}

	// Allowlist of safe docker subcommands
	allowed := map[string]bool{
		"ps": true, "images": true, "network": true, "volume": true,
		"system": true, "info": true, "version": true, "inspect": true,
		"logs": true, "stats": true, "top": true, "port": true,
		"diff": true, "history": true, "search": true, "tag": true,
		"compose": true,
	}
	if !allowed[args[0]] {
		return nil, huma.Error422UnprocessableEntity("command '" + args[0] + "' is not allowed; permitted: ps, images, network, volume, system, info, version, inspect, logs, stats, top, port, diff, history, search, tag, compose")
	}

	result, err := h.compose.RunDocker(ctx, args)

	out := &DockerExecOutput{}
	if result != nil {
		out.Body.Stdout = result.Stdout
		out.Body.Stderr = result.Stderr
		out.Body.ExitCode = result.ExitCode
	}
	if err != nil {
		if result != nil {
			return out, nil
		}
		return nil, serverError(err)
	}
	return out, nil
}
