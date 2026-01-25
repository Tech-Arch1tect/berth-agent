package main

import (
	"context"
	"github.com/tech-arch1tect/berth-agent/config"
	"github.com/tech-arch1tect/berth-agent/internal/audit"
	"github.com/tech-arch1tect/berth-agent/internal/auth"
	"github.com/tech-arch1tect/berth-agent/internal/composeeditor"
	"github.com/tech-arch1tect/berth-agent/internal/docker"
	"github.com/tech-arch1tect/berth-agent/internal/files"
	"github.com/tech-arch1tect/berth-agent/internal/grypescanner"
	"github.com/tech-arch1tect/berth-agent/internal/health"
	"github.com/tech-arch1tect/berth-agent/internal/images"
	"github.com/tech-arch1tect/berth-agent/internal/logging"
	"github.com/tech-arch1tect/berth-agent/internal/logs"
	"github.com/tech-arch1tect/berth-agent/internal/maintenance"
	"github.com/tech-arch1tect/berth-agent/internal/operations"
	"github.com/tech-arch1tect/berth-agent/internal/sidecar"
	"github.com/tech-arch1tect/berth-agent/internal/socketproxy"
	"github.com/tech-arch1tect/berth-agent/internal/ssl"
	"github.com/tech-arch1tect/berth-agent/internal/stack"
	"github.com/tech-arch1tect/berth-agent/internal/stats"
	"github.com/tech-arch1tect/berth-agent/internal/terminal"
	"github.com/tech-arch1tect/berth-agent/internal/vulnscan"
	"github.com/tech-arch1tect/berth-agent/internal/websocket"
	"os"
	"time"

	"github.com/labstack/echo/v4"
	echomiddleware "github.com/labstack/echo/v4/middleware"
	"go.uber.org/fx"
	"go.uber.org/zap"
)

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "sidecar":
			runSidecar()
			return
		case "socket-proxy":
			runSocketProxy()
			return
		case "grype-scanner":
			runGrypeScanner()
			return
		}
	}

	runAgent()
}

func runAgent() {
	app := fx.New(
		config.Module,
		logging.Module,
		audit.Module,
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
		images.Module,
		composeeditor.Module,
		vulnscan.Module,
		fx.Provide(NewEcho),
		fx.Provide(NewWebSocketHandler),
		fx.Provide(NewEventMonitorWithConfig),
		fx.Invoke(RegisterRoutes),
		fx.Invoke(StartServer),
		fx.Invoke(StartWebSocketHub),
		fx.Invoke(StartEventMonitor),
		fx.Invoke(LogAgentStartup),
	)
	app.Run()
}

func runSidecar() {
	app := fx.New(
		config.Module,
		logging.Module,
		ssl.Module,
		sidecar.Module,
		fx.Provide(NewSidecarEcho),
		fx.Invoke(RegisterSidecarRoutes),
		fx.Invoke(StartSidecarServer),
		fx.Invoke(LogSidecarStartup),
	)
	app.Run()
}

func NewEcho(loggingService *logging.Service) *echo.Echo {
	e := echo.New()
	e.Use(echomiddleware.Recover())
	e.Use(logging.RequestLoggingMiddleware(loggingService))
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
	imagesHandler *images.Handler,
	composeEditorHandler *composeeditor.Handler,
	vulnscanHandler *vulnscan.Handler,
	logger *logging.Logger,
) {
	api := e.Group("/api")
	api.Use(auth.TokenMiddleware(cfg.AccessToken, logger))

	api.GET("/health", healthHandler.Health)
	api.GET("/stacks", stackHandler.ListStacks)
	api.POST("/stacks", stackHandler.CreateStack)
	api.GET("/stacks/summary", stackHandler.GetStacksSummary)
	api.GET("/stacks/:name", stackHandler.GetStackDetails)
	api.GET("/stacks/:name/networks", stackHandler.GetStackNetworks)
	api.GET("/stacks/:name/volumes", stackHandler.GetStackVolumes)
	api.GET("/stacks/:name/environment", stackHandler.GetStackEnvironmentVariables)
	api.GET("/stacks/:name/images", stackHandler.GetContainerImageDetails)
	api.GET("/stacks/:name/compose", composeEditorHandler.GetComposeConfig)
	api.PATCH("/stacks/:name/compose", composeEditorHandler.UpdateCompose)
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
	api.GET("/stacks/:stackName/files/stats", filesHandler.GetDirectoryStats)

	api.POST("/images/check-updates", imagesHandler.CheckImageUpdates)

	api.GET("/vulnscan/status", vulnscanHandler.GetScannerStatus)
	api.POST("/stacks/:stackName/scan", vulnscanHandler.StartScan)
	api.GET("/scans/:scanId", vulnscanHandler.GetScan)

	api.GET("/maintenance/info", maintenanceHandler.GetSystemInfo)
	api.POST("/maintenance/prune", maintenanceHandler.PruneDocker)
	api.DELETE("/maintenance/resource", maintenanceHandler.DeleteResource)

	e.GET("/ws/agent/status", wsHandler.HandleAgentWebSocket)
	e.GET("/ws/terminal", terminalHandler.HandleTerminalWebSocket)
}

