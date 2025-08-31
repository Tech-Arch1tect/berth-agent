package maintenance

import (
	"berth-agent/internal/docker"
	"context"
	"fmt"
	"strings"
	"time"
)

type Service struct {
	dockerClient *docker.Client
}

func NewService(dockerClient *docker.Client) *Service {
	return &Service{
		dockerClient: dockerClient,
	}
}

func (s *Service) GetSystemInfo(ctx context.Context) (*MaintenanceInfo, error) {
	systemInfo, err := s.getSystemInfo(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get system info: %w", err)
	}

	diskUsage, err := s.getDiskUsage(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get disk usage: %w", err)
	}

	imageSummary, err := s.getImageSummary(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get image summary: %w", err)
	}

	containerSummary, err := s.getContainerSummary(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get container summary: %w", err)
	}

	volumeSummary, err := s.getVolumeSummary(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get volume summary: %w", err)
	}

	networkSummary, err := s.getNetworkSummary(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get network summary: %w", err)
	}

	buildCacheSummary, err := s.getBuildCacheSummary(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get build cache summary: %w", err)
	}

	return &MaintenanceInfo{
		SystemInfo:        *systemInfo,
		DiskUsage:         *diskUsage,
		ImageSummary:      *imageSummary,
		ContainerSummary:  *containerSummary,
		VolumeSummary:     *volumeSummary,
		NetworkSummary:    *networkSummary,
		BuildCacheSummary: *buildCacheSummary,
		LastUpdated:       time.Now(),
	}, nil
}

func (s *Service) PruneDocker(ctx context.Context, req *PruneRequest) (*PruneResult, error) {
	switch req.Type {
	case "images":
		return s.pruneImages(ctx, req)
	case "containers":
		return s.pruneContainers(ctx, req)
	case "volumes":
		return s.pruneVolumes(ctx, req)
	case "networks":
		return s.pruneNetworks(ctx, req)
	case "build-cache":
		return s.pruneBuildCache(ctx, req)
	case "system":
		return s.pruneSystem(ctx, req)
	default:
		return nil, fmt.Errorf("unsupported prune type: %s", req.Type)
	}
}

func (s *Service) DeleteResource(ctx context.Context, req *DeleteRequest) (*DeleteResult, error) {
	switch req.Type {
	case "image":
		return s.deleteImage(ctx, req.ID)
	case "container":
		return s.deleteContainer(ctx, req.ID)
	case "volume":
		return s.deleteVolume(ctx, req.ID)
	case "network":
		return s.deleteNetwork(ctx, req.ID)
	default:
		return &DeleteResult{
			Type:    req.Type,
			ID:      req.ID,
			Success: false,
			Error:   fmt.Sprintf("unsupported resource type: %s", req.Type),
		}, nil
	}
}

func (s *Service) deleteImage(ctx context.Context, imageID string) (*DeleteResult, error) {
	_, err := s.dockerClient.ImageRemove(ctx, imageID, true, false)
	if err != nil {
		return &DeleteResult{
			Type:    "image",
			ID:      imageID,
			Success: false,
			Error:   err.Error(),
		}, nil
	}

	return &DeleteResult{
		Type:    "image",
		ID:      imageID,
		Success: true,
	}, nil
}

func (s *Service) deleteContainer(ctx context.Context, containerID string) (*DeleteResult, error) {
	err := s.dockerClient.ContainerRemove(ctx, containerID, true, true, true)
	if err != nil {
		return &DeleteResult{
			Type:    "container",
			ID:      containerID,
			Success: false,
			Error:   err.Error(),
		}, nil
	}

	return &DeleteResult{
		Type:    "container",
		ID:      containerID,
		Success: true,
	}, nil
}

func (s *Service) deleteVolume(ctx context.Context, volumeName string) (*DeleteResult, error) {
	err := s.dockerClient.VolumeRemove(ctx, volumeName, true)
	if err != nil {
		return &DeleteResult{
			Type:    "volume",
			ID:      volumeName,
			Success: false,
			Error:   err.Error(),
		}, nil
	}

	return &DeleteResult{
		Type:    "volume",
		ID:      volumeName,
		Success: true,
	}, nil
}

func (s *Service) deleteNetwork(ctx context.Context, networkID string) (*DeleteResult, error) {
	err := s.dockerClient.NetworkRemove(ctx, networkID)
	if err != nil {
		return &DeleteResult{
			Type:    "network",
			ID:      networkID,
			Success: false,
			Error:   err.Error(),
		}, nil
	}

	return &DeleteResult{
		Type:    "network",
		ID:      networkID,
		Success: true,
	}, nil
}

