package terminal

import (
	"berth-agent/internal/logging"
	"context"

	"github.com/docker/docker/client"
	"go.uber.org/fx"
)

func Module() fx.Option {
	return fx.Module("terminal",
		fx.Provide(func(dockerClient *client.Client, auditLog *logging.Service) *Handler {
			return NewHandler(dockerClient, auditLog)
		}),
		fx.Invoke(registerLifecycle),
	)
}

func registerLifecycle(lc fx.Lifecycle, handler *Handler) {
	lc.Append(fx.Hook{
		OnStop: func(ctx context.Context) error {
			handler.Shutdown()
			return nil
		},
	})
}
