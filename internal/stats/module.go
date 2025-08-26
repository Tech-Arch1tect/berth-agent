package stats

import "go.uber.org/fx"

var Module = fx.Options(
	fx.Provide(NewService),
	fx.Provide(NewHandler),
)
