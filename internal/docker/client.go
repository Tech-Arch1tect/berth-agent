package docker

import (
	"context"
	"fmt"

	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/network"
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
