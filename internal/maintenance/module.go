package maintenance

import "go.uber.org/fx"

var Module = fx.Module("maintenance",
	fx.Provide(NewService),
	fx.Provide(NewHandler),
)
