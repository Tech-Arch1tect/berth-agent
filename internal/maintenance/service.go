package maintenance

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/tech-arch1tect/berth-agent/internal/docker"
	"github.com/tech-arch1tect/berth-agent/internal/logging"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/build"
	dockercontainer "github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/volume"
	"go.uber.org/zap"
)

const (
	localVolumeDriver    = "local"
	anonymousVolumeLabel = "com.docker.volume.anonymous"
)

type Service struct {
	dockerClient *docker.Client
	logger       *logging.Logger
}

func NewService(dockerClient *docker.Client, logger *logging.Logger) *Service {
	return &Service{
		dockerClient: dockerClient,
		logger:       logger.With(zap.String("service", "maintenance")),
	}
}

type diskUsageResult struct {
	usage types.DiskUsage
	err   error
}

func (s *Service) GetSystemInfo(ctx context.Context) (*MaintenanceInfo, error) {
	s.logger.Debug("collecting system information")

	diskUsageCh := make(chan diskUsageResult, 1)
	go func() {
		usage, err := s.dockerClient.SystemDiskUsage(ctx)
		diskUsageCh <- diskUsageResult{usage: usage, err: err}
	}()

	systemInfo, err := s.getSystemInfo(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get system info: %w", err)
	}

	networks, err := s.dockerClient.ListNetworks(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list networks: %w", err)
	}

	diskUsage := <-diskUsageCh
	if diskUsage.err != nil {
		return nil, fmt.Errorf("failed to get disk usage: %w", diskUsage.err)
	}

	networkSummary := networkSummaryFrom(networks, diskUsage.usage.Containers)

	info := &MaintenanceInfo{
		SystemInfo:        *systemInfo,
		ImageSummary:      imageSummaryFrom(diskUsage.usage.Images, diskUsage.usage.LayersSize),
		ContainerSummary:  containerSummaryFrom(diskUsage.usage.Containers),
		VolumeSummary:     volumeSummaryFrom(diskUsage.usage.Volumes),
		NetworkSummary:    networkSummary,
		BuildCacheSummary: buildCacheSummaryFrom(diskUsage.usage.BuildCache),

		SystemCleanupCovers: systemCleanupCovers,
		LastUpdated:         time.Now(),
	}

	s.logger.Info("system information collected",
		zap.Int("total_images", info.ImageSummary.Total.Count),
		zap.Int("total_containers", info.ContainerSummary.Total.Count),
		zap.Int("total_volumes", info.VolumeSummary.Total.Count),
		zap.Int("total_networks", info.NetworkSummary.TotalCount),
		zap.Int64("images_size_bytes", info.ImageSummary.Total.Size),
		zap.Int64("volumes_size_bytes", info.VolumeSummary.Total.Size),
		zap.Int64("build_cache_size_bytes", info.BuildCacheSummary.Total.Size),
	)

	return info, nil
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
	}, nil
}

func bareID(id string) string {
	if _, hex, found := strings.Cut(id, ":"); found {
		return hex
	}
	return id
}

func imageTagsFrom(repoTags []string) []string {
	tags := make([]string, 0, len(repoTags))
	for _, tag := range repoTags {
		if tag != "" && tag != "<none>:<none>" {
			tags = append(tags, tag)
		}
	}
	return tags
}

func parentImageIDs(images []*image.Summary) map[string]bool {
	parents := make(map[string]bool)
	for _, img := range images {
		if img != nil && img.ParentID != "" {
			parents[img.ParentID] = true
		}
	}
	return parents
}

func imageRemoval(img *image.Summary, tags []string, isParent bool) Removal {
	if img.Containers != 0 {
		return RemovalNever
	}
	if len(tags) > 0 {
		return RemovalWithAll
	}
	if len(img.RepoDigests) == 0 && isParent {
		return RemovalNever
	}
	return RemovalAlways
}