func NewWebSocketHandler(hub *websocket.Hub, cfg *config.Config) *websocket.Handler {
	return websocket.NewHandler(hub, cfg.AccessToken)
}

func NewEventMonitorWithConfig(hub *websocket.Hub, cfg *config.Config, logger *logging.Logger) *docker.EventMonitor {
	return docker.NewEventMonitor(hub, cfg.StackLocation, logger)
}

func LogAgentStartup(logger *logging.Logger, cfg *config.Config) {
	logger.Info("berth-agent starting in agent mode",
		zap.String("port", cfg.Port),
		zap.String("stack_location", cfg.StackLocation),
		zap.String("log_level", cfg.LogLevel),
		zap.Bool("api_log_enabled", cfg.APILogEnabled),
		zap.Bool("audit_log_enabled", cfg.AuditLogEnabled),
	)
}

func LogSidecarStartup(logger *logging.Logger, cfg *config.Config) {
	logger.Info("berth-agent starting in sidecar mode",
		zap.String("log_level", cfg.LogLevel),
	)
}

func StartWebSocketHub(lc fx.Lifecycle, hub *websocket.Hub, logger *logging.Logger) {
	lc.Append(fx.Hook{
		OnStart: func(ctx context.Context) error {
			logger.Info("starting websocket hub")
			go hub.Run()
			return nil
		},
		OnStop: func(ctx context.Context) error {
			logger.Info("stopping websocket hub")
			return nil
		},
	})
}

func StartEventMonitor(lc fx.Lifecycle, monitor *docker.EventMonitor, logger *logging.Logger) {
	lc.Append(fx.Hook{
		OnStart: func(ctx context.Context) error {
			logger.Info("starting docker event monitor")
			return monitor.Start()
		},
		OnStop: func(ctx context.Context) error {
			logger.Info("stopping docker event monitor")
			monitor.Stop()
			return nil
		},
	})
}

func StartServer(lc fx.Lifecycle, e *echo.Echo, cfg *config.Config, certManager *ssl.CertificateManager, logger *logging.Logger) {
	lc.Append(fx.Hook{
		OnStart: func(ctx context.Context) error {
			certFile, keyFile, err := certManager.EnsureCertificates()
			if err != nil {
				logger.Error("failed to setup SSL certificates", zap.Error(err))
				e.Logger.Fatal("Failed to setup SSL certificates:", err)
				return err
			}

			logger.Info("starting HTTPS server",
				zap.String("port", cfg.Port),
				zap.String("cert_file", certFile),
			)

			go func() {
				if err := e.StartTLS(":"+cfg.Port, certFile, keyFile); err != nil {
					logger.Error("HTTPS server failed to start", zap.Error(err))
					e.Logger.Fatal("HTTPS Server failed to start:", err)
				}
			}()
			return nil
		},
		OnStop: func(ctx context.Context) error {
			logger.Info("shutting down HTTPS server")
			return e.Shutdown(ctx)
		},
	})
}

func NewSidecarEcho(loggingService *logging.Service) *echo.Echo {
	e := echo.New()
	e.Use(echomiddleware.Recover())
	e.Use(logging.RequestLoggingMiddleware(loggingService))
	return e
}

func RegisterSidecarRoutes(e *echo.Echo, handler *sidecar.Handler, cfg *config.Config, logger *logging.Logger) {
	api := e.Group("")
	api.Use(auth.TokenMiddleware(cfg.AccessToken, logger))

	api.POST("/operation", handler.HandleOperation)
	api.GET("/health", handler.Health)
}

