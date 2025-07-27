package docker

type ImageInfo struct {
	ID          string            `json:"id"`
	RepoTags    []string          `json:"repo_tags"`
	RepoDigests []string          `json:"repo_digests"`
	Size        int64             `json:"size"`
	Created     int64             `json:"created"`
	Labels      map[string]string `json:"labels"`
	Containers  int64             `json:"containers"`
}

type PruneResult struct {
	SpaceReclaimed uint64   `json:"space_reclaimed"`
	ImagesDeleted  []string `json:"images_deleted"`
}

type SystemPruneResult struct {
	SpaceReclaimed    uint64   `json:"space_reclaimed"`
	ContainersDeleted []string `json:"containers_deleted"`
	ImagesDeleted     []string `json:"images_deleted"`
	NetworksDeleted   []string `json:"networks_deleted"`
	VolumesDeleted    []string `json:"volumes_deleted"`
	BuildCacheDeleted []string `json:"build_cache_deleted"`
}

type VolumeInfo struct {
	Name       string            `json:"name"`
	Driver     string            `json:"driver"`
	Mountpoint string            `json:"mountpoint"`
	Labels     map[string]string `json:"labels"`
	Scope      string            `json:"scope"`
	CreatedAt  string            `json:"created_at"`
	Status     map[string]any    `json:"status"`
}

type NetworkInfo struct {
	ID       string            `json:"id"`
	Name     string            `json:"name"`
	Driver   string            `json:"driver"`
	Scope    string            `json:"scope"`
	Internal bool              `json:"internal"`
	Labels   map[string]string `json:"labels"`
	Created  string            `json:"created"`
}
