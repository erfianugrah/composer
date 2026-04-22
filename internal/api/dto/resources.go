package dto

// --- Networks ---

// NetworkInfo is a network summary in the list response.
// Mirrors a subset of docker.NetworkInfo; kept separate so the internal domain
// type can evolve without mutating the API.
type NetworkInfo struct {
	ID         string            `json:"id"`
	Name       string            `json:"name"`
	Driver     string            `json:"driver"`
	Scope      string            `json:"scope"`
	Internal   bool              `json:"internal"`
	Containers int               `json:"containers" doc:"Number of containers attached"`
	Labels     map[string]string `json:"labels,omitempty"`
}

// NetworkListOutput is the response body for listNetworks.
type NetworkListOutput struct {
	Body struct {
		Networks []NetworkInfo `json:"networks"`
	}
}

// CreateNetworkInput is the request body for createNetwork.
type CreateNetworkInput struct {
	Body struct {
		Name   string `json:"name" minLength:"1" maxLength:"128" doc:"Network name"`
		Driver string `json:"driver,omitempty" maxLength:"32" doc:"Network driver (default: bridge)"`
	}
}

// NetworkIDInput is a path parameter for network operations.
type NetworkIDInput struct {
	ID string `path:"id" maxLength:"256" doc:"Network ID or name"`
}

// NetworkInspectIPAM describes IPAM config for a network.
type NetworkInspectIPAM struct {
	Driver  string              `json:"driver,omitempty"`
	Options map[string]string   `json:"options,omitempty"`
	Config  []map[string]string `json:"config,omitempty"`
}

// NetworkInspectOutput is a curated subset of Docker's full network inspect response.
// Intentionally narrower than dockernetwork.Inspect to avoid leaking upstream type changes.
type NetworkInspectOutput struct {
	Body struct {
		ID         string             `json:"id"`
		Name       string             `json:"name"`
		Driver     string             `json:"driver"`
		Scope      string             `json:"scope"`
		EnableIPv6 bool                `json:"enable_ipv6"`
		Internal   bool                `json:"internal"`
		Attachable bool                `json:"attachable"`
		Ingress    bool                `json:"ingress"`
		IPAM       NetworkInspectIPAM  `json:"ipam"`
		Options    map[string]string   `json:"options,omitempty"`
		Labels     map[string]string   `json:"labels,omitempty"`
		Containers map[string]string   `json:"containers,omitempty" doc:"Map of container ID to container name attached to this network"`
		Created    string              `json:"created,omitempty" format:"date-time"`
	}
}

// --- Volumes ---

// VolumeInfo is a volume summary in the list response.
type VolumeInfo struct {
	Name       string            `json:"name"`
	Driver     string            `json:"driver"`
	Mountpoint string            `json:"mountpoint"`
	CreatedAt  string            `json:"created_at,omitempty" format:"date-time"`
	Labels     map[string]string `json:"labels,omitempty"`
}

// VolumeListOutput is the response body for listVolumes.
type VolumeListOutput struct {
	Body struct {
		Volumes []VolumeInfo `json:"volumes"`
	}
}

// CreateVolumeInput is the request body for createVolume.
type CreateVolumeInput struct {
	Body struct {
		Name   string `json:"name" minLength:"1" maxLength:"128" doc:"Volume name"`
		Driver string `json:"driver,omitempty" maxLength:"32" doc:"Volume driver (default: local)"`
	}
}

// VolumeNameInput is a path parameter for volume operations.
type VolumeNameInput struct {
	Name string `path:"name" maxLength:"128" doc:"Volume name"`
}

// VolumeInspectOutput is a curated subset of Docker's full volume inspect response.
type VolumeInspectOutput struct {
	Body struct {
		Name       string            `json:"name"`
		Driver     string            `json:"driver"`
		Mountpoint string            `json:"mountpoint"`
		Scope      string            `json:"scope,omitempty"`
		CreatedAt  string            `json:"created_at,omitempty" format:"date-time"`
		Options    map[string]string `json:"options,omitempty"`
		Labels     map[string]string `json:"labels,omitempty"`
	}
}

// --- Images ---

// ImageInfo is an image summary in the list response.
type ImageInfo struct {
	ID      string   `json:"id"`
	Tags    []string `json:"tags"`
	Size    int64    `json:"size" doc:"Uncompressed image size in bytes"`
	Created int64    `json:"created" doc:"Unix timestamp"`
}

// ImageListOutput is the response body for listImages.
type ImageListOutput struct {
	Body struct {
		Images []ImageInfo `json:"images"`
	}
}

// PullImageInput is the request body for pullImage.
type PullImageInput struct {
	Body struct {
		Ref string `json:"ref" minLength:"1" maxLength:"512" doc:"Image reference (e.g. nginx:alpine, ghcr.io/user/image:tag)"`
	}
}

// ImageIDInput is a path parameter for image operations.
type ImageIDInput struct {
	ID string `path:"id" maxLength:"256" doc:"Image ID or tag"`
}

// --- Prune ---

// PruneOutput is a simple reclaim-space result.
type PruneOutput struct {
	Body struct {
		SpaceReclaimed string `json:"space_reclaimed" doc:"Human-readable reclaimed size (e.g. '1.2 GB')"`
	}
}

// PruneImagesInput configures image pruning.
type PruneImagesInput struct {
	All bool `query:"all" default:"false" doc:"Remove all unused images, not just dangling/untagged"`
}

// PruneNetworksOutput is the result of network pruning.
type PruneNetworksOutput struct {
	Body struct {
		NetworksDeleted []string `json:"networks_deleted"`
	}
}

// SystemPruneInput configures a system-wide prune.
type SystemPruneInput struct {
	All     bool `query:"all" default:"true" doc:"Remove all unused images (not just dangling)"`
	Volumes bool `query:"volumes" default:"false" doc:"Also prune unused volumes"`
}

// SystemPruneOutput reports the results of a system-wide prune.
type SystemPruneOutput struct {
	Body struct {
		ContainersReclaimed string   `json:"containers_reclaimed"`
		ImagesReclaimed     string   `json:"images_reclaimed"`
		NetworksDeleted     []string `json:"networks_deleted"`
		BuildCacheReclaimed string   `json:"build_cache_reclaimed"`
		VolumesReclaimed    string   `json:"volumes_reclaimed,omitempty"`
		TotalReclaimed      string   `json:"total_reclaimed"`
	}
}

// --- Docker events ---

// DockerEventItem is a single Docker daemon event.
type DockerEventItem struct {
	Type   string `json:"type" enum:"container,network,volume,image,daemon,plugin,service,node,secret,config" doc:"Event resource type"`
	Action string `json:"action" doc:"create, start, stop, die, destroy, pull, etc."`
	Actor  string `json:"actor" doc:"Container name, image ref, or network name"`
	ID     string `json:"id" doc:"Short ID (first 12 chars of the full resource ID)"`
	Time   string `json:"time" format:"date-time"`
}

// RecentEventsInput configures the look-back window for recent events.
type RecentEventsInput struct {
	Since string `query:"since" default:"5m" doc:"How far back to look, as Go duration (e.g. 5m, 1h, 30m)"`
}

// RecentEventsOutput returns up to 100 recent events.
type RecentEventsOutput struct {
	Body struct {
		Events []DockerEventItem `json:"events"`
	}
}
