package websocket

import (
	"go.uber.org/fx"
)

var Module = fx.Options(
	fx.Provide(NewHub),
)
