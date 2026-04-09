package handler

import (
	"context"
	"net/http"
	"runtime"
	"time"

	"github.com/danielgtaylor/huma/v2"

	composer "github.com/erfianugrah/composer"
	"github.com/erfianugrah/composer/internal/infra/docker"
)

var startTime = time.Now()

// SystemHandler registers system endpoints.
type SystemHandler struct {
	docker *docker.Client
}

func NewSystemHandler(docker *docker.Client) *SystemHandler {
	return &SystemHandler{docker: docker}
}

func (h *SystemHandler) Register(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "systemInfo", Method: http.MethodGet,
		Path: "/api/v1/system/info", Summary: "Docker engine info", Tags: []string{"system"},
	}, h.Info)

	huma.Register(api, huma.Operation{
		OperationID: "systemVersion", Method: http.MethodGet,
		Path: "/api/v1/system/version", Summary: "Composer version info", Tags: []string{"system"},
	}, h.Version)
}

type SystemInfoOutput struct {
	Body struct {
		Docker struct {
			Version    string `json:"version"`
			APIVersion string `json:"api_version"`
			Runtime    string `json:"runtime"`
			OS         string `json:"os"`
			Arch       string `json:"arch"`
			Containers int    `json:"containers"`
			Images     int    `json:"images"`
		} `json:"docker"`
	}
}

func (h *SystemHandler) Info(ctx context.Context, input *struct{}) (*SystemInfoOutput, error) {
	if h.docker == nil {
		return nil, huma.Error503ServiceUnavailable("docker not available")
	}

	info, err := h.docker.Info(ctx)
	if err != nil {
		return nil, internalError()
	}

	out := &SystemInfoOutput{}
	out.Body.Docker.Version = info.ServerVersion
	out.Body.Docker.APIVersion = info.Driver
	out.Body.Docker.Runtime = h.docker.Runtime()
	out.Body.Docker.OS = info.OperatingSystem
	out.Body.Docker.Arch = info.Architecture
	out.Body.Docker.Containers = info.Containers
	out.Body.Docker.Images = info.Images
	return out, nil
}

type VersionOutput struct {
	Body struct {
		Version   string `json:"version"`
		GoVersion string `json:"go_version"`
		OS        string `json:"os"`
		Arch      string `json:"arch"`
		Uptime    string `json:"uptime"`
	}
}

func (h *SystemHandler) Version(ctx context.Context, input *struct{}) (*VersionOutput, error) {
	out := &VersionOutput{}
	out.Body.Version = composer.Version
	out.Body.GoVersion = runtime.Version()
	out.Body.OS = runtime.GOOS
	out.Body.Arch = runtime.GOARCH
	out.Body.Uptime = time.Since(startTime).Round(time.Second).String()
	return out, nil
}
