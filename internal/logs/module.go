package logs

import (
	"berth-agent/config"

	"go.uber.org/fx"
)

var Module = fx.Options(
	fx.Provide(func(cfg *config.Config) *Service {
		return NewService(cfg.StackLocation)
	}),
	fx.Provide(NewHandler),
)
