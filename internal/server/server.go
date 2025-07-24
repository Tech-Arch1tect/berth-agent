package server

import (
	"berth-agent/internal/compose"
	"berth-agent/internal/config"
	"berth-agent/internal/export"
	"berth-agent/internal/files"
	"berth-agent/internal/handlers"
	"berth-agent/internal/stacks"
	"berth-agent/internal/terminal"
	"net/http"
	"strings"
	"time"

	"github.com/tech-arch1tect/simplerouter"
)

func New(cfg *config.AppConfig) *simplerouter.Router {
	terminal.InitTerminalManager(30 * time.Minute)

	return setupRoutes(cfg)
}

func authMiddleware(cfg *config.AppConfig) simplerouter.Middleware {
	return func(next simplerouter.HandlerFunc) simplerouter.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			authHeader := r.Header.Get("Authorization")
			token := ""

			if authHeader != "" && strings.HasPrefix(authHeader, "Bearer ") {
				token = strings.TrimPrefix(authHeader, "Bearer ")
			} else {
				// Fall back to query parameter for WebSocket connections
				token = r.URL.Query().Get("token")
			}

			if token == "" {
				http.Error(w, "Authorization required", http.StatusUnauthorized)
				return
			}

			if token != cfg.Token {
				http.Error(w, "Invalid token", http.StatusUnauthorized)
				return
			}

			next(w, r)
		}
	}
}

func setupRoutes(cfg *config.AppConfig) *simplerouter.Router {
	router := simplerouter.NewWithDefaults().Use(authMiddleware(cfg))

	// Health endpoint
	router.GET("/health", simplerouter.HandlerFunc(handlers.Health))

	// Compose endpoints
	router.GET("/api/v1/stacks/{stack}/compose/info", simplerouter.HandlerFunc(compose.ComposeInfoHandler(cfg)))
	router.POST("/api/v1/stacks/{stack}/compose/exec", simplerouter.HandlerFunc(compose.ComposeExecHandler(cfg)))
	router.GET("/api/v1/stacks/{stack}/compose/ps", simplerouter.HandlerFunc(compose.ComposePsHandler(cfg)))
	router.GET("/api/v1/stacks/{stack}/compose/logs", simplerouter.HandlerFunc(compose.ComposeLogsHandler(cfg)))

	// Streaming endpoints
	router.GET("/api/v1/stacks/{stack}/compose/up/stream", simplerouter.HandlerFunc(compose.ComposeUpStreamHandler(cfg)))
	router.GET("/api/v1/stacks/{stack}/compose/down/stream", simplerouter.HandlerFunc(compose.ComposeDownStreamHandler(cfg)))

	// Terminal session endpoint
	router.GET("/api/v1/stacks/{stack}/terminal/session/{service}", simplerouter.HandlerFunc(terminal.TerminalSessionHandler(cfg)))

	// Files endpoints
	router.GET("/api/v1/stacks/{stack}/files", simplerouter.HandlerFunc(files.ListFilesHandler(cfg)))
	router.GET("/api/v1/stacks/{stack}/file", simplerouter.HandlerFunc(files.GetFileHandler(cfg)))
	router.GET("/api/v1/stacks/{stack}/file/metadata", simplerouter.HandlerFunc(files.GetFileMetadataHandler(cfg)))
	router.GET("/api/v1/stacks/{stack}/file/download", simplerouter.HandlerFunc(files.DownloadFileHandler(cfg)))
	router.POST("/api/v1/stacks/{stack}/file", simplerouter.HandlerFunc(files.CreateFileHandler(cfg)))
	router.PUT("/api/v1/stacks/{stack}/file", simplerouter.HandlerFunc(files.UpdateFileHandler(cfg)))
	router.DELETE("/api/v1/stacks/{stack}/file", simplerouter.HandlerFunc(files.DeleteFileHandler(cfg)))

	// Export endpoints
	router.GET("/api/v1/export/", simplerouter.HandlerFunc(export.ExportHandler))

	// Stacks endpoints
	router.GET("/api/v1/stacks/stacks", simplerouter.HandlerFunc(stacks.ListStacks(cfg)))

	return router
}
