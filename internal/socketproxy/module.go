package socketproxy

import (
	"berth-agent/config"
	"berth-agent/internal/logging"
	"os"

	"go.uber.org/fx"
)

func NewProxyFromConfig(cfg *config.Config, logger *logging.Logger) *Proxy {
	socketPath := os.Getenv("PROXY_SOCKET_PATH")
	if socketPath == "" {
		socketPath = "/tmp/docker-proxy.sock"
	}

	dockerSocket := os.Getenv("DOCKER_SOCKET")
	if dockerSocket == "" {
		dockerSocket = "/var/run/docker.sock"
	}

	return NewProxy(socketPath, dockerSocket, logger)
}

var Module = fx.Options(
	fx.Provide(NewProxyFromConfig),
)
