package files

import (
	"berth-agent/internal/config"
	"berth-agent/internal/utils"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

func GetFileMetadataHandler(cfg *config.AppConfig) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
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

		GetFileMetadata(w, r, cfg, stackName, filePath)
	}
}

type GetFileMetadataResponse struct {
	Stack    string `json:"stack"`
	Path     string `json:"path"`
	Size     int64  `json:"size"`
	MimeType string `json:"mimeType"`
	IsBinary bool   `json:"isBinary"`
	ModTime  string `json:"modTime"`
	Editable bool   `json:"editable"`
	SizeStr  string `json:"sizeStr"`
}

func GetFileMetadata(w http.ResponseWriter, r *http.Request, cfg *config.AppConfig, stackName, filePath string) {
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
			"error": "File not found",
			"path":  filePath,
		})
		return
	}

	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "Failed to access file",
		})
		return
	}

	if fileInfo.IsDir() {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "Path is a directory, not a file",
		})
		return
	}

	enhancedInfo, err := utils.GetEnhancedFileInfo(fullPath)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "Failed to get file metadata",
		})
		return
	}

	response := GetFileMetadataResponse{
		Stack:    stackName,
		Path:     filePath,
		Size:     enhancedInfo.Size,
		MimeType: enhancedInfo.MimeType,
		IsBinary: enhancedInfo.IsBinary,
		ModTime:  enhancedInfo.ModTime,
		Editable: utils.IsTextEditable(enhancedInfo.MimeType),
		SizeStr:  utils.GetFileSizeString(enhancedInfo.Size),
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)
}
