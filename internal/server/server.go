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
	"strings"
)

func New(cfg *config.AppConfig) *http.Server {
	return &http.Server{
		Addr:    fmt.Sprintf(":%d", cfg.Port),
		Handler: setupRoutes(cfg),
	}
}

func setupRoutes(cfg *config.AppConfig) *http.ServeMux {
	mux := http.NewServeMux()

	// Health endpoint
	mux.HandleFunc("/health", handlers.Health)

	// Compose endpoints
	mux.HandleFunc("/api/v1/stacks/compose/info", handleMethod("GET", compose.ComposeInfo))
	mux.HandleFunc("/api/v1/stacks/compose/exec", handleMethod("POST", compose.ComposeExec))
	mux.HandleFunc("/api/v1/stacks/compose/ps", handleMethod("GET", compose.ComposePs))
	mux.HandleFunc("/api/v1/stacks/compose/logs", handleMethod("GET", compose.ComposeLogs))
	mux.HandleFunc("/api/v1/stacks/compose/up", handleMethod("POST", compose.ComposeUp))
	mux.HandleFunc("/api/v1/stacks/compose/down", handleMethod("POST", compose.ComposeDown))
	mux.HandleFunc("/api/v1/stacks/compose/status", handleMethod("GET", compose.ComposeStatus))

	// Files endpoints
	mux.HandleFunc("/api/v1/stacks/", func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/api/v1/stacks/")
		parts := strings.Split(path, "/")

		// Route based on URL structure and method
		if len(parts) >= 2 && parts[1] == "files" {
			if len(parts) == 2 {
				files.ListFilesHandler(cfg)(w, r)
			} else {
				switch r.Method {
				case "GET":
					files.GetFileHandler(w, r)
				case "PUT":
					files.UpdateFileHandler(w, r)
				case "DELETE":
					files.DeleteFileHandler(w, r)
				default:
					http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
				}
			}
		} else {
			http.NotFound(w, r)
		}
	})

	// Export endpoints
	mux.HandleFunc("/api/v1/export/", export.ExportHandler)

	// Stacks endpoints
	mux.HandleFunc("/api/v1/stacks/stacks", handleMethod("GET", stacks.ListStacks(cfg)))

	return mux
}

func handleMethod(method string, handler http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != method {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		handler(w, r)
	}
}
