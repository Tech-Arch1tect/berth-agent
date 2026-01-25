package docker

import (
	"context"
	"fmt"
	"github.com/tech-arch1tect/berth-agent/internal/logging"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/build"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/system"
	"github.com/docker/docker/api/types/volume"
	"github.com/docker/docker/client"
	"go.uber.org/zap"
)

type Client struct {
	cli    *client.Client
	logger *logging.Logger
}

func NewClient(logger *logging.Logger) (*Client, error) {
	logger.Debug("creating docker client")
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		logger.Error("failed to create docker client", zap.Error(err))
		return nil, fmt.Errorf("failed to create Docker client: %w", err)
	}

	logger.Info("docker client created successfully")
	return &Client{
		cli:    cli,
		logger: logger,
	}, nil
}

func (c *Client) Close() error {
	return c.cli.Close()
}

func (c *Client) ListNetworks(ctx context.Context) ([]network.Summary, error) {
	networks, err := c.cli.NetworkList(ctx, network.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to list networks: %w", err)
	}
	return networks, nil
}

func (c *Client) InspectNetwork(ctx context.Context, networkID string) (network.Inspect, error) {
	networkResource, err := c.cli.NetworkInspect(ctx, networkID, network.InspectOptions{})
	if err != nil {
		return network.Inspect{}, fmt.Errorf("failed to inspect network %s: %w", networkID, err)
	}
	return networkResource, nil
}

