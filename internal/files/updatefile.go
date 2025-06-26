package files

import (
	"berth-agent/internal/config"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

func UpdateFileHandler(cfg *config.AppConfig) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "PUT" {
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

		UpdateFile(w, r, cfg, stackName, filePath)
	}
}

type UpdateFileRequest struct {
	Content string `json:"content"`
}

type UpdateFileResponse struct {
	Stack   string `json:"stack"`
	Path    string `json:"path"`
	Message string `json:"message"`
	Size    int64  `json:"size"`
}

func UpdateFile(w http.ResponseWriter, r *http.Request, cfg *config.AppConfig, stackName, filePath string) {
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

	var req UpdateFileRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "Invalid JSON request body",
		})
		return
	}

	fileInfo, err := os.Stat(fullPath)
	if os.IsNotExist(err) {
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "File not found",
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
	} else if fileInfo.IsDir() {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "Path is a directory, not a file",
		})
		return
	}

	file, err := os.Create(fullPath)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{
			"error": fmt.Sprintf("Failed to create/open file: %v", err),
		})
		return
	}
	defer file.Close()

	_, err = io.WriteString(file, req.Content)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{
			"error": fmt.Sprintf("Failed to write file: %v", err),
		})
		return
	}

	newFileInfo, err := file.Stat()
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{
			"error": fmt.Sprintf("Failed to get file info: %v", err),
		})
		return
	}

	message := "File updated successfully"

	response := UpdateFileResponse{
		Stack:   stackName,
		Path:    filePath,
		Message: message,
		Size:    newFileInfo.Size(),
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)
}