func imageSummaryFrom(images []*image.Summary, layersSize int64) ImageSummary {
	summary := ImageSummary{
		Total:  Amount{Count: len(images), Size: layersSize},
		Images: make([]ImageInfo, 0, len(images)),
	}

	parents := parentImageIDs(images)

	for _, img := range images {
		if img == nil {
			continue
		}

		tags := imageTagsFrom(img.RepoTags)
		unused := img.Containers == 0

		summary.Images = append(summary.Images, ImageInfo{
			ID:         bareID(img.ID),
			Tags:       tags,
			Size:       img.Size,
			SharedSize: img.SharedSize,
			Created:    time.Unix(img.Created, 0),
			Containers: int(img.Containers),
			Dangling:   len(tags) == 0,
			Unused:     unused,
			Removal:    imageRemoval(img, tags, parents[img.ID]),
		})

		if unused {
			summary.UnusedCount++
		}
	}

	sort.Slice(summary.Images, func(i, j int) bool {
		return summary.Images[i].Size > summary.Images[j].Size
	})

	return summary
}

func containerIsPrunable(state dockercontainer.ContainerState) bool {
	switch state {
	case dockercontainer.StateRunning, dockercontainer.StatePaused, dockercontainer.StateRestarting:
		return false
	}
	return true
}

func containerSummaryFrom(containers []*dockercontainer.Summary) ContainerSummary {
	summary := ContainerSummary{
		Containers: make([]ContainerInfo, 0, len(containers)),
	}

	for _, c := range containers {
		if c == nil {
			continue
		}

		if c.State == dockercontainer.StateRunning {
			summary.RunningCount++
		}

		name := ""
		if len(c.Names) > 0 {
			name = strings.TrimPrefix(c.Names[0], "/")
		}

		removal := RemovalNever
		if containerIsPrunable(c.State) {
			removal = RemovalAlways
		}

		summary.Containers = append(summary.Containers, ContainerInfo{
			ID:      bareID(c.ID),
			Name:    name,
			Image:   c.Image,
			State:   c.State,
			Status:  c.Status,
			Created: time.Unix(c.Created, 0),
			Size:    c.SizeRw,
			Labels:  c.Labels,
			Removal: removal,
		})

		summary.Total.Count++
		summary.Total.Size += c.SizeRw
	}

	sort.Slice(summary.Containers, func(i, j int) bool {
		return summary.Containers[i].Name < summary.Containers[j].Name
	})

	return summary
}

func volumeIsAnonymous(vol *volume.Volume) bool {
	_, anonymous := vol.Labels[anonymousVolumeLabel]
	return anonymous
}

func volumeIsUnused(vol *volume.Volume) bool {
	return vol.UsageData != nil && vol.UsageData.RefCount == 0
}

func volumeRemoval(vol *volume.Volume) Removal {
	if vol.Driver != localVolumeDriver || len(vol.Options) > 0 || !volumeIsUnused(vol) {
		return RemovalNever
	}
	if volumeIsAnonymous(vol) {
		return RemovalAlways
	}
	return RemovalWithAll
}

func volumeSize(vol *volume.Volume) int64 {
	if vol.UsageData == nil || vol.UsageData.Size < 0 {
		return 0
	}
	return vol.UsageData.Size
}

func volumeSummaryFrom(volumes []*volume.Volume) VolumeSummary {
	summary := VolumeSummary{
		Volumes: make([]VolumeInfo, 0, len(volumes)),
	}

	for _, vol := range volumes {
		if vol == nil {
			continue
		}

		size := volumeSize(vol)
		unused := volumeIsUnused(vol)
		created, _ := time.Parse(time.RFC3339, vol.CreatedAt)

		summary.Volumes = append(summary.Volumes, VolumeInfo{
			Name:       vol.Name,
			Driver:     vol.Driver,
			Mountpoint: vol.Mountpoint,
			Created:    created,
			Size:       size,
			Labels:     vol.Labels,
			Anonymous:  volumeIsAnonymous(vol),
			Unused:     unused,
			Removal:    volumeRemoval(vol),
		})

		summary.Total.Count++
		summary.Total.Size += size

		if unused {
			summary.Unused.Count++
			summary.Unused.Size += size
		}
	}

	sort.Slice(summary.Volumes, func(i, j int) bool {
		return summary.Volumes[i].Size > summary.Volumes[j].Size
	})

	return summary
}

