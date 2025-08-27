package operations

import (
	"berth-agent/config"

	"go.uber.org/fx"
)

var Module = fx.Options(
	fx.Provide(NewServiceWithConfig),
	fx.Provide(NewHandler),
)

func NewServiceWithConfig(cfg *config.Config) *Service {
	return NewService(cfg.StackLocation)
}
