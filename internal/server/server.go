package server

import (
	"net/http"
	
	"berth-agent/internal/handlers"
)

func New() *http.Server {
	return &http.Server{
		Addr:    ":8081",
		Handler: setupRoutes(),
	}
}

func setupRoutes() *http.ServeMux {
	mux := http.NewServeMux()

	// Health endpoint
	mux.HandleFunc("/health", handlers.Health)

	// Compose endpoints
	mux.HandleFunc("/api/v1/stacks/compose/info", handleMethod("GET", handlers.ComposeInfo))
	mux.HandleFunc("/api/v1/stacks/compose/exec", handleMethod("POST", handlers.ComposeExec))
	mux.HandleFunc("/api/v1/stacks/compose/ps", handleMethod("GET", handlers.ComposePs))
	mux.HandleFunc("/api/v1/stacks/compose/logs", handleMethod("GET", handlers.ComposeLogs))
	mux.HandleFunc("/api/v1/stacks/compose/up", handleMethod("POST", handlers.ComposeUp))
	mux.HandleFunc("/api/v1/stacks/compose/down", handleMethod("POST", handlers.ComposeDown))
	mux.HandleFunc("/api/v1/stacks/compose/status", handleMethod("GET", handlers.ComposeStatus))

	// Files endpoints
	mux.HandleFunc("/api/v1/stacks/files", handlers.FilesRoot)
	mux.HandleFunc("/api/v1/stacks/files/export", handleMethod("POST", handlers.ExportFiles))
	mux.HandleFunc("/api/v1/stacks/files/", handlers.FilesWithPath)

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