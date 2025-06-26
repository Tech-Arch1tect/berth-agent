package files

import (
	"berth-agent/internal/config"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

func DeleteFileHandler(cfg *config.AppConfig) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "DELETE" {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		stackName := extractStackName(r, "/api/v1/stacks/")
		filePath := extractFilePath(r)

		if stackName == "" {
			http.NotFound(w, r)
			return
		}

		if filePath == "" {
			http.Error(w, "Path parameter is required", http.StatusBadRequest)
			return
		}

		DeleteFile(w, r, cfg, stackName, filePath)
	}
}

type DeleteFileResponse struct {
	Stack   string `json:"stack"`
	Path    string `json:"path"`
	Message string `json:"message"`
}

func DeleteFile(w http.ResponseWriter, r *http.Request, cfg *config.AppConfig, stackName, filePath string) {
	w.Header().Set("Content-Type", "application/json")

	filePath = filepath.Clean(filePath)
	if strings.Contains(filePath, "..") {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "Invalid path: directory traversal not allowed",
		})
		return
	}

	stackDir := filepath.Join(cfg.ComposeDirPath, stackName)
	fullPath := filepath.Join(stackDir, filePath)

	if !strings.HasPrefix(fullPath, stackDir) {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "Invalid path: outside stack directory",
		})
		return
	}

	fileInfo, err := os.Stat(fullPath)
	if os.IsNotExist(err) {
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "File or directory not found",
			"path":  filePath,
		})
		return
	}

	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{
			"error": fmt.Sprintf("Failed to access file: %v", err),
		})
		return
	}

	recursive := r.URL.Query().Get("recursive") == "true"

	if fileInfo.IsDir() {
		if !recursive {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{
				"error": "Path is a directory. Use recursive=true to delete directories",
			})
			return
		}
		
		err = os.RemoveAll(fullPath)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]string{
				"error": fmt.Sprintf("Failed to delete directory: %v", err),
			})
			return
		}
	} else {
		err = os.Remove(fullPath)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]string{
				"error": fmt.Sprintf("Failed to delete file: %v", err),
			})
			return
		}
	}

	var message string
	if fileInfo.IsDir() {
		message = "Directory deleted successfully"
	} else {
		message = "File deleted successfully"
	}

	response := DeleteFileResponse{
		Stack:   stackName,
		Path:    filePath,
		Message: message,
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)
}
