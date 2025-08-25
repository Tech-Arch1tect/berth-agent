package main

import (
	"berth-agent/config"
	"berth-agent/internal/auth"
	"berth-agent/internal/health"
	"berth-agent/internal/stack"
	"context"

	"github.com/labstack/echo/v4"
	echomiddleware "github.com/labstack/echo/v4/middleware"
	"go.uber.org/fx"
)

func main() {
	fx.New(
		config.Module,
		stack.Module,
		health.Module,
		fx.Provide(NewEcho),
		fx.Invoke(RegisterRoutes),
		fx.Invoke(StartServer),
	).Run()
}

func NewEcho() *echo.Echo {
	e := echo.New()
	e.Use(echomiddleware.Logger())
	e.Use(echomiddleware.Recover())
	return e
}

func RegisterRoutes(
	e *echo.Echo,
	cfg *config.Config,
	stackHandler *stack.Handler,
	healthHandler *health.Handler,
) {
	api := e.Group("/api")
	api.Use(auth.TokenMiddleware(cfg.AccessToken))

	api.GET("/health", healthHandler.Health)
	api.GET("/stacks", stackHandler.ListStacks)
	api.GET("/stacks/:name", stackHandler.GetStackDetails)
}

func StartServer(lc fx.Lifecycle, e *echo.Echo, cfg *config.Config) {
	lc.Append(fx.Hook{
		OnStart: func(ctx context.Context) error {
			go func() {
				if err := e.Start(":" + cfg.Port); err != nil {
					e.Logger.Fatal("Server failed to start:", err)
				}
			}()
			return nil
		},
		OnStop: func(ctx context.Context) error {
			return e.Shutdown(ctx)
		},
	})
}
