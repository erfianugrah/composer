package handler

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"

	authmw "github.com/erfianugrah/composer/internal/api/middleware"
	"github.com/erfianugrah/composer/internal/domain/auth"
	"github.com/erfianugrah/composer/internal/infra/docker"

	dockernetwork "github.com/docker/docker/api/types/network"
	dockervolume "github.com/docker/docker/api/types/volume"
)

// ResourceHandler manages Docker networks, volumes, and images.
type ResourceHandler struct {
	docker *docker.Client
}

func NewResourceHandler(docker *docker.Client) *ResourceHandler {
	return &ResourceHandler{docker: docker}
}

func (h *ResourceHandler) Register(api huma.API) {
	// Networks
	huma.Register(api, huma.Operation{
		OperationID: "listNetworks", Method: http.MethodGet,
		Path: "/api/v1/networks", Summary: "List Docker networks", Tags: []string{"networks"},
	}, h.ListNetworks)
	huma.Register(api, huma.Operation{
		OperationID: "createNetwork", Method: http.MethodPost,
		Path: "/api/v1/networks", Summary: "Create a Docker network", Tags: []string{"networks"},
	}, h.CreateNetwork)
	huma.Register(api, huma.Operation{
		OperationID: "inspectNetwork", Method: http.MethodGet,
		Path: "/api/v1/networks/{id}", Summary: "Inspect a Docker network (full JSON)", Tags: []string{"networks"},
	}, h.InspectNetwork)
	huma.Register(api, huma.Operation{
		OperationID: "removeNetwork", Method: http.MethodDelete,
		Path: "/api/v1/networks/{id}", Summary: "Remove a Docker network", Tags: []string{"networks"},
	}, h.RemoveNetwork)

	// Volumes
	huma.Register(api, huma.Operation{
		OperationID: "listVolumes", Method: http.MethodGet,
		Path: "/api/v1/volumes", Summary: "List Docker volumes", Tags: []string{"volumes"},
	}, h.ListVolumes)
	huma.Register(api, huma.Operation{
		OperationID: "createVolume", Method: http.MethodPost,
		Path: "/api/v1/volumes", Summary: "Create a Docker volume", Tags: []string{"volumes"},
	}, h.CreateVolume)
	huma.Register(api, huma.Operation{
		OperationID: "inspectVolume", Method: http.MethodGet,
		Path: "/api/v1/volumes/{name}", Summary: "Inspect a Docker volume (full JSON)", Tags: []string{"volumes"},
	}, h.InspectVolume)
	huma.Register(api, huma.Operation{
		OperationID: "removeVolume", Method: http.MethodDelete,
		Path: "/api/v1/volumes/{name}", Summary: "Remove a Docker volume", Tags: []string{"volumes"},
	}, h.RemoveVolume)
	huma.Register(api, huma.Operation{
		OperationID: "pruneVolumes", Method: http.MethodPost,
		Path: "/api/v1/volumes/prune", Summary: "Remove unused Docker volumes", Tags: []string{"volumes"},
	}, h.PruneVolumes)

	// Images
	huma.Register(api, huma.Operation{
		OperationID: "listImages", Method: http.MethodGet,
		Path: "/api/v1/images", Summary: "List Docker images", Tags: []string{"images"},
	}, h.ListImages)
	huma.Register(api, huma.Operation{
		OperationID: "pullImage", Method: http.MethodPost,
		Path: "/api/v1/images/pull", Summary: "Pull a Docker image", Tags: []string{"images"},
	}, h.PullImage)
	huma.Register(api, huma.Operation{
		OperationID: "removeImage", Method: http.MethodDelete,
		Path: "/api/v1/images/{id}", Summary: "Remove a Docker image", Tags: []string{"images"},
	}, h.RemoveImage)
	huma.Register(api, huma.Operation{
		OperationID: "recentDockerEvents", Method: http.MethodGet,
		Path: "/api/v1/docker/events", Summary: "Recent Docker events (last 5 minutes)", Tags: []string{"docker"},
	}, h.RecentEvents)

	huma.Register(api, huma.Operation{
		OperationID: "pruneImages", Method: http.MethodPost,
		Path: "/api/v1/images/prune", Summary: "Remove unused Docker images", Tags: []string{"images"},
	}, h.PruneImages)
}

// --- Networks ---

type NetworkListOutput struct {
	Body struct {
		Networks []docker.NetworkInfo `json:"networks"`
	}
}

type CreateNetworkInput struct {
	Body struct {
		Name   string `json:"name" minLength:"1" doc:"Network name"`
		Driver string `json:"driver,omitempty" doc:"Network driver (default: bridge)"`
	}
}

type NetworkIDInput struct {
	ID string `path:"id" doc:"Network ID or name"`
}

