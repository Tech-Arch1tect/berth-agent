package server

import (
	"berth-agent/internal/bulk"
	"berth-agent/internal/compose"
	"berth-agent/internal/config"
	"berth-agent/internal/docker"
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
	router := simplerouter.NewWithDefaults().Use(authMiddleware(cfg)).Use(simplerouter.Compression())

	// Health endpoint
	router.GET("/health", simplerouter.HandlerFunc(handlers.Health))

	// Compose endpoints
	router.GET("/api/v1/stacks/{stack}/compose/info", simplerouter.HandlerFunc(compose.ComposeInfoHandler(cfg)))
	router.GET("/api/v1/stacks/{stack}/compose/ps", simplerouter.HandlerFunc(compose.ComposePsHandler(cfg)))
	router.GET("/api/v1/stacks/{stack}/compose/logs", simplerouter.HandlerFunc(compose.ComposeLogsHandler(cfg)))

	// Streaming endpoints
	router.GET("/api/v1/stacks/{stack}/compose/up/stream", simplerouter.HandlerFunc(compose.ComposeUpStreamHandler(cfg)))
	router.GET("/api/v1/stacks/{stack}/compose/down/stream", simplerouter.HandlerFunc(compose.ComposeDownStreamHandler(cfg)))
	router.GET("/api/v1/stacks/{stack}/compose/pull/stream", simplerouter.HandlerFunc(compose.ComposePullStreamHandler(cfg)))

	// Terminal session endpoint
	router.GET("/api/v1/stacks/{stack}/terminal/session/{service}", simplerouter.HandlerFunc(terminal.TerminalSessionHandler(cfg)))

	// Files endpoints
	router.GET("/api/v1/stacks/{stack}/files", simplerouter.HandlerFunc(files.ListFilesHandler(cfg)))
	router.GET("/api/v1/stacks/{stack}/file", simplerouter.HandlerFunc(files.GetFileHandler(cfg)))
	router.GET("/api/v1/stacks/{stack}/file/metadata", simplerouter.HandlerFunc(files.GetFileMetadataHandler(cfg)))
	router.GET("/api/v1/stacks/{stack}/file/download", simplerouter.HandlerFunc(files.DownloadFileHandler(cfg)))
	router.POST("/api/v1/stacks/{stack}/file", simplerouter.HandlerFunc(files.CreateFileHandler(cfg)))
	router.PUT("/api/v1/stacks/{stack}/file", simplerouter.HandlerFunc(files.UpdateFileHandler(cfg)))
	router.PATCH("/api/v1/stacks/{stack}/file", simplerouter.HandlerFunc(files.RenameFileHandler(cfg)))
	router.DELETE("/api/v1/stacks/{stack}/file", simplerouter.HandlerFunc(files.DeleteFileHandler(cfg)))

	// Export endpoints
	router.GET("/api/v1/export/", simplerouter.HandlerFunc(export.ExportHandler))

	// Stacks endpoints
	router.GET("/api/v1/stacks/stacks", simplerouter.HandlerFunc(stacks.ListStacks(cfg)))

	// Bulk endpoints
	router.GET("/api/v1/bulk/stacks-with-status", simplerouter.HandlerFunc(bulk.BulkStacksWithStatusHandler(cfg)))

	// Docker maintenance endpoints
	// Images
	router.GET("/api/v1/docker/images", simplerouter.HandlerFunc(docker.ListImagesHandler(cfg)))
	router.DELETE("/api/v1/docker/images/{imageID}", simplerouter.HandlerFunc(docker.DeleteImageHandler(cfg)))
	router.POST("/api/v1/docker/images/prune", simplerouter.HandlerFunc(docker.PruneImagesHandler(cfg)))

	// Build cache
	router.POST("/api/v1/docker/buildcache/prune", simplerouter.HandlerFunc(docker.PruneBuildCacheHandler(cfg)))

	// System
	router.POST("/api/v1/docker/system/prune", simplerouter.HandlerFunc(docker.SystemPruneHandler(cfg)))
	router.GET("/api/v1/docker/system/info", simplerouter.HandlerFunc(docker.GetSystemInfoHandler(cfg)))
	router.GET("/api/v1/docker/system/df", simplerouter.HandlerFunc(docker.GetDiskUsageHandler(cfg)))

	// Volumes
	router.GET("/api/v1/docker/volumes", simplerouter.HandlerFunc(docker.ListVolumesHandler(cfg)))
	router.DELETE("/api/v1/docker/volumes/{volumeName}", simplerouter.HandlerFunc(docker.DeleteVolumeHandler(cfg)))
	router.POST("/api/v1/docker/volumes/prune", simplerouter.HandlerFunc(docker.PruneVolumesHandler(cfg)))

	// Networks
	router.GET("/api/v1/docker/networks", simplerouter.HandlerFunc(docker.ListNetworksHandler(cfg)))
	router.DELETE("/api/v1/docker/networks/{networkID}", simplerouter.HandlerFunc(docker.DeleteNetworkHandler(cfg)))
	router.POST("/api/v1/docker/networks/prune", simplerouter.HandlerFunc(docker.PruneNetworksHandler(cfg)))

	return router
}
