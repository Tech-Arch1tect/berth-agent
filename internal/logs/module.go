package logs

import (
	"berth-agent/config"
	"berth-agent/internal/logging"

	"go.uber.org/fx"
)

var Module = fx.Options(
	fx.Provide(func(cfg *config.Config, logger *logging.Logger) *Service {
		return NewService(cfg.StackLocation, logger)
	}),
	fx.Provide(NewHandler),
)
