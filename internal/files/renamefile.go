package files

import (
	"berth-agent/internal/config"
	"berth-agent/internal/utils"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

func RenameFileHandler(cfg *config.AppConfig) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "PATCH" {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		stackName := utils.ExtractStackName(r, "/api/v1/stacks/")
		oldPath := utils.ExtractFilePath(r)

		if stackName == "" {
			http.NotFound(w, r)
			return
		}

		if oldPath == "" {
			http.Error(w, "Path parameter is required", http.StatusBadRequest)
			return
		}

		RenameFile(w, r, cfg, stackName, oldPath)
	}
}

type RenameFileRequest struct {
	NewName string `json:"newName"`
}

type RenameFileResponse struct {
	Stack   string `json:"stack"`
	OldPath string `json:"oldPath"`
	NewPath string `json:"newPath"`
	Message string `json:"message"`
}

func RenameFile(w http.ResponseWriter, r *http.Request, cfg *config.AppConfig, stackName, oldPath string) {
	w.Header().Set("Content-Type", "application/json")

	oldPath = filepath.Clean(oldPath)
	if strings.Contains(oldPath, "..") {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "Invalid path: directory traversal not allowed",
		})
		return
	}

	var req RenameFileRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "Invalid JSON request body",
		})
		return
	}

	if strings.TrimSpace(req.NewName) == "" {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "New name cannot be empty",
		})
		return
	}

	if strings.ContainsAny(req.NewName, "/\\:*?\"<>|") {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "New name contains invalid characters",
		})
		return
	}

	stackDir := filepath.Join(cfg.ComposeDirPath, stackName)
	oldFullPath := filepath.Join(stackDir, oldPath)

	if !strings.HasPrefix(oldFullPath, stackDir) {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "Invalid path: outside stack directory",
		})
		return
	}

	if _, err := os.Stat(oldFullPath); os.IsNotExist(err) {
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "File or directory not found",
			"path":  oldPath,
		})
		return
	}

	oldDir := filepath.Dir(oldFullPath)
	newFullPath := filepath.Join(oldDir, req.NewName)

	newPath := filepath.Join(filepath.Dir(oldPath), req.NewName)
	if filepath.Dir(oldPath) == "." {
		newPath = req.NewName
	}

	if !strings.HasPrefix(newFullPath, stackDir) {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "Invalid new path: outside stack directory",
		})
		return
	}

	if _, err := os.Stat(newFullPath); err == nil {
		w.WriteHeader(http.StatusConflict)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "A file or directory with that name already exists",
			"path":  newPath,
		})
		return
	}

	if err := os.Rename(oldFullPath, newFullPath); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{
			"error": fmt.Sprintf("Failed to rename: %v", err),
		})
		return
	}

	response := RenameFileResponse{
		Stack:   stackName,
		OldPath: oldPath,
		NewPath: newPath,
		Message: "File renamed successfully",
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)
}
