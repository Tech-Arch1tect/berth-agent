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

	mux.HandleFunc("/api/v1/stacks/", func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/api/v1/stacks/")
		parts := strings.Split(path, "/")

		if len(parts) >= 3 && parts[1] == "compose" {
			switch parts[2] {
			case "info":
				if r.Method == "GET" {
					compose.ComposeInfoHandler(cfg)(w, r)
				} else {
					http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
				}
			case "exec":
				if r.Method == "POST" {
					compose.ComposeExecHandler(cfg)(w, r)
				} else {
					http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
				}
			case "ps":
				if r.Method == "GET" {
					compose.ComposePsHandler(cfg)(w, r)
				} else {
					http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
				}
			case "logs":
				if r.Method == "GET" {
					compose.ComposeLogsHandler(cfg)(w, r)
				} else {
					http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
				}
			case "up":
				if r.Method == "POST" {
					compose.ComposeUpHandler(cfg)(w, r)
				} else {
					http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
				}
			case "down":
				if r.Method == "POST" {
					compose.ComposeDownHandler(cfg)(w, r)
				} else {
					http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
				}
			default:
				http.NotFound(w, r)
			}
			return
		}

		if len(parts) == 2 && parts[1] == "files" {
			switch r.Method {
			case "GET":
				files.ListFilesHandler(cfg)(w, r)
			default:
				http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			}
			return
		}

		if len(parts) == 2 && parts[1] == "file" {
			switch r.Method {
			case "GET":
				files.GetFileHandler(cfg)(w, r)
			case "POST":
				files.CreateFileHandler(cfg)(w, r)
			case "PUT":
				files.UpdateFileHandler(cfg)(w, r)
			case "DELETE":
				files.DeleteFileHandler(cfg)(w, r)
			default:
				http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			}
			return
		}

		http.NotFound(w, r)
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