func (s *Service) getSystemInfo(ctx context.Context) (*SystemInfo, error) {
	info, err := s.dockerClient.SystemInfo(ctx)
	if err != nil {
		return nil, err
	}

	version, err := s.dockerClient.SystemVersion(ctx)
	if err != nil {
		return nil, err
	}

	return &SystemInfo{
		Version:       version.Version,
		APIVersion:    version.APIVersion,
		Architecture:  info.Architecture,
		OS:            info.OperatingSystem,
		KernelVersion: info.KernelVersion,
		TotalMemory:   info.MemTotal,
		NCPU:          info.NCPU,
		StorageDriver: info.Driver,
		DockerRootDir: info.DockerRootDir,
		ServerVersion: version.Version,
	}, nil
}

func (s *Service) getDiskUsage(ctx context.Context) (*DiskUsage, error) {
	diskUsage, err := s.dockerClient.SystemDiskUsage(ctx)
	if err != nil {
		return nil, err
	}

	var imagesSize int64
	if diskUsage.Images != nil {
		for _, img := range diskUsage.Images {
			imagesSize += img.Size
		}
	}

	var containersSize int64
	if diskUsage.Containers != nil {
		for _, container := range diskUsage.Containers {
			containersSize += container.SizeRw
		}
	}

	var volumesSize int64
	if diskUsage.Volumes != nil {
		for _, vol := range diskUsage.Volumes {
			if vol.UsageData != nil {
				volumesSize += vol.UsageData.Size
			}
		}
	}

	var buildCacheSize int64
	if diskUsage.BuildCache != nil {
		for _, cache := range diskUsage.BuildCache {
			buildCacheSize += cache.Size
		}
	}

	return &DiskUsage{
		LayersSize:     diskUsage.LayersSize,
		ImagesSize:     imagesSize,
		ContainersSize: containersSize,
		VolumesSize:    volumesSize,
		BuildCacheSize: buildCacheSize,
		TotalSize:      diskUsage.LayersSize + imagesSize + containersSize + volumesSize + buildCacheSize,
	}, nil
}

func (s *Service) getImageSummary(ctx context.Context) (*ImageSummary, error) {
	images, err := s.dockerClient.ImageList(ctx)
	if err != nil {
		return nil, err
	}

	containers, err := s.dockerClient.ContainerListAll(ctx)
	if err != nil {
		return nil, err
	}

	usedImages := make(map[string]bool)
	for _, container := range containers {
		usedImages[container.ImageID] = true
	}

	summary := &ImageSummary{
		Images: make([]ImageInfo, 0),
	}

	var totalSize, danglingSize, unusedSize int64
	var danglingCount, unusedCount int

	for _, img := range images {
		repository := "<none>"
		tag := "<none>"
		dangling := true

		if len(img.RepoTags) > 0 && img.RepoTags[0] != "<none>:<none>" {
			parts := strings.Split(img.RepoTags[0], ":")
			if len(parts) == 2 {
				repository = parts[0]
				tag = parts[1]
				dangling = false
			}
		}

		unused := !usedImages[img.ID]

		imageInfo := ImageInfo{
			Repository: repository,
			Tag:        tag,
			ID:         img.ID[:12],
			Size:       img.Size,
			Created:    time.Unix(img.Created, 0),
			Dangling:   dangling,
			Unused:     unused,
		}

		summary.Images = append(summary.Images, imageInfo)
		totalSize += img.Size

		if dangling {
			danglingCount++
			danglingSize += img.Size
		}

		if unused {
			unusedCount++
			unusedSize += img.Size
		}
	}

	summary.TotalCount = len(images)
	summary.DanglingCount = danglingCount
	summary.UnusedCount = unusedCount
	summary.TotalSize = totalSize
	summary.DanglingSize = danglingSize
	summary.UnusedSize = unusedSize

	return summary, nil
}

func (s *Service) getContainerSummary(ctx context.Context) (*ContainerSummary, error) {
	containers, err := s.dockerClient.ContainerListAll(ctx)
	if err != nil {
		return nil, err
	}

	var runningCount, stoppedCount int
	var totalSize int64
	containerInfos := make([]ContainerInfo, 0, len(containers))

	for _, container := range containers {
		if container.State == "running" {
			runningCount++
		} else {
			stoppedCount++
		}
		totalSize += container.SizeRw

		name := ""
		if len(container.Names) > 0 {
			name = strings.TrimPrefix(container.Names[0], "/")
		}

		containerInfo := ContainerInfo{
			ID:      container.ID[:12],
			Name:    name,
			Image:   container.Image,
			State:   container.State,
			Status:  container.Status,
			Created: time.Unix(container.Created, 0),
			Size:    container.SizeRw,
			Labels:  container.Labels,
		}
		containerInfos = append(containerInfos, containerInfo)
	}

	return &ContainerSummary{
		RunningCount: runningCount,
		StoppedCount: stoppedCount,
		TotalCount:   len(containers),
		TotalSize:    totalSize,
		Containers:   containerInfos,
	}, nil
}

