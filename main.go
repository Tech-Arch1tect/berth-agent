package main

import (
	"berth-agent/config"
	"berth-agent/internal/auth"
	"berth-agent/internal/docker"
	"berth-agent/internal/files"
	"berth-agent/internal/health"
	"berth-agent/internal/logs"
	"berth-agent/internal/maintenance"
	"berth-agent/internal/operations"
	"berth-agent/internal/sidecar"
	"berth-agent/internal/ssl"
	"berth-agent/internal/stack"
	"berth-agent/internal/stats"
	"berth-agent/internal/terminal"
	"berth-agent/internal/websocket"
	"context"
	"os"

	"github.com/labstack/echo/v4"
	echomiddleware "github.com/labstack/echo/v4/middleware"
	"go.uber.org/fx"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "sidecar" {
		runSidecar()
		return
	}

	runAgent()
}

func runAgent() {
	fx.New(
		config.Module,
		ssl.Module,
		stack.Module,
		stats.Module,
		maintenance.Module,
		health.Module,
		logs.Module,
		operations.Module,
		websocket.Module,
		terminal.Module(),
		files.Module,
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

func runSidecar() {
	fx.New(
		config.Module,
		ssl.Module,
		sidecar.Module,
		fx.Provide(NewSidecarEcho),
		fx.Invoke(RegisterSidecarRoutes),
		fx.Invoke(StartSidecarServer),
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
	maintenanceHandler *maintenance.Handler,
	healthHandler *health.Handler,
	logsHandler *logs.Handler,
	operationsHandler *operations.Handler,
	wsHandler *websocket.Handler,
	terminalHandler *terminal.Handler,
	filesHandler *files.Handler,
) {
	api := e.Group("/api")
	api.Use(auth.TokenMiddleware(cfg.AccessToken))

	api.GET("/health", healthHandler.Health)
	api.GET("/stacks", stackHandler.ListStacks)
	api.GET("/stacks/summary", stackHandler.GetStacksSummary)
	api.GET("/stacks/:name", stackHandler.GetStackDetails)
	api.GET("/stacks/:name/networks", stackHandler.GetStackNetworks)
	api.GET("/stacks/:name/volumes", stackHandler.GetStackVolumes)
	api.GET("/stacks/:name/environment", stackHandler.GetStackEnvironmentVariables)
	api.GET("/stacks/:name/stats", statsHandler.GetStackStats)
	api.GET("/stacks/:stackName/logs", logsHandler.GetStackLogs)
	api.GET("/stacks/:stackName/containers/:containerName/logs", logsHandler.GetContainerLogs)

	api.POST("/stacks/:stackName/operations", operationsHandler.StartOperation)
	api.GET("/operations/:operationId/stream", operationsHandler.StreamOperation)
	api.GET("/operations/:operationId/status", operationsHandler.GetOperationStatus)

	api.GET("/stacks/:stackName/files", filesHandler.ListDirectory)
	api.GET("/stacks/:stackName/files/read", filesHandler.ReadFile)
	api.POST("/stacks/:stackName/files/write", filesHandler.WriteFile)
	api.POST("/stacks/:stackName/files/upload", filesHandler.UploadFile)
	api.POST("/stacks/:stackName/files/mkdir", filesHandler.CreateDirectory)
	api.DELETE("/stacks/:stackName/files/delete", filesHandler.Delete)
	api.POST("/stacks/:stackName/files/rename", filesHandler.Rename)
	api.POST("/stacks/:stackName/files/copy", filesHandler.Copy)
	api.POST("/stacks/:stackName/files/chmod", filesHandler.Chmod)
	api.POST("/stacks/:stackName/files/chown", filesHandler.Chown)
	api.GET("/stacks/:stackName/files/download", filesHandler.DownloadFile)

	api.GET("/maintenance/info", maintenanceHandler.GetSystemInfo)
	api.POST("/maintenance/prune", maintenanceHandler.PruneDocker)
	api.DELETE("/maintenance/resource", maintenanceHandler.DeleteResource)

	e.GET("/ws/agent/status", wsHandler.HandleAgentWebSocket)
	e.GET("/ws/terminal", terminalHandler.HandleTerminalWebSocket)
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

func StartServer(lc fx.Lifecycle, e *echo.Echo, cfg *config.Config, certManager *ssl.CertificateManager) {
	lc.Append(fx.Hook{
		OnStart: func(ctx context.Context) error {
			certFile, keyFile, err := certManager.EnsureCertificates()
			if err != nil {
				e.Logger.Fatal("Failed to setup SSL certificates:", err)
				return err
			}

			go func() {
				if err := e.StartTLS(":"+cfg.Port, certFile, keyFile); err != nil {
					e.Logger.Fatal("HTTPS Server failed to start:", err)
				}
			}()
			return nil
		},
		OnStop: func(ctx context.Context) error {
			return e.Shutdown(ctx)
		},
	})
}

func NewSidecarEcho() *echo.Echo {
	e := echo.New()
	e.Use(echomiddleware.Logger())
	e.Use(echomiddleware.Recover())
	return e
}

func RegisterSidecarRoutes(e *echo.Echo, handler *sidecar.Handler, cfg *config.Config) {
	api := e.Group("")
	api.Use(auth.TokenMiddleware(cfg.AccessToken))

	api.POST("/operation", handler.HandleOperation)
	api.GET("/health", handler.Health)
}

func StartSidecarServer(lc fx.Lifecycle, e *echo.Echo, certManager *ssl.CertificateManager) {
	lc.Append(fx.Hook{
		OnStart: func(ctx context.Context) error {
			certFile, keyFile, err := certManager.EnsureCertificates()
			if err != nil {
				e.Logger.Fatal("Failed to setup SSL certificates:", err)
				return err
			}

			go func() {
				if err := e.StartTLS(":8081", certFile, keyFile); err != nil {
					e.Logger.Fatal("Sidecar HTTPS server failed to start:", err)
				}
			}()
			return nil
		},
		OnStop: func(ctx context.Context) error {
			return e.Shutdown(ctx)
		},
	})
}