func networkIsPredefined(name string) bool {
	return name == "bridge" || name == "host" || name == "none"
}

func networkSummaryFrom(networks []network.Summary, containers []*dockercontainer.Summary) NetworkSummary {
	attached := make(map[string]bool)
	for _, c := range containers {
		if c == nil || c.NetworkSettings == nil {
			continue
		}
		for name := range c.NetworkSettings.Networks {
			attached[name] = true
		}
	}

	summary := NetworkSummary{
		TotalCount: len(networks),
		Networks:   make([]NetworkInfo, 0, len(networks)),
	}

	for _, net := range networks {
		unused := !attached[net.Name] && !networkIsPredefined(net.Name)
		removal := RemovalNever
		if unused {
			summary.UnusedCount++
			removal = RemovalAlways
		}

		subnet := ""
		if len(net.IPAM.Config) > 0 {
			subnet = net.IPAM.Config[0].Subnet
		}

		summary.Networks = append(summary.Networks, NetworkInfo{
			ID:       bareID(net.ID),
			Name:     net.Name,
			Driver:   net.Driver,
			Scope:    net.Scope,
			Created:  net.Created,
			Internal: net.Internal,
			Labels:   net.Labels,
			Unused:   unused,
			Subnet:   subnet,
			Removal:  removal,
		})
	}

	sort.Slice(summary.Networks, func(i, j int) bool {
		return summary.Networks[i].Name < summary.Networks[j].Name
	})

	return summary
}

func buildCacheRemoval(record *build.CacheRecord) Removal {
	switch {
	case record.InUse:
		return RemovalNever
	case record.Shared:
		return RemovalWithAll
	default:
		return RemovalAlways
	}
}

func buildCacheSummaryFrom(records []*build.CacheRecord) BuildCacheSummary {
	summary := BuildCacheSummary{
		Cache: make([]BuildCacheInfo, 0, len(records)),
	}

	for _, record := range records {
		if record == nil {
			continue
		}

		lastUsed := time.Time{}
		if record.LastUsedAt != nil {
			lastUsed = *record.LastUsedAt
		}

		summary.Cache = append(summary.Cache, BuildCacheInfo{
			ID:          record.ID,
			Type:        record.Type,
			Description: record.Description,
			Size:        record.Size,
			Created:     record.CreatedAt,
			LastUsed:    lastUsed,
			UsageCount:  record.UsageCount,
			InUse:       record.InUse,
			Shared:      record.Shared,
			Removal:     buildCacheRemoval(record),
		})

		summary.Total.Count++
		summary.Total.Size += record.Size
	}

	sort.Slice(summary.Cache, func(i, j int) bool {
		return summary.Cache[i].Size > summary.Cache[j].Size
	})

	return summary
}

var systemCleanupCovers = []string{"images", "containers", "networks", "build_cache"}

