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

const (
	LabelComposeProject         = "com.docker.compose.project"
	LabelComposeService         = "com.docker.compose.service"
	LabelComposeContainerNumber = "com.docker.compose.container-number"
	LabelComposeWorkingDir      = "com.docker.compose.project.working_dir"
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

func (c *Client) CreateVolume(ctx context.Context, name, driver string, driverOpts, labels map[string]string) (volume.Volume, error) {
	vol, err := c.cli.VolumeCreate(ctx, volume.CreateOptions{
		Name:       name,
		Driver:     driver,
		DriverOpts: driverOpts,
		Labels:     labels,
	})
	if err != nil {
		return volume.Volume{}, fmt.Errorf("failed to create volume %s: %w", name, err)
	}
	return vol, nil
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

func imagePruneArgs(all bool) filters.Args {
	args := filters.NewArgs()
	if all {
		args.Add("dangling", "false")
	}
	return args
}

func (c *Client) ImagePrune(ctx context.Context, all bool) (image.PruneReport, error) {
	report, err := c.cli.ImagesPrune(ctx, imagePruneArgs(all))
	if err != nil {
		return image.PruneReport{}, fmt.Errorf("failed to prune images: %w", err)
	}
	return report, nil
}

func (c *Client) ContainerPrune(ctx context.Context) (container.PruneReport, error) {
	report, err := c.cli.ContainersPrune(ctx, filters.NewArgs())
	if err != nil {
		return container.PruneReport{}, fmt.Errorf("failed to prune containers: %w", err)
	}
	return report, nil
}

func (c *Client) VolumePrune(ctx context.Context, all bool) (volume.PruneReport, error) {
	args := filters.NewArgs()
	if all {
		args.Add("all", "1")
	}

	report, err := c.cli.VolumesPrune(ctx, args)
	if err != nil {
		return volume.PruneReport{}, fmt.Errorf("failed to prune volumes: %w", err)
	}
	return report, nil
}

func (c *Client) NetworkPrune(ctx context.Context) (network.PruneReport, error) {
	report, err := c.cli.NetworksPrune(ctx, filters.NewArgs())
	if err != nil {
		return network.PruneReport{}, fmt.Errorf("failed to prune networks: %w", err)
	}
	return report, nil
}

func (c *Client) BuildCachePrune(ctx context.Context, all bool) (build.CachePruneReport, error) {
	report, err := c.cli.BuildCachePrune(ctx, build.CachePruneOptions{
		All:     all,
		Filters: filters.NewArgs(),
	})
	if err != nil {
		return build.CachePruneReport{}, fmt.Errorf("failed to prune build cache: %w", err)
	}
	return *report, nil
}

type SystemPruneReport struct {
	ContainersDeleted []string
	NetworksDeleted   []string
	CachesDeleted     []string
	ImagesDeleted     []image.DeleteResponse
	SpaceReclaimed    uint64
}

func (c *Client) SystemPrune(ctx context.Context, all bool) (SystemPruneReport, error) {
	c.logger.Info("starting system prune", zap.Bool("all", all))

	containerReport, err := c.cli.ContainersPrune(ctx, filters.NewArgs())
	if err != nil {
		c.logger.Error("failed to prune containers", zap.Error(err))
		return SystemPruneReport{}, fmt.Errorf("failed to prune containers: %w", err)
	}

	imageReport, err := c.cli.ImagesPrune(ctx, imagePruneArgs(all))
	if err != nil {
		c.logger.Error("failed to prune images", zap.Error(err))
		return SystemPruneReport{}, fmt.Errorf("failed to prune images: %w", err)
	}

	networkReport, err := c.cli.NetworksPrune(ctx, filters.NewArgs())
	if err != nil {
		c.logger.Error("failed to prune networks", zap.Error(err))
		return SystemPruneReport{}, fmt.Errorf("failed to prune networks: %w", err)
	}

	cacheReport, err := c.cli.BuildCachePrune(ctx, build.CachePruneOptions{
		All:     all,
		Filters: filters.NewArgs(),
	})
	if err != nil {
		c.logger.Error("failed to prune build cache", zap.Error(err))
		return SystemPruneReport{}, fmt.Errorf("failed to prune build cache: %w", err)
	}

	totalSpace := containerReport.SpaceReclaimed + imageReport.SpaceReclaimed + cacheReport.SpaceReclaimed
	c.logger.Info("system prune completed",
		zap.Uint64("space_reclaimed_bytes", totalSpace),
		zap.Int("containers_deleted", len(containerReport.ContainersDeleted)),
		zap.Int("images_deleted", len(imageReport.ImagesDeleted)),
		zap.Int("networks_deleted", len(networkReport.NetworksDeleted)),
		zap.Int("build_cache_deleted", len(cacheReport.CachesDeleted)),
	)

	return SystemPruneReport{
		ContainersDeleted: containerReport.ContainersDeleted,
		NetworksDeleted:   networkReport.NetworksDeleted,
		CachesDeleted:     cacheReport.CachesDeleted,
		ImagesDeleted:     imageReport.ImagesDeleted,
		SpaceReclaimed:    totalSpace,
	}, nil
}

func (c *Client) ImageRemove(ctx context.Context, imageID string) ([]image.DeleteResponse, error) {
	responses, err := c.cli.ImageRemove(ctx, imageID, image.RemoveOptions{
		PruneChildren: true,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to remove image %s: %w", imageID, err)
	}
	return responses, nil
}

func (c *Client) ContainerStop(ctx context.Context, containerID string) error {
	if err := c.cli.ContainerStop(ctx, containerID, container.StopOptions{}); err != nil {
		return fmt.Errorf("failed to stop container %s: %w", containerID, err)
	}
	return nil
}

func (c *Client) ContainerStart(ctx context.Context, containerID string) error {
	if err := c.cli.ContainerStart(ctx, containerID, container.StartOptions{}); err != nil {
		return fmt.Errorf("failed to start container %s: %w", containerID, err)
	}
	return nil
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

func (c *Client) VolumeRemove(ctx context.Context, volumeID string) error {
	err := c.cli.VolumeRemove(ctx, volumeID, false)
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
