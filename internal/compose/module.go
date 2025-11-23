package compose

import (
	"berth-agent/config"
	"berth-agent/internal/logging"

	"go.uber.org/fx"
)

type Module struct {
	Handler *Handler
}

func NewModule(handler *Handler) *Module {
	return &Module{
		Handler: handler,
	}
}

func NewServiceWithConfig(cfg *config.Config, logger *logging.Logger) *Service {
	return NewService(cfg.StackLocation, logger)
}

var FxModule = fx.Module("compose",
	fx.Provide(
		NewServiceWithConfig,
		NewHandler,
		NewModule,
	),
)
