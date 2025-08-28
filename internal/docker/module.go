package docker

import (
	"github.com/docker/docker/client"
	"go.uber.org/fx"
)

var Module = fx.Options(
	fx.Provide(NewClient),
	fx.Provide(NewRawClient),
)

func NewRawClient() (*client.Client, error) {
	return client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
}