func (c *Client) GetNetworksByLabels(ctx context.Context, labels map[string]string) ([]network.Summary, error) {
	args := filters.NewArgs()
	for key, value := range labels {
		args.Add("label", fmt.Sprintf("%s=%s", key, value))
	}

	networks, err := c.cli.NetworkList(ctx, network.ListOptions{
		Filters: args,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list networks with labels: %w", err)
	}
	return networks, nil
}

func (c *Client) ListVolumes(ctx context.Context) (volume.ListResponse, error) {
	volumes, err := c.cli.VolumeList(ctx, volume.ListOptions{})
	if err != nil {
		return volume.ListResponse{}, fmt.Errorf("failed to list volumes: %w", err)
	}
	return volumes, nil
}

func (c *Client) InspectVolume(ctx context.Context, volumeID string) (*volume.Volume, error) {
	vol, err := c.cli.VolumeInspect(ctx, volumeID)
	if err != nil {
		return nil, fmt.Errorf("failed to inspect volume %s: %w", volumeID, err)
	}
	return &vol, nil
}

func (c *Client) GetVolumesByLabels(ctx context.Context, labels map[string]string) ([]*volume.Volume, error) {
	args := filters.NewArgs()
	for key, value := range labels {
		args.Add("label", fmt.Sprintf("%s=%s", key, value))
	}

	volumes, err := c.cli.VolumeList(ctx, volume.ListOptions{
		Filters: args,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list volumes with labels: %w", err)
	}
	return volumes.Volumes, nil
}

func (c *Client) ContainerInspect(ctx context.Context, containerID string) (container.InspectResponse, error) {
	containerInfo, err := c.cli.ContainerInspect(ctx, containerID)
	if err != nil {
		return container.InspectResponse{}, fmt.Errorf("failed to inspect container %s: %w", containerID, err)
	}
	return containerInfo, nil
}

func (c *Client) ContainerList(ctx context.Context, filterLabels map[string][]string) ([]container.Summary, error) {
	args := filters.NewArgs()
	for key, values := range filterLabels {
		for _, value := range values {
			args.Add(key, value)
		}
	}

	containers, err := c.cli.ContainerList(ctx, container.ListOptions{
		All:     true,
		Filters: args,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list containers: %w", err)
	}
	return containers, nil
}

func (c *Client) ContainerStats(ctx context.Context, containerID string, stream bool) (container.StatsResponseReader, error) {
	stats, err := c.cli.ContainerStats(ctx, containerID, stream)
	if err != nil {
		return container.StatsResponseReader{}, fmt.Errorf("failed to get container stats %s: %w", containerID, err)
	}
	return stats, nil
}

func (c *Client) SystemInfo(ctx context.Context) (system.Info, error) {
	info, err := c.cli.Info(ctx)
	if err != nil {
		return system.Info{}, fmt.Errorf("failed to get system info: %w", err)
	}
	return info, nil
}

func (c *Client) SystemVersion(ctx context.Context) (types.Version, error) {
	version, err := c.cli.ServerVersion(ctx)
	if err != nil {
		return types.Version{}, fmt.Errorf("failed to get server version: %w", err)
	}
	return version, nil
}

func (c *Client) SystemDiskUsage(ctx context.Context) (types.DiskUsage, error) {
	diskUsage, err := c.cli.DiskUsage(ctx, types.DiskUsageOptions{})
	if err != nil {
		return types.DiskUsage{}, fmt.Errorf("failed to get disk usage: %w", err)
	}
	return diskUsage, nil
}

func (c *Client) ImageList(ctx context.Context) ([]image.Summary, error) {
	images, err := c.cli.ImageList(ctx, image.ListOptions{All: true})
	if err != nil {
		return nil, fmt.Errorf("failed to list images: %w", err)
	}
	return images, nil
}

func (c *Client) ImageInspect(ctx context.Context, imageID string) (image.InspectResponse, error) {
	imageInfo, _, err := c.cli.ImageInspectWithRaw(ctx, imageID)
	if err != nil {
		return image.InspectResponse{}, fmt.Errorf("failed to inspect image %s: %w", imageID, err)
	}
	return imageInfo, nil
}

func (c *Client) ImageHistory(ctx context.Context, imageID string) ([]image.HistoryResponseItem, error) {
	history, err := c.cli.ImageHistory(ctx, imageID)
	if err != nil {
		return nil, fmt.Errorf("failed to get image history for %s: %w", imageID, err)
	}
	return history, nil
}

func (c *Client) ContainerListAll(ctx context.Context) ([]container.Summary, error) {
	containers, err := c.cli.ContainerList(ctx, container.ListOptions{All: true})
	if err != nil {
		return nil, fmt.Errorf("failed to list all containers: %w", err)
	}
	return containers, nil
}

func (c *Client) ImagePrune(ctx context.Context, all bool, filterMap map[string][]string) (image.PruneReport, error) {
	args := filters.NewArgs()
	for key, values := range filterMap {
		for _, value := range values {
			args.Add(key, value)
		}
	}

	if all {
		args.Add("dangling", "false")
	}

	report, err := c.cli.ImagesPrune(ctx, args)
	if err != nil {
		return image.PruneReport{}, fmt.Errorf("failed to prune images: %w", err)
	}
	return report, nil
}

func (c *Client) ContainerPrune(ctx context.Context, filterMap map[string][]string) (container.PruneReport, error) {
	args := filters.NewArgs()
	for key, values := range filterMap {
		for _, value := range values {
			args.Add(key, value)
		}
	}

	report, err := c.cli.ContainersPrune(ctx, args)
	if err != nil {
		return container.PruneReport{}, fmt.Errorf("failed to prune containers: %w", err)
	}
	return report, nil
}

func (c *Client) VolumePrune(ctx context.Context, filterMap map[string][]string) (volume.PruneReport, error) {
	args := filters.NewArgs()
	for key, values := range filterMap {
		for _, value := range values {
			args.Add(key, value)
		}
	}

	report, err := c.cli.VolumesPrune(ctx, args)
	if err != nil {
		return volume.PruneReport{}, fmt.Errorf("failed to prune volumes: %w", err)
	}
	return report, nil
}

func (c *Client) NetworkPrune(ctx context.Context, filterMap map[string][]string) (network.PruneReport, error) {
	args := filters.NewArgs()
	for key, values := range filterMap {
		for _, value := range values {
			args.Add(key, value)
		}
	}

	report, err := c.cli.NetworksPrune(ctx, args)
	if err != nil {
		return network.PruneReport{}, fmt.Errorf("failed to prune networks: %w", err)
	}
	return report, nil
}

func (c *Client) BuildCachePrune(ctx context.Context, all bool, filterMap map[string][]string) (build.CachePruneReport, error) {
	args := filters.NewArgs()
	for key, values := range filterMap {
		for _, value := range values {
			args.Add(key, value)
		}
	}

	opts := build.CachePruneOptions{
		All:     all,
		Filters: args,
	}

	report, err := c.cli.BuildCachePrune(ctx, opts)
	if err != nil {
		return build.CachePruneReport{}, fmt.Errorf("failed to prune build cache: %w", err)
	}
	return *report, nil
}

type SystemPruneReport struct {
	ContainersDeleted []string
	VolumesDeleted    []string
	NetworksDeleted   []string
	ImagesDeleted     []image.DeleteResponse
	SpaceReclaimed    uint64
}

func (c *Client) SystemPrune(ctx context.Context, all bool, filterMap map[string][]string) (SystemPruneReport, error) {
	c.logger.Info("starting system prune", zap.Bool("all", all))
	args := filters.NewArgs()
	for key, values := range filterMap {
		for _, value := range values {
			args.Add(key, value)
		}
	}

	containerReport, err := c.cli.ContainersPrune(ctx, args)
	if err != nil {
		c.logger.Error("failed to prune containers", zap.Error(err))
		return SystemPruneReport{}, fmt.Errorf("failed to prune containers: %w", err)
	}
	c.logger.Debug("containers pruned", zap.Int("deleted", len(containerReport.ContainersDeleted)))

	imageReport, err := c.cli.ImagesPrune(ctx, args)
	if err != nil {
		c.logger.Error("failed to prune images", zap.Error(err))
		return SystemPruneReport{}, fmt.Errorf("failed to prune images: %w", err)
	}
	c.logger.Debug("images pruned", zap.Int("deleted", len(imageReport.ImagesDeleted)))

	volumeReport, err := c.cli.VolumesPrune(ctx, args)
	if err != nil {
		c.logger.Error("failed to prune volumes", zap.Error(err))
		return SystemPruneReport{}, fmt.Errorf("failed to prune volumes: %w", err)
	}
	c.logger.Debug("volumes pruned", zap.Int("deleted", len(volumeReport.VolumesDeleted)))

	networkReport, err := c.cli.NetworksPrune(ctx, args)
	if err != nil {
		c.logger.Error("failed to prune networks", zap.Error(err))
		return SystemPruneReport{}, fmt.Errorf("failed to prune networks: %w", err)
	}
	c.logger.Debug("networks pruned", zap.Int("deleted", len(networkReport.NetworksDeleted)))

	totalSpace := containerReport.SpaceReclaimed + imageReport.SpaceReclaimed + volumeReport.SpaceReclaimed
	c.logger.Info("system prune completed",
		zap.Uint64("space_reclaimed_bytes", totalSpace),
		zap.Int("containers_deleted", len(containerReport.ContainersDeleted)),
		zap.Int("images_deleted", len(imageReport.ImagesDeleted)),
		zap.Int("volumes_deleted", len(volumeReport.VolumesDeleted)),
		zap.Int("networks_deleted", len(networkReport.NetworksDeleted)),
	)

	return SystemPruneReport{
		ContainersDeleted: containerReport.ContainersDeleted,
		VolumesDeleted:    volumeReport.VolumesDeleted,
		NetworksDeleted:   networkReport.NetworksDeleted,
		ImagesDeleted:     imageReport.ImagesDeleted,
		SpaceReclaimed:    totalSpace,
	}, nil
}

func (c *Client) ImageRemove(ctx context.Context, imageID string, force, noPrune bool) ([]image.DeleteResponse, error) {
	responses, err := c.cli.ImageRemove(ctx, imageID, image.RemoveOptions{
		Force:         force,
		PruneChildren: !noPrune,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to remove image %s: %w", imageID, err)
	}
	return responses, nil
}

func (c *Client) ContainerRemove(ctx context.Context, containerID string, removeVolumes, removeLinks, force bool) error {
	err := c.cli.ContainerRemove(ctx, containerID, container.RemoveOptions{
		RemoveVolumes: removeVolumes,
		RemoveLinks:   removeLinks,
		Force:         force,
	})
	if err != nil {
		return fmt.Errorf("failed to remove container %s: %w", containerID, err)
	}
	return nil
}

func (c *Client) VolumeRemove(ctx context.Context, volumeID string, force bool) error {
	err := c.cli.VolumeRemove(ctx, volumeID, force)
	if err != nil {
		return fmt.Errorf("failed to remove volume %s: %w", volumeID, err)
	}
	return nil
}

func (c *Client) NetworkRemove(ctx context.Context, networkID string) error {
	err := c.cli.NetworkRemove(ctx, networkID)
	if err != nil {
		return fmt.Errorf("failed to remove network %s: %w", networkID, err)
	}
	return nil
}