func (s *Service) PruneDocker(ctx context.Context, req *PruneRequest) (*PruneResult, error) {
	switch req.Type {
	case "images":
		return s.pruneImages(ctx, req.All)
	case "containers":
		return s.pruneContainers(ctx)
	case "volumes":
		return s.pruneVolumes(ctx, req.All)
	case "networks":
		return s.pruneNetworks(ctx)
	case "build-cache":
		return s.pruneBuildCache(ctx, req.All)
	case "system":
		return s.pruneSystem(ctx, req.All)
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
	s.logger.Debug("deleting image", zap.String("image_id", imageID))

	_, err := s.dockerClient.ImageRemove(ctx, imageID, true, false)
	if err != nil {
		s.logger.Error("failed to delete image",
			zap.String("image_id", imageID),
			zap.Error(err),
		)
		return &DeleteResult{
			Type:    "image",
			ID:      imageID,
			Success: false,
			Error:   err.Error(),
		}, nil
	}

	s.logger.Info("image deleted successfully", zap.String("image_id", imageID))

	return &DeleteResult{
		Type:    "image",
		ID:      imageID,
		Success: true,
	}, nil
}

func (s *Service) deleteContainer(ctx context.Context, containerID string) (*DeleteResult, error) {
	s.logger.Debug("deleting container", zap.String("container_id", containerID))

	err := s.dockerClient.ContainerRemove(ctx, containerID, true, true, true)
	if err != nil {
		s.logger.Error("failed to delete container",
			zap.String("container_id", containerID),
			zap.Error(err),
		)
		return &DeleteResult{
			Type:    "container",
			ID:      containerID,
			Success: false,
			Error:   err.Error(),
		}, nil
	}

	s.logger.Info("container deleted successfully", zap.String("container_id", containerID))

	return &DeleteResult{
		Type:    "container",
		ID:      containerID,
		Success: true,
	}, nil
}

func (s *Service) deleteVolume(ctx context.Context, volumeName string) (*DeleteResult, error) {
	s.logger.Debug("deleting volume", zap.String("volume_name", volumeName))

	err := s.dockerClient.VolumeRemove(ctx, volumeName, true)
	if err != nil {
		s.logger.Error("failed to delete volume",
			zap.String("volume_name", volumeName),
			zap.Error(err),
		)
		return &DeleteResult{
			Type:    "volume",
			ID:      volumeName,
			Success: false,
			Error:   err.Error(),
		}, nil
	}

	s.logger.Info("volume deleted successfully", zap.String("volume_name", volumeName))

	return &DeleteResult{
		Type:    "volume",
		ID:      volumeName,
		Success: true,
	}, nil
}

func (s *Service) deleteNetwork(ctx context.Context, networkID string) (*DeleteResult, error) {
	s.logger.Debug("deleting network", zap.String("network_id", networkID))

	err := s.dockerClient.NetworkRemove(ctx, networkID)
	if err != nil {
		s.logger.Error("failed to delete network",
			zap.String("network_id", networkID),
			zap.Error(err),
		)
		return &DeleteResult{
			Type:    "network",
			ID:      networkID,
			Success: false,
			Error:   err.Error(),
		}, nil
	}

	s.logger.Info("network deleted successfully", zap.String("network_id", networkID))

	return &DeleteResult{
		Type:    "network",
		ID:      networkID,
		Success: true,
	}, nil
}

func imagePruneItems(deleted []image.DeleteResponse) []string {
	items := make([]string, 0, len(deleted))
	for _, img := range deleted {
		if img.Deleted != "" {
			items = append(items, img.Deleted)
		}
		if img.Untagged != "" {
			items = append(items, img.Untagged)
		}
	}
	return items
}

func itemsOrEmpty(items []string) []string {
	if items == nil {
		return make([]string, 0)
	}
	return items
}

func (s *Service) pruneImages(ctx context.Context, all bool) (*PruneResult, error) {
	s.logger.Debug("pruning images", zap.Bool("all", all))

	report, err := s.dockerClient.ImagePrune(ctx, all)
	if err != nil {
		s.logger.Error("failed to prune images",
			zap.Bool("all", all),
			zap.Error(err),
		)
		return &PruneResult{Type: "images", Error: err.Error()}, nil
	}

	deleted := imagePruneItems(report.ImagesDeleted)

	s.logger.Info("images pruned successfully",
		zap.Int("items_deleted", len(deleted)),
		zap.Uint64("space_reclaimed_bytes", report.SpaceReclaimed),
	)

	return &PruneResult{
		Type:           "images",
		ItemsDeleted:   deleted,
		SpaceReclaimed: int64(report.SpaceReclaimed),
	}, nil
}

func (s *Service) pruneContainers(ctx context.Context) (*PruneResult, error) {
	s.logger.Debug("pruning containers")

	report, err := s.dockerClient.ContainerPrune(ctx)
	if err != nil {
		s.logger.Error("failed to prune containers", zap.Error(err))
		return &PruneResult{Type: "containers", Error: err.Error()}, nil
	}

	s.logger.Info("containers pruned successfully",
		zap.Int("items_deleted", len(report.ContainersDeleted)),
		zap.Uint64("space_reclaimed_bytes", report.SpaceReclaimed),
	)

	return &PruneResult{
		Type:           "containers",
		ItemsDeleted:   itemsOrEmpty(report.ContainersDeleted),
		SpaceReclaimed: int64(report.SpaceReclaimed),
	}, nil
}

func (s *Service) pruneVolumes(ctx context.Context, all bool) (*PruneResult, error) {
	s.logger.Debug("pruning volumes", zap.Bool("all", all))

	report, err := s.dockerClient.VolumePrune(ctx, all)
	if err != nil {
		s.logger.Error("failed to prune volumes",
			zap.Bool("all", all),
			zap.Error(err),
		)
		return &PruneResult{Type: "volumes", Error: err.Error()}, nil
	}

	s.logger.Info("volumes pruned successfully",
		zap.Int("items_deleted", len(report.VolumesDeleted)),
		zap.Uint64("space_reclaimed_bytes", report.SpaceReclaimed),
	)

	return &PruneResult{
		Type:           "volumes",
		ItemsDeleted:   itemsOrEmpty(report.VolumesDeleted),
		SpaceReclaimed: int64(report.SpaceReclaimed),
	}, nil
}

func (s *Service) pruneNetworks(ctx context.Context) (*PruneResult, error) {
	s.logger.Debug("pruning networks")

	report, err := s.dockerClient.NetworkPrune(ctx)
	if err != nil {
		s.logger.Error("failed to prune networks", zap.Error(err))
		return &PruneResult{Type: "networks", Error: err.Error()}, nil
	}

	s.logger.Info("networks pruned successfully", zap.Int("items_deleted", len(report.NetworksDeleted)))

	return &PruneResult{
		Type:         "networks",
		ItemsDeleted: itemsOrEmpty(report.NetworksDeleted),
	}, nil
}

func (s *Service) pruneBuildCache(ctx context.Context, all bool) (*PruneResult, error) {
	s.logger.Debug("pruning build cache", zap.Bool("all", all))

	report, err := s.dockerClient.BuildCachePrune(ctx, all)
	if err != nil {
		s.logger.Error("failed to prune build cache",
			zap.Bool("all", all),
			zap.Error(err),
		)
		return &PruneResult{Type: "build-cache", Error: err.Error()}, nil
	}

	s.logger.Info("build cache pruned successfully",
		zap.Int("items_deleted", len(report.CachesDeleted)),
		zap.Uint64("space_reclaimed_bytes", report.SpaceReclaimed),
	)

	return &PruneResult{
		Type:           "build-cache",
		ItemsDeleted:   itemsOrEmpty(report.CachesDeleted),
		SpaceReclaimed: int64(report.SpaceReclaimed),
	}, nil
}

func (s *Service) pruneSystem(ctx context.Context, all bool) (*PruneResult, error) {
	s.logger.Debug("pruning system", zap.Bool("all", all))

	report, err := s.dockerClient.SystemPrune(ctx, all)
	if err != nil {
		s.logger.Error("failed to prune system",
			zap.Bool("all", all),
			zap.Error(err),
		)
		return &PruneResult{Type: "system", Error: err.Error()}, nil
	}

	deleted := make([]string, 0)
	deleted = append(deleted, report.ContainersDeleted...)
	deleted = append(deleted, report.NetworksDeleted...)
	deleted = append(deleted, report.CachesDeleted...)
	deleted = append(deleted, imagePruneItems(report.ImagesDeleted)...)

	s.logger.Info("system pruned successfully",
		zap.Int("items_deleted", len(deleted)),
		zap.Uint64("space_reclaimed_bytes", report.SpaceReclaimed),
		zap.Int("containers_deleted", len(report.ContainersDeleted)),
		zap.Int("networks_deleted", len(report.NetworksDeleted)),
		zap.Int("images_deleted", len(report.ImagesDeleted)),
		zap.Int("build_cache_deleted", len(report.CachesDeleted)),
	)

	return &PruneResult{
		Type:           "system",
		ItemsDeleted:   deleted,
		SpaceReclaimed: int64(report.SpaceReclaimed),
	}, nil
}
