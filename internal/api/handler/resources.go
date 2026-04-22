package handler

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/erfianugrah/composer/internal/api/dto"
	authmw "github.com/erfianugrah/composer/internal/api/middleware"
	"github.com/erfianugrah/composer/internal/domain/auth"
	"github.com/erfianugrah/composer/internal/infra/docker"
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
		Path:        "/api/v1/networks",
		Summary:     "List Docker networks",
		Description: "Returns all Docker networks visible to the daemon including the default bridge and compose-created networks.",
		Tags:        []string{"networks"},
		Errors:      errsViewer,
	}, h.ListNetworks)
	huma.Register(api, huma.Operation{
		OperationID: "createNetwork", Method: http.MethodPost,
		Path:        "/api/v1/networks",
		Summary:     "Create a Docker network",
		Description: "Creates a new network with the specified driver. Defaults to `bridge` driver when omitted.",
		Tags:        []string{"networks"},
		Errors:      errsOperatorMutation,
	}, h.CreateNetwork)
	huma.Register(api, huma.Operation{
		OperationID: "inspectNetwork", Method: http.MethodGet,
		Path:        "/api/v1/networks/{id}",
		Summary:     "Inspect a Docker network",
		Description: "Returns detailed network configuration (IPAM, attached containers, options, labels).",
		Tags:        []string{"networks"},
		Errors:      errsViewerNotFound,
	}, h.InspectNetwork)
	huma.Register(api, huma.Operation{
		OperationID: "removeNetwork", Method: http.MethodDelete,
		Path:        "/api/v1/networks/{id}",
		Summary:     "Remove a Docker network",
		Description: "Deletes the network. Fails if the network has active endpoints (detach containers first).",
		Tags:        []string{"networks"},
		Errors:      errsOperatorMutation,
	}, h.RemoveNetwork)

	// Volumes
	huma.Register(api, huma.Operation{
		OperationID: "listVolumes", Method: http.MethodGet,
		Path:        "/api/v1/volumes",
		Summary:     "List Docker volumes",
		Description: "Returns all named Docker volumes on the daemon.",
		Tags:        []string{"volumes"},
		Errors:      errsViewer,
	}, h.ListVolumes)
	huma.Register(api, huma.Operation{
		OperationID: "createVolume", Method: http.MethodPost,
		Path:        "/api/v1/volumes",
		Summary:     "Create a Docker volume",
		Description: "Creates a new named volume. Defaults to `local` driver.",
		Tags:        []string{"volumes"},
		Errors:      errsOperatorMutation,
	}, h.CreateVolume)
	huma.Register(api, huma.Operation{
		OperationID: "inspectVolume", Method: http.MethodGet,
		Path:        "/api/v1/volumes/{name}",
		Summary:     "Inspect a Docker volume",
		Description: "Returns full volume metadata (driver, mountpoint, options, labels, creation time).",
		Tags:        []string{"volumes"},
		Errors:      errsViewerNotFound,
	}, h.InspectVolume)
	huma.Register(api, huma.Operation{
		OperationID: "removeVolume", Method: http.MethodDelete,
		Path:        "/api/v1/volumes/{name}",
		Summary:     "Remove a Docker volume",
		Description: "Deletes the volume and all its data. Fails if any container still references the volume.",
		Tags:        []string{"volumes"},
		Errors:      errsOperatorMutation,
	}, h.RemoveVolume)
	huma.Register(api, huma.Operation{
		OperationID: "pruneVolumes", Method: http.MethodPost,
		Path:        "/api/v1/volumes/prune",
		Summary:     "Remove unused Docker volumes",
		Description: "Deletes all volumes not referenced by any container. Destructive: data in unused volumes is permanently lost. Admin only.",
		Tags:        []string{"volumes"},
		Errors:      errsAdminMutation,
	}, h.PruneVolumes)

	// Images
	huma.Register(api, huma.Operation{
		OperationID: "listImages", Method: http.MethodGet,
		Path:        "/api/v1/images",
		Summary:     "List Docker images",
		Description: "Returns all local Docker images with tags, sizes, and creation timestamps.",
		Tags:        []string{"images"},
		Errors:      errsViewer,
	}, h.ListImages)
	huma.Register(api, huma.Operation{
		OperationID: "pullImage", Method: http.MethodPost,
		Path:        "/api/v1/images/pull",
		Summary:     "Pull a Docker image",
		Description: "Pulls an image from its registry. Accepts any valid image ref (nginx, nginx:alpine, ghcr.io/owner/repo:tag).",
		Tags:        []string{"images"},
		Errors:      errsOperatorMutation,
	}, h.PullImage)
	huma.Register(api, huma.Operation{
		OperationID: "removeImage", Method: http.MethodDelete,
		Path:        "/api/v1/images/{id}",
		Summary:     "Remove a Docker image",
		Description: "Deletes the image. Fails if any container (including stopped) uses it.",
		Tags:        []string{"images"},
		Errors:      errsOperatorMutation,
	}, h.RemoveImage)
	huma.Register(api, huma.Operation{
		OperationID: "recentDockerEvents", Method: http.MethodGet,
		Path:        "/api/v1/docker/events",
		Summary:     "Recent Docker events",
		Description: "Returns Docker daemon events from the last `since` duration (default 5 minutes). Caps at 100 events to avoid unbounded responses.",
		Tags:        []string{"docker"},
		Errors:      errsViewer,
	}, h.RecentEvents)

	huma.Register(api, huma.Operation{
		OperationID: "pruneImages", Method: http.MethodPost,
		Path:        "/api/v1/images/prune",
		Summary:     "Remove unused Docker images",
		Description: "Deletes dangling images. Pass `?all=true` to also delete unused tagged images. Admin only.",
		Tags:        []string{"images"},
		Errors:      errsAdminMutation,
	}, h.PruneImages)

	// System-wide cleanup
	huma.Register(api, huma.Operation{
		OperationID: "pruneContainers", Method: http.MethodPost,
		Path:        "/api/v1/containers/prune",
		Summary:     "Remove stopped containers",
		Description: "Deletes all stopped containers on the daemon. Admin only.",
		Tags:        []string{"docker"},
		Errors:      errsAdminMutation,
	}, h.PruneContainers)
	huma.Register(api, huma.Operation{
		OperationID: "pruneNetworks", Method: http.MethodPost,
		Path:        "/api/v1/networks/prune",
		Summary:     "Remove unused Docker networks",
		Description: "Deletes networks with no active endpoints. Admin only.",
		Tags:        []string{"docker"},
		Errors:      errsAdminMutation,
	}, h.PruneNetworks)
	huma.Register(api, huma.Operation{
		OperationID: "pruneBuildCache", Method: http.MethodPost,
		Path:        "/api/v1/builder/prune",
		Summary:     "Remove Docker build cache",
		Description: "Deletes BuildKit cache. Admin only.",
		Tags:        []string{"docker"},
		Errors:      errsAdminMutation,
	}, h.PruneBuildCache)
	huma.Register(api, huma.Operation{
		OperationID: "systemPrune", Method: http.MethodPost,
		Path:        "/api/v1/docker/prune",
		Summary:     "System prune",
		Description: "Equivalent to `docker system prune`. Removes stopped containers, unused networks, dangling images, and build cache. Pass `?all=true` for all unused images, `?volumes=true` to also prune volumes. Admin only.",
		Tags:        []string{"docker"},
		Errors:      errsAdminMutation,
	}, h.SystemPrune)
}