func (s *Service) getVolumeSummary(ctx context.Context) (*VolumeSummary, error) {
	volumes, err := s.dockerClient.ListVolumes(ctx)
	if err != nil {
		return nil, err
	}

	containers, err := s.dockerClient.ContainerListAll(ctx)
	if err != nil {
		return nil, err
	}

	usedVolumes := make(map[string]bool)
	for _, container := range containers {
		for _, mount := range container.Mounts {
			if mount.Type == "volume" {
				usedVolumes[mount.Name] = true
			}
		}
	}

	var unusedCount int
	var totalSize, unusedSize int64
	volumeInfos := make([]VolumeInfo, 0, len(volumes.Volumes))

	for _, volume := range volumes.Volumes {
		size := int64(0)
		if volume.UsageData != nil {
			size = volume.UsageData.Size
			totalSize += size
			if !usedVolumes[volume.Name] {
				unusedSize += size
			}
		}

		unused := !usedVolumes[volume.Name]
		if unused {
			unusedCount++
		}

		created, _ := time.Parse(time.RFC3339, volume.CreatedAt)

		volumeInfo := VolumeInfo{
			Name:       volume.Name,
			Driver:     volume.Driver,
			Mountpoint: volume.Mountpoint,
			Created:    created,
			Size:       size,
			Labels:     volume.Labels,
			Unused:     unused,
		}
		volumeInfos = append(volumeInfos, volumeInfo)
	}

	return &VolumeSummary{
		TotalCount:  len(volumes.Volumes),
		UnusedCount: unusedCount,
		TotalSize:   totalSize,
		UnusedSize:  unusedSize,
		Volumes:     volumeInfos,
	}, nil
}

func (s *Service) getNetworkSummary(ctx context.Context) (*NetworkSummary, error) {
	networks, err := s.dockerClient.ListNetworks(ctx)
	if err != nil {
		return nil, err
	}

	containers, err := s.dockerClient.ContainerListAll(ctx)
	if err != nil {
		return nil, err
	}

	usedNetworks := make(map[string]bool)
	for _, container := range containers {
		for networkName := range container.NetworkSettings.Networks {
			usedNetworks[networkName] = true
		}
	}

	var unusedCount int
	networkInfos := make([]NetworkInfo, 0, len(networks))

	for _, network := range networks {
		unused := !usedNetworks[network.Name] && network.Name != "bridge" && network.Name != "host" && network.Name != "none"
		if unused {
			unusedCount++
		}

		created := network.Created

		networkInfo := NetworkInfo{
			ID:       network.ID[:12],
			Name:     network.Name,
			Driver:   network.Driver,
			Scope:    network.Scope,
			Created:  created,
			Internal: network.Internal,
			Labels:   network.Labels,
			Unused:   unused,
		}
		networkInfos = append(networkInfos, networkInfo)
	}

	return &NetworkSummary{
		TotalCount:  len(networks),
		UnusedCount: unusedCount,
		Networks:    networkInfos,
	}, nil
}

func (s *Service) getBuildCacheSummary(ctx context.Context) (*BuildCacheSummary, error) {
	diskUsage, err := s.dockerClient.SystemDiskUsage(ctx)
	if err != nil {
		return nil, err
	}

	var buildCacheSize int64
	for _, cache := range diskUsage.BuildCache {
		buildCacheSize += cache.Size
	}

	cacheCount := len(diskUsage.BuildCache)

	return &BuildCacheSummary{
		TotalCount: cacheCount,
		TotalSize:  buildCacheSize,
		Cache:      make([]BuildCacheInfo, 0), // Empty array since we can't list individual entries
	}, nil
}

func (s *Service) pruneImages(ctx context.Context, req *PruneRequest) (*PruneResult, error) {
	filters := s.parseFilters(req.Filters)

	report, err := s.dockerClient.ImagePrune(ctx, req.All, filters)
	if err != nil {
		return &PruneResult{
			Type:  "images",
			Error: err.Error(),
		}, nil
	}

	deletedItems := make([]string, 0)
	for _, img := range report.ImagesDeleted {
		if img.Deleted != "" {
			deletedItems = append(deletedItems, img.Deleted)
		}
		if img.Untagged != "" {
			deletedItems = append(deletedItems, img.Untagged)
		}
	}

	return &PruneResult{
		Type:           "images",
		ItemsDeleted:   deletedItems,
		SpaceReclaimed: int64(report.SpaceReclaimed),
	}, nil
}