func StartSidecarServer(lc fx.Lifecycle, e *echo.Echo, certManager *ssl.CertificateManager, logger *logging.Logger) {
	lc.Append(fx.Hook{
		OnStart: func(ctx context.Context) error {
			certFile, keyFile, err := certManager.EnsureCertificates()
			if err != nil {
				logger.Error("failed to setup SSL certificates for sidecar", zap.Error(err))
				e.Logger.Fatal("Failed to setup SSL certificates:", err)
				return err
			}

			logger.Info("starting sidecar HTTPS server",
				zap.String("port", "8081"),
				zap.String("cert_file", certFile),
			)

			go func() {
				if err := e.StartTLS(":8081", certFile, keyFile); err != nil {
					logger.Error("sidecar HTTPS server failed to start", zap.Error(err))
					e.Logger.Fatal("Sidecar HTTPS server failed to start:", err)
				}
			}()
			return nil
		},
		OnStop: func(ctx context.Context) error {
			logger.Info("shutting down sidecar HTTPS server")
			return e.Shutdown(ctx)
		},
	})
}

func runSocketProxy() {
	app := fx.New(
		config.Module,
		logging.Module,
		socketproxy.Module,
		fx.Invoke(StartSocketProxy),
		fx.Invoke(LogSocketProxyStartup),
	)
	app.Run()
}

func StartSocketProxy(lc fx.Lifecycle, proxy *socketproxy.Proxy, logger *logging.Logger) {
	lc.Append(fx.Hook{
		OnStart: func(ctx context.Context) error {
			return proxy.Start(ctx)
		},
		OnStop: func(ctx context.Context) error {
			return proxy.Stop(ctx)
		},
	})
}

func LogSocketProxyStartup(logger *logging.Logger) {
	logger.Info("berth-agent starting in socket-proxy mode")
}

func runGrypeScanner() {
	app := fx.New(
		config.Module,
		logging.Module,
		ssl.Module,
		grypescanner.Module,
		fx.Provide(NewGrypeScannerEcho),
		fx.Invoke(RegisterGrypeScannerRoutes),
		fx.Invoke(StartGrypeScannerServer),
		fx.Invoke(LogGrypeScannerStartup),
	)
	app.Run()
}

func NewGrypeScannerEcho(loggingService *logging.Service) *echo.Echo {
	e := echo.New()
	e.Use(echomiddleware.Recover())
	e.Use(logging.RequestLoggingMiddleware(loggingService))
	return e
}

func RegisterGrypeScannerRoutes(e *echo.Echo, handler *grypescanner.Handler, cfg *config.Config, logger *logging.Logger) {
	api := e.Group("")
	api.Use(auth.TokenMiddleware(cfg.AccessToken, logger))

	api.POST("/scan", handler.Scan)
	api.GET("/health", handler.Health)
}

func StartGrypeScannerServer(lc fx.Lifecycle, e *echo.Echo, certManager *ssl.CertificateManager, logger *logging.Logger) {
	lc.Append(fx.Hook{
		OnStart: func(ctx context.Context) error {
			certFile, keyFile, err := certManager.EnsureCertificates()
			if err != nil {
				logger.Error("failed to setup SSL certificates for grype-scanner", zap.Error(err))
				e.Logger.Fatal("Failed to setup SSL certificates:", err)
				return err
			}

			logger.Info("starting grype-scanner HTTPS server",
				zap.String("port", "8082"),
				zap.String("cert_file", certFile),
			)

			e.Server.ReadTimeout = 30 * time.Second
			e.Server.WriteTimeout = 15 * time.Minute

			go func() {
				if err := e.StartTLS(":8082", certFile, keyFile); err != nil {
					logger.Error("grype-scanner HTTPS server failed to start", zap.Error(err))
					e.Logger.Fatal("Grype-scanner HTTPS server failed to start:", err)
				}
			}()
			return nil
		},
		OnStop: func(ctx context.Context) error {
			logger.Info("shutting down grype-scanner HTTPS server")
			return e.Shutdown(ctx)
		},
	})
}

func LogGrypeScannerStartup(logger *logging.Logger) {
	logger.Info("berth-agent starting in grype-scanner mode")
}
