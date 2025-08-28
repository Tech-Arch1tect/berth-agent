package docker

import (
	"context"
	"fmt"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/volume"
	"github.com/docker/docker/client"
)

type Client struct {
	cli *client.Client
}

func NewClient() (*Client, error) {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return nil, fmt.Errorf("failed to create Docker client: %w", err)
	}

	return &Client{
		cli: cli,
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
