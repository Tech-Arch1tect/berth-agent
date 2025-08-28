package terminal

import (
	"context"

	"go.uber.org/fx"
)

func Module() fx.Option {
	return fx.Module("terminal",
		fx.Provide(NewHandler),
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
