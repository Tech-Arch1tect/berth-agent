package compose

import (
	"berth-agent/config"
	"berth-agent/internal/logging"

	"github.com/labstack/echo/v4"
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

func (m *Module) RegisterRoutes(g *echo.Group) {
	g.PATCH("/compose", m.Handler.UpdateCompose)
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