func (s *Service) pruneContainers(ctx context.Context, req *PruneRequest) (*PruneResult, error) {
	filters := s.parseFilters(req.Filters)

	report, err := s.dockerClient.ContainerPrune(ctx, filters)
	if err != nil {
		return &PruneResult{
			Type:  "containers",
			Error: err.Error(),
		}, nil
	}

	itemsDeleted := report.ContainersDeleted
	if itemsDeleted == nil {
		itemsDeleted = make([]string, 0)
	}

	return &PruneResult{
		Type:           "containers",
		ItemsDeleted:   itemsDeleted,
		SpaceReclaimed: int64(report.SpaceReclaimed),
	}, nil
}

func (s *Service) pruneVolumes(ctx context.Context, req *PruneRequest) (*PruneResult, error) {
	filters := s.parseFilters(req.Filters)

	if req.All {
		filters["all"] = []string{"1"}
	}

	report, err := s.dockerClient.VolumePrune(ctx, filters)
	if err != nil {
		return &PruneResult{
			Type:  "volumes",
			Error: err.Error(),
		}, nil
	}

	itemsDeleted := report.VolumesDeleted
	if itemsDeleted == nil {
		itemsDeleted = make([]string, 0)
	}

	return &PruneResult{
		Type:           "volumes",
		ItemsDeleted:   itemsDeleted,
		SpaceReclaimed: int64(report.SpaceReclaimed),
	}, nil
}

func (s *Service) pruneNetworks(ctx context.Context, req *PruneRequest) (*PruneResult, error) {
	filters := s.parseFilters(req.Filters)

	report, err := s.dockerClient.NetworkPrune(ctx, filters)
	if err != nil {
		return &PruneResult{
			Type:  "networks",
			Error: err.Error(),
		}, nil
	}

	itemsDeleted := report.NetworksDeleted
	if itemsDeleted == nil {
		itemsDeleted = make([]string, 0)
	}

	return &PruneResult{
		Type:           "networks",
		ItemsDeleted:   itemsDeleted,
		SpaceReclaimed: 0,
	}, nil
}

func (s *Service) pruneBuildCache(ctx context.Context, req *PruneRequest) (*PruneResult, error) {
	filters := s.parseFilters(req.Filters)

	report, err := s.dockerClient.BuildCachePrune(ctx, req.All, filters)
	if err != nil {
		return &PruneResult{
			Type:  "build-cache",
			Error: err.Error(),
		}, nil
	}

	itemsDeleted := report.CachesDeleted
	if itemsDeleted == nil {
		itemsDeleted = make([]string, 0)
	}

	return &PruneResult{
		Type:           "build-cache",
		ItemsDeleted:   itemsDeleted,
		SpaceReclaimed: int64(report.SpaceReclaimed),
	}, nil
}

func (s *Service) pruneSystem(ctx context.Context, req *PruneRequest) (*PruneResult, error) {
	filters := s.parseFilters(req.Filters)

	report, err := s.dockerClient.SystemPrune(ctx, req.All, filters)
	if err != nil {
		return &PruneResult{
			Type:  "system",
			Error: err.Error(),
		}, nil
	}

	allDeleted := make([]string, 0)
	if report.ContainersDeleted != nil {
		allDeleted = append(allDeleted, report.ContainersDeleted...)
	}
	if report.NetworksDeleted != nil {
		allDeleted = append(allDeleted, report.NetworksDeleted...)
	}
	if report.VolumesDeleted != nil {
		allDeleted = append(allDeleted, report.VolumesDeleted...)
	}

	for _, img := range report.ImagesDeleted {
		if img.Deleted != "" {
			allDeleted = append(allDeleted, img.Deleted)
		}
		if img.Untagged != "" {
			allDeleted = append(allDeleted, img.Untagged)
		}
	}

	return &PruneResult{
		Type:           "system",
		ItemsDeleted:   allDeleted,
		SpaceReclaimed: int64(report.SpaceReclaimed),
	}, nil
}

func (s *Service) parseFilters(filtersStr string) map[string][]string {
	filters := make(map[string][]string)

	if filtersStr == "" {
		return filters
	}

	for filter := range strings.SplitSeq(filtersStr, ",") {
		parts := strings.SplitN(strings.TrimSpace(filter), "=", 2)
		if len(parts) == 2 {
			key := strings.TrimSpace(parts[0])
			value := strings.TrimSpace(parts[1])
			filters[key] = append(filters[key], value)
		}
	}

	return filters
}
