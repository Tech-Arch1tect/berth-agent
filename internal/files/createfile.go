package files

import (
	"berth-agent/internal/compose"
	"berth-agent/internal/config"
	"berth-agent/internal/utils"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
)

func CreateFileHandler(cfg *config.AppConfig) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		stackName := utils.ExtractStackName(r, "/api/v1/stacks/")
		filePath := utils.ExtractFilePath(r)

		if stackName == "" {
			http.NotFound(w, r)
			return
		}

		if filePath == "" {
			http.Error(w, "Path parameter is required", http.StatusBadRequest)
			return
		}

		CreateFile(w, r, cfg, stackName, filePath)
	}
}

type CreateFileRequest struct {
	Content  string `json:"content"`
	IsBinary bool   `json:"isBinary"`
	IsBase64 bool   `json:"isBase64"`
}

type CreateFileResponse struct {
	Stack   string `json:"stack"`
	Path    string `json:"path"`
	Message string `json:"message"`
	Size    int64  `json:"size"`
}

func CreateFile(w http.ResponseWriter, r *http.Request, cfg *config.AppConfig, stackName, filePath string) {
	w.Header().Set("Content-Type", "application/json")

	stackDir, _, err := compose.ValidateStackAndFindComposeFile(cfg, stackName)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	safePath, err := utils.SafeFilePath(filePath)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	fullPath := filepath.Join(stackDir, safePath)

	var req CreateFileRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "Invalid JSON request body",
		})
		return
	}

	_, err = os.Stat(fullPath)
	if err == nil {
		w.WriteHeader(http.StatusConflict)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "File already exists",
			"path":  filePath,
		})
		return
	}

	if !os.IsNotExist(err) {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{
			"error": fmt.Sprintf("Failed to access file: %v", err),
		})
		return
	}

	if err := utils.WriteFileContent(fullPath, req.Content, req.IsBinary, req.IsBase64); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{
			"error": fmt.Sprintf("Failed to write file: %v", err),
		})
		return
	}

	fileInfo, err := os.Stat(fullPath)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{
			"error": fmt.Sprintf("Failed to get file info: %v", err),
		})
		return
	}

	response := CreateFileResponse{
		Stack:   stackName,
		Path:    filePath,
		Message: "File created successfully",
		Size:    fileInfo.Size(),
	}

	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(response)
}
