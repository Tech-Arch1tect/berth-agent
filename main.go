package main

import (
	"berth-agent/config"
	"berth-agent/internal/auth"
	"berth-agent/internal/docker"
	"berth-agent/internal/health"
	"berth-agent/internal/logs"
	"berth-agent/internal/stack"
	"berth-agent/internal/stats"
	"berth-agent/internal/websocket"
	"context"

	"github.com/labstack/echo/v4"
	echomiddleware "github.com/labstack/echo/v4/middleware"
	"go.uber.org/fx"
)

func main() {
	fx.New(
		config.Module,
		stack.Module,
		stats.Module,
		health.Module,
		logs.Module,
		websocket.Module,
		docker.Module,
		fx.Provide(NewEcho),
		fx.Provide(NewWebSocketHandler),
		fx.Provide(NewEventMonitorWithConfig),
		fx.Invoke(RegisterRoutes),
		fx.Invoke(StartServer),
		fx.Invoke(StartWebSocketHub),
		fx.Invoke(StartEventMonitor),
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
	statsHandler *stats.Handler,
	healthHandler *health.Handler,
	logsHandler *logs.Handler,
	wsHandler *websocket.Handler,
) {
	api := e.Group("/api")
	api.Use(auth.TokenMiddleware(cfg.AccessToken))

	api.GET("/health", healthHandler.Health)
	api.GET("/stacks", stackHandler.ListStacks)
	api.GET("/stacks/:name", stackHandler.GetStackDetails)
	api.GET("/stacks/:name/networks", stackHandler.GetStackNetworks)
	api.GET("/stacks/:name/volumes", stackHandler.GetStackVolumes)
	api.GET("/stacks/:name/environment", stackHandler.GetStackEnvironmentVariables)
	api.GET("/stacks/:name/stats", statsHandler.GetStackStats)
	api.GET("/stacks/:stackName/logs", logsHandler.GetStackLogs)
	api.GET("/stacks/:stackName/containers/:containerName/logs", logsHandler.GetContainerLogs)

	e.GET("/ws/agent/status", wsHandler.HandleAgentWebSocket)
}

func NewWebSocketHandler(hub *websocket.Hub, cfg *config.Config) *websocket.Handler {
	return websocket.NewHandler(hub, cfg.AccessToken)
}

func NewEventMonitorWithConfig(hub *websocket.Hub, cfg *config.Config) *docker.EventMonitor {
	return docker.NewEventMonitor(hub, cfg.StackLocation)
}

func StartWebSocketHub(lc fx.Lifecycle, hub *websocket.Hub) {
	lc.Append(fx.Hook{
		OnStart: func(ctx context.Context) error {
			go hub.Run()
			return nil
		},
	})
}

func StartEventMonitor(lc fx.Lifecycle, monitor *docker.EventMonitor) {
	lc.Append(fx.Hook{
		OnStart: func(ctx context.Context) error {
			return monitor.Start()
		},
		OnStop: func(ctx context.Context) error {
			monitor.Stop()
			return nil
		},
	})
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
