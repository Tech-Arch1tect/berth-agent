package websocket

import (
	"berth-agent/internal/logging"

	"go.uber.org/fx"
)

var Module = fx.Options(
	fx.Provide(func(logger *logging.Logger) *Hub {
		return NewHub(logger)
	}),
)