func (h *ResourceHandler) ListNetworks(ctx context.Context, input *struct{}) (*NetworkListOutput, error) {
	if err := authmw.CheckRole(ctx, auth.RoleViewer); err != nil {
		return nil, err
	}
	nets, err := h.docker.ListNetworks(ctx)
	if err != nil {
		return nil, serverError(err)
	}
	out := &NetworkListOutput{}
	out.Body.Networks = nets
	return out, nil
}

func (h *ResourceHandler) CreateNetwork(ctx context.Context, input *CreateNetworkInput) (*struct{}, error) {
	if err := authmw.CheckRole(ctx, auth.RoleOperator); err != nil {
		return nil, err
	}
	driver := input.Body.Driver
	if driver == "" {
		driver = "bridge"
	}
	if err := h.docker.CreateNetwork(ctx, input.Body.Name, driver); err != nil {
		return nil, huma.Error422UnprocessableEntity(err.Error())
	}
	return nil, nil
}

func (h *ResourceHandler) RemoveNetwork(ctx context.Context, input *NetworkIDInput) (*struct{}, error) {
	if err := authmw.CheckRole(ctx, auth.RoleOperator); err != nil {
		return nil, err
	}
	if err := h.docker.RemoveNetwork(ctx, input.ID); err != nil {
		return nil, huma.Error422UnprocessableEntity(err.Error())
	}
	return nil, nil
}

type NetworkInspectOutput struct {
	Body dockernetwork.Inspect
}

func (h *ResourceHandler) InspectNetwork(ctx context.Context, input *NetworkIDInput) (*NetworkInspectOutput, error) {
	if err := authmw.CheckRole(ctx, auth.RoleViewer); err != nil {
		return nil, err
	}
	data, err := h.docker.InspectNetwork(ctx, input.ID)
	if err != nil {
		return nil, huma.Error404NotFound("network not found: " + err.Error())
	}
	return &NetworkInspectOutput{Body: data}, nil
}

// --- Volumes ---

type VolumeListOutput struct {
	Body struct {
		Volumes []docker.VolumeInfo `json:"volumes"`
	}
}

type CreateVolumeInput struct {
	Body struct {
		Name   string `json:"name" minLength:"1" doc:"Volume name"`
		Driver string `json:"driver,omitempty" doc:"Volume driver (default: local)"`
	}
}

type VolumeNameInput struct {
	Name string `path:"name" doc:"Volume name"`
}

type PruneOutput struct {
	Body struct {
		SpaceReclaimed string `json:"space_reclaimed"`
	}
}

func (h *ResourceHandler) ListVolumes(ctx context.Context, input *struct{}) (*VolumeListOutput, error) {
	if err := authmw.CheckRole(ctx, auth.RoleViewer); err != nil {
		return nil, err
	}
	vols, err := h.docker.ListVolumes(ctx)
	if err != nil {
		return nil, serverError(err)
	}
	out := &VolumeListOutput{}
	out.Body.Volumes = vols
	return out, nil
}

func (h *ResourceHandler) CreateVolume(ctx context.Context, input *CreateVolumeInput) (*struct{}, error) {
	if err := authmw.CheckRole(ctx, auth.RoleOperator); err != nil {
		return nil, err
	}
	driver := input.Body.Driver
	if driver == "" {
		driver = "local"
	}
	if err := h.docker.CreateVolume(ctx, input.Body.Name, driver); err != nil {
		return nil, huma.Error422UnprocessableEntity(err.Error())
	}
	return nil, nil
}

func (h *ResourceHandler) RemoveVolume(ctx context.Context, input *VolumeNameInput) (*struct{}, error) {
	if err := authmw.CheckRole(ctx, auth.RoleOperator); err != nil {
		return nil, err
	}
	if err := h.docker.RemoveVolume(ctx, input.Name); err != nil {
		return nil, huma.Error422UnprocessableEntity(err.Error())
	}
	return nil, nil
}

type VolumeInspectOutput struct {
	Body dockervolume.Volume
}

func (h *ResourceHandler) InspectVolume(ctx context.Context, input *VolumeNameInput) (*VolumeInspectOutput, error) {
	if err := authmw.CheckRole(ctx, auth.RoleViewer); err != nil {
		return nil, err
	}
	data, err := h.docker.InspectVolume(ctx, input.Name)
	if err != nil {
		return nil, huma.Error404NotFound("volume not found: " + err.Error())
	}
	return &VolumeInspectOutput{Body: data}, nil
}

func (h *ResourceHandler) PruneVolumes(ctx context.Context, input *struct{}) (*PruneOutput, error) {
	if err := authmw.CheckRole(ctx, auth.RoleAdmin); err != nil {
		return nil, err
	}
	reclaimed, err := h.docker.PruneVolumes(ctx)
	if err != nil {
		return nil, serverError(err)
	}
	out := &PruneOutput{}
	out.Body.SpaceReclaimed = formatBytes(reclaimed)
	return out, nil
}

