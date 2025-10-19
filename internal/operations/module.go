package operations

import (
	"berth-agent/config"
	"berth-agent/internal/logging"

	"go.uber.org/fx"
)

var Module = fx.Options(
	fx.Provide(NewServiceWithConfig),
	fx.Provide(NewHandler),
)

func NewServiceWithConfig(cfg *config.Config, logger *logging.Logger) *Service {
	return NewService(cfg.StackLocation, cfg.AccessToken, logger)
}
