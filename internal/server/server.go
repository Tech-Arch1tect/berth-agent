package server

import (
	"berth-agent/internal/compose"
	"berth-agent/internal/config"
	"berth-agent/internal/export"
	"berth-agent/internal/files"
	"berth-agent/internal/handlers"
	"berth-agent/internal/stacks"
	"fmt"
	"net/http"

	"github.com/tech-arch1tect/simplerouter"
)

func New(cfg *config.AppConfig) *http.Server {
	return &http.Server{
		Addr:    fmt.Sprintf(":%d", cfg.Port),
		Handler: setupRoutes(cfg),
	}
}

func setupRoutes(cfg *config.AppConfig) *simplerouter.Router {
	router := simplerouter.New()

	// Health endpoint
	router.GET("/health", simplerouter.HandlerFunc(handlers.Health))

	// Compose endpoints
	router.GET("/api/v1/stacks/{stack}/compose/info", simplerouter.HandlerFunc(compose.ComposeInfoHandler(cfg)))
	router.POST("/api/v1/stacks/{stack}/compose/exec", simplerouter.HandlerFunc(compose.ComposeExecHandler(cfg)))
	router.GET("/api/v1/stacks/{stack}/compose/ps", simplerouter.HandlerFunc(compose.ComposePsHandler(cfg)))
	router.GET("/api/v1/stacks/{stack}/compose/logs", simplerouter.HandlerFunc(compose.ComposeLogsHandler(cfg)))
	router.POST("/api/v1/stacks/{stack}/compose/up", simplerouter.HandlerFunc(compose.ComposeUpHandler(cfg)))
	router.POST("/api/v1/stacks/{stack}/compose/down", simplerouter.HandlerFunc(compose.ComposeDownHandler(cfg)))

	// Files endpoints
	router.GET("/api/v1/stacks/{stack}/files", simplerouter.HandlerFunc(files.ListFilesHandler(cfg)))
	router.GET("/api/v1/stacks/{stack}/file", simplerouter.HandlerFunc(files.GetFileHandler(cfg)))
	router.POST("/api/v1/stacks/{stack}/file", simplerouter.HandlerFunc(files.CreateFileHandler(cfg)))
	router.PUT("/api/v1/stacks/{stack}/file", simplerouter.HandlerFunc(files.UpdateFileHandler(cfg)))
	router.DELETE("/api/v1/stacks/{stack}/file", simplerouter.HandlerFunc(files.DeleteFileHandler(cfg)))

	// Export endpoints
	router.GET("/api/v1/export/", simplerouter.HandlerFunc(export.ExportHandler))

	// Stacks endpoints
	router.GET("/api/v1/stacks/stacks", simplerouter.HandlerFunc(stacks.ListStacks(cfg)))

	return router
}