// --- Networks ---

func (h *ResourceHandler) ListNetworks(ctx context.Context, input *struct{}) (*dto.NetworkListOutput, error) {
	if err := authmw.CheckRole(ctx, auth.RoleViewer); err != nil {
		return nil, err
	}
	nets, err := h.docker.ListNetworks(ctx)
	if err != nil {
		return nil, serverError(ctx, err)
	}
	out := &dto.NetworkListOutput{}
	out.Body.Networks = make([]dto.NetworkInfo, 0, len(nets))
	for _, n := range nets {
		out.Body.Networks = append(out.Body.Networks, dto.NetworkInfo{
			ID: n.ID, Name: n.Name, Driver: n.Driver, Scope: n.Scope,
			Internal: n.Internal, Containers: n.Containers, Labels: n.Labels,
		})
	}
	return out, nil
}

func (h *ResourceHandler) CreateNetwork(ctx context.Context, input *dto.CreateNetworkInput) (*struct{}, error) {
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

func (h *ResourceHandler) RemoveNetwork(ctx context.Context, input *dto.NetworkIDInput) (*struct{}, error) {
	if err := authmw.CheckRole(ctx, auth.RoleOperator); err != nil {
		return nil, err
	}
	if err := h.docker.RemoveNetwork(ctx, input.ID); err != nil {
		return nil, huma.Error422UnprocessableEntity(err.Error())
	}
	return nil, nil
}

func (h *ResourceHandler) InspectNetwork(ctx context.Context, input *dto.NetworkIDInput) (*dto.NetworkInspectOutput, error) {
	if err := authmw.CheckRole(ctx, auth.RoleViewer); err != nil {
		return nil, err
	}
	data, err := h.docker.InspectNetwork(ctx, input.ID)
	if err != nil {
		return nil, huma.Error404NotFound("network not found: " + err.Error())
	}
	out := &dto.NetworkInspectOutput{}
	out.Body.ID = data.ID
	out.Body.Name = data.Name
	out.Body.Driver = data.Driver
	out.Body.Scope = data.Scope
	out.Body.EnableIPv6 = data.EnableIPv6
	out.Body.Internal = data.Internal
	out.Body.Attachable = data.Attachable
	out.Body.Ingress = data.Ingress
	out.Body.IPAM.Driver = data.IPAM.Driver
	out.Body.IPAM.Options = data.IPAM.Options
	for _, cfg := range data.IPAM.Config {
		out.Body.IPAM.Config = append(out.Body.IPAM.Config, map[string]string{
			"subnet":     cfg.Subnet,
			"gateway":    cfg.Gateway,
			"ip_range":   cfg.IPRange,
			"aux_addrs":  fmt.Sprintf("%v", cfg.AuxAddress),
		})
	}
	out.Body.Options = data.Options
	out.Body.Labels = data.Labels
	if len(data.Containers) > 0 {
		out.Body.Containers = make(map[string]string, len(data.Containers))
		for id, c := range data.Containers {
			out.Body.Containers[id] = c.Name
		}
	}
	if !data.Created.IsZero() {
		out.Body.Created = data.Created.Format(time.RFC3339)
	}
	return out, nil
}

// --- Volumes ---

func (h *ResourceHandler) ListVolumes(ctx context.Context, input *struct{}) (*dto.VolumeListOutput, error) {
	if err := authmw.CheckRole(ctx, auth.RoleViewer); err != nil {
		return nil, err
	}
	vols, err := h.docker.ListVolumes(ctx)
	if err != nil {
		return nil, serverError(ctx, err)
	}
	out := &dto.VolumeListOutput{}
	out.Body.Volumes = make([]dto.VolumeInfo, 0, len(vols))
	for _, v := range vols {
		out.Body.Volumes = append(out.Body.Volumes, dto.VolumeInfo{
			Name: v.Name, Driver: v.Driver, Mountpoint: v.Mountpoint,
			CreatedAt: v.CreatedAt, Labels: v.Labels,
		})
	}
	return out, nil
}

func (h *ResourceHandler) CreateVolume(ctx context.Context, input *dto.CreateVolumeInput) (*struct{}, error) {
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

func (h *ResourceHandler) RemoveVolume(ctx context.Context, input *dto.VolumeNameInput) (*struct{}, error) {
	if err := authmw.CheckRole(ctx, auth.RoleOperator); err != nil {
		return nil, err
	}
	if err := h.docker.RemoveVolume(ctx, input.Name); err != nil {
		return nil, huma.Error422UnprocessableEntity(err.Error())
	}
	return nil, nil
}

func (h *ResourceHandler) InspectVolume(ctx context.Context, input *dto.VolumeNameInput) (*dto.VolumeInspectOutput, error) {
	if err := authmw.CheckRole(ctx, auth.RoleViewer); err != nil {
		return nil, err
	}
	data, err := h.docker.InspectVolume(ctx, input.Name)
	if err != nil {
		return nil, huma.Error404NotFound("volume not found: " + err.Error())
	}
	out := &dto.VolumeInspectOutput{}
	out.Body.Name = data.Name
	out.Body.Driver = data.Driver
	out.Body.Mountpoint = data.Mountpoint
	out.Body.Scope = data.Scope
	out.Body.CreatedAt = data.CreatedAt
	out.Body.Options = data.Options
	out.Body.Labels = data.Labels
	return out, nil
}

func (h *ResourceHandler) PruneVolumes(ctx context.Context, input *struct{}) (*dto.PruneOutput, error) {
	if err := authmw.CheckRole(ctx, auth.RoleAdmin); err != nil {
		return nil, err
	}
	reclaimed, err := h.docker.PruneVolumes(ctx)
	if err != nil {
		return nil, serverError(ctx, err)
	}
	out := &dto.PruneOutput{}
	out.Body.SpaceReclaimed = formatBytes(reclaimed)
	return out, nil
}

// --- Images ---

func (h *ResourceHandler) ListImages(ctx context.Context, input *struct{}) (*dto.ImageListOutput, error) {
	if err := authmw.CheckRole(ctx, auth.RoleViewer); err != nil {
		return nil, err
	}
	imgs, err := h.docker.ListImages(ctx)
	if err != nil {
		return nil, serverError(ctx, err)
	}
	out := &dto.ImageListOutput{}
	out.Body.Images = make([]dto.ImageInfo, 0, len(imgs))
	for _, img := range imgs {
		tags := img.Tags
		if tags == nil {
			tags = []string{}
		}
		out.Body.Images = append(out.Body.Images, dto.ImageInfo{
			ID: img.ID, Tags: tags, Size: img.Size, Created: img.Created,
		})
	}
	return out, nil
}

func (h *ResourceHandler) PullImage(ctx context.Context, input *dto.PullImageInput) (*struct{}, error) {
	if err := authmw.CheckRole(ctx, auth.RoleOperator); err != nil {
		return nil, err
	}
	if err := h.docker.PullImage(ctx, input.Body.Ref); err != nil {
		return nil, huma.Error422UnprocessableEntity(err.Error())
	}
	return nil, nil
}

func (h *ResourceHandler) RemoveImage(ctx context.Context, input *dto.ImageIDInput) (*struct{}, error) {
	if err := authmw.CheckRole(ctx, auth.RoleOperator); err != nil {
		return nil, err
	}
	if err := h.docker.RemoveImage(ctx, input.ID); err != nil {
		return nil, huma.Error422UnprocessableEntity(err.Error())
	}
	return nil, nil
}

func (h *ResourceHandler) PruneImages(ctx context.Context, input *dto.PruneImagesInput) (*dto.PruneOutput, error) {
	if err := authmw.CheckRole(ctx, auth.RoleAdmin); err != nil {
		return nil, err
	}
	// Decouple from HTTP request context — prune can take minutes on large hosts.
	opCtx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	reclaimed, err := h.docker.PruneImages(opCtx, input.All)
	if err != nil {
		return nil, huma.Error422UnprocessableEntity(err.Error())
	}
	out := &dto.PruneOutput{}
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

func (h *ResourceHandler) RecentEvents(ctx context.Context, input *dto.RecentEventsInput) (*dto.RecentEventsOutput, error) {
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

	out := &dto.RecentEventsOutput{}
	out.Body.Events = []dto.DockerEventItem{}

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
			out.Body.Events = append(out.Body.Events, dto.DockerEventItem{
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

// --- Prune handlers ---

func (h *ResourceHandler) PruneContainers(ctx context.Context, input *struct{}) (*dto.PruneOutput, error) {
	if err := authmw.CheckRole(ctx, auth.RoleAdmin); err != nil {
		return nil, err
	}
	reclaimed, err := h.docker.PruneContainers(ctx)
	if err != nil {
		return nil, serverError(ctx, err)
	}
	out := &dto.PruneOutput{}
	out.Body.SpaceReclaimed = formatBytes(reclaimed)
	return out, nil
}

func (h *ResourceHandler) PruneNetworks(ctx context.Context, input *struct{}) (*dto.PruneNetworksOutput, error) {
	if err := authmw.CheckRole(ctx, auth.RoleAdmin); err != nil {
		return nil, err
	}
	deleted, err := h.docker.PruneNetworks(ctx)
	if err != nil {
		return nil, serverError(ctx, err)
	}
	out := &dto.PruneNetworksOutput{}
	out.Body.NetworksDeleted = deleted
	return out, nil
}

func (h *ResourceHandler) PruneBuildCache(ctx context.Context, input *struct{}) (*dto.PruneOutput, error) {
	if err := authmw.CheckRole(ctx, auth.RoleAdmin); err != nil {
		return nil, err
	}
	reclaimed, err := h.docker.PruneBuildCache(ctx)
	if err != nil {
		return nil, serverError(ctx, err)
	}
	out := &dto.PruneOutput{}
	out.Body.SpaceReclaimed = formatBytes(reclaimed)
	return out, nil
}

func (h *ResourceHandler) SystemPrune(ctx context.Context, input *dto.SystemPruneInput) (*dto.SystemPruneOutput, error) {
	if err := authmw.CheckRole(ctx, auth.RoleAdmin); err != nil {
		return nil, err
	}

	// Decouple from HTTP request context — full system prune can take minutes.
	opCtx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	var total uint64
	out := &dto.SystemPruneOutput{}

	// 1. Containers
	cr, _ := h.docker.PruneContainers(opCtx)
	total += cr
	out.Body.ContainersReclaimed = formatBytes(cr)

	// 2. Networks
	nd, _ := h.docker.PruneNetworks(opCtx)
	out.Body.NetworksDeleted = nd

	// 3. Images
	ir, _ := h.docker.PruneImages(opCtx, input.All)
	total += ir
	out.Body.ImagesReclaimed = formatBytes(ir)

	// 4. Build cache
	br, _ := h.docker.PruneBuildCache(opCtx)
	total += br
	out.Body.BuildCacheReclaimed = formatBytes(br)

	// 5. Volumes (optional — destructive)
	if input.Volumes {
		vr, _ := h.docker.PruneVolumes(opCtx)
		total += vr
		out.Body.VolumesReclaimed = formatBytes(vr)
	}

	out.Body.TotalReclaimed = formatBytes(total)
	return out, nil
}