// --- Images ---

type ImageListOutput struct {
	Body struct {
		Images []docker.ImageInfo `json:"images"`
	}
}

type PullImageInput struct {
	Body struct {
		Ref string `json:"ref" minLength:"1" doc:"Image reference (e.g. nginx:alpine, ghcr.io/user/image:tag)"`
	}
}

type ImageIDInput struct {
	ID string `path:"id" doc:"Image ID or tag"`
}

func (h *ResourceHandler) ListImages(ctx context.Context, input *struct{}) (*ImageListOutput, error) {
	if err := authmw.CheckRole(ctx, auth.RoleViewer); err != nil {
		return nil, err
	}
	imgs, err := h.docker.ListImages(ctx)
	if err != nil {
		return nil, serverError(err)
	}
	out := &ImageListOutput{}
	out.Body.Images = imgs
	return out, nil
}

func (h *ResourceHandler) PullImage(ctx context.Context, input *PullImageInput) (*struct{}, error) {
	if err := authmw.CheckRole(ctx, auth.RoleOperator); err != nil {
		return nil, err
	}
	if err := h.docker.PullImage(ctx, input.Body.Ref); err != nil {
		return nil, huma.Error422UnprocessableEntity(err.Error())
	}
	return nil, nil
}

func (h *ResourceHandler) RemoveImage(ctx context.Context, input *ImageIDInput) (*struct{}, error) {
	if err := authmw.CheckRole(ctx, auth.RoleOperator); err != nil {
		return nil, err
	}
	if err := h.docker.RemoveImage(ctx, input.ID); err != nil {
		return nil, huma.Error422UnprocessableEntity(err.Error())
	}
	return nil, nil
}

func (h *ResourceHandler) PruneImages(ctx context.Context, input *struct{}) (*PruneOutput, error) {
	if err := authmw.CheckRole(ctx, auth.RoleAdmin); err != nil {
		return nil, err
	}
	reclaimed, err := h.docker.PruneImages(ctx)
	if err != nil {
		return nil, serverError(err)
	}
	out := &PruneOutput{}
	out.Body.SpaceReclaimed = formatBytes(reclaimed)
	return out, nil
}

func formatBytes(b uint64) string {
	if b < 1024 {
		return fmt.Sprintf("%d B", b)
	}
	if b < 1024*1024 {
		return fmt.Sprintf("%.1f KB", float64(b)/1024)
	}
	if b < 1024*1024*1024 {
		return fmt.Sprintf("%.1f MB", float64(b)/(1024*1024))
	}
	return fmt.Sprintf("%.2f GB", float64(b)/(1024*1024*1024))
}

// --- Recent Docker Events ---

type DockerEventItem struct {
	Type   string `json:"type"`   // container, network, volume, image
	Action string `json:"action"` // create, start, stop, die, destroy, etc.
	Actor  string `json:"actor"`  // container name, image ref, network name
	ID     string `json:"id"`     // short ID
	Time   string `json:"time"`   // ISO timestamp
}

type RecentEventsInput struct {
	Since string `query:"since" default:"5m" doc:"How far back to look (e.g. 5m, 1h, 30m)"`
}

type RecentEventsOutput struct {
	Body struct {
		Events []DockerEventItem `json:"events"`
	}
}

func (h *ResourceHandler) RecentEvents(ctx context.Context, input *RecentEventsInput) (*RecentEventsOutput, error) {
	if err := authmw.CheckRole(ctx, auth.RoleViewer); err != nil {
		return nil, err
	}

	// Parse since duration
	dur, err := time.ParseDuration(input.Since)
	if err != nil {
		dur = 5 * time.Minute
	}
	sinceUnix := time.Now().Add(-dur).Unix()

	// Get events with a short timeout context (don't block forever)
	evtCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	msgCh, errCh := h.docker.EventsSince(evtCtx, sinceUnix)

	out := &RecentEventsOutput{}
	out.Body.Events = []DockerEventItem{}

	for {
		select {
		case msg, ok := <-msgCh:
			if !ok {
				return out, nil
			}
			actor := msg.Actor.Attributes["name"]
			if actor == "" {
				actor = msg.Actor.Attributes["image"]
			}
			id := msg.Actor.ID
			if len(id) > 12 {
				id = id[:12]
			}
			out.Body.Events = append(out.Body.Events, DockerEventItem{
				Type:   string(msg.Type),
				Action: string(msg.Action),
				Actor:  actor,
				ID:     id,
				Time:   time.Unix(msg.Time, msg.TimeNano).Format(time.RFC3339),
			})
			// Cap at 100 events
			if len(out.Body.Events) >= 100 {
				return out, nil
			}
		case <-errCh:
			return out, nil
		case <-evtCtx.Done():
			return out, nil
		}
	}
}
