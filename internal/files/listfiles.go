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

func ListFilesHandler(cfg *config.AppConfig) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		stackName := utils.ExtractStackName(r, "/api/v1/stacks/")
		if stackName == "" {
			http.NotFound(w, r)
			return
		}

		subPath := r.URL.Query().Get("path")
		if subPath == "" {
			subPath = "/"
		}

		ListFiles(w, r, cfg, stackName, subPath)
	}
}

type ListFilesResponse struct {
	Stack string           `json:"stack"`
	Path  string           `json:"path"`
	Files []utils.FileInfo `json:"files"`
}

func ListFiles(w http.ResponseWriter, r *http.Request, cfg *config.AppConfig, stackName, subPath string) {
	w.Header().Set("Content-Type", "application/json")

	subPath = filepath.Clean(subPath)
	if strings.Contains(subPath, "..") {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "Invalid path: directory traversal not allowed",
		})
		return
	}

	stackDir := filepath.Join(cfg.ComposeDirPath, stackName)
	fullPath := filepath.Join(stackDir, subPath)

	if !strings.HasPrefix(fullPath, stackDir) {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "Invalid path: outside stack directory",
		})
		return
	}

	if _, err := os.Stat(fullPath); os.IsNotExist(err) {
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "Directory not found",
			"path":  subPath,
		})
		return
	}

	entries, err := os.ReadDir(fullPath)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{
			"error": fmt.Sprintf("Failed to read directory: %v", err),
		})
		return
	}

	files := make([]utils.FileInfo, 0, len(entries))
	for _, entry := range entries {
		entryPath := filepath.Join(fullPath, entry.Name())
		fileInfo, err := utils.GetEnhancedFileInfo(entryPath)
		if err != nil {
			info, err := entry.Info()
			if err != nil {
				continue
			}
			fileInfo = &utils.FileInfo{
				Name:     entry.Name(),
				IsDir:    entry.IsDir(),
				Size:     info.Size(),
				MimeType: "application/octet-stream",
				IsBinary: true,
				ModTime:  info.ModTime().Format("2006-01-02T15:04:05Z07:00"),
			}
		}

		files = append(files, *fileInfo)
	}

	response := ListFilesResponse{
		Stack: stackName,
		Path:  subPath,
		Files: files,
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)
}
