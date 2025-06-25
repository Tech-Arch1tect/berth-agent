package files

import (
	"encoding/json"
	"net/http"
)

func DeleteFileHandler(w http.ResponseWriter, r *http.Request) {
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

	DeleteFile(w, r, stackName, filePath)
}

func DeleteFile(w http.ResponseWriter, r *http.Request, stackName, filePath string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusNotImplemented)
	json.NewEncoder(w).Encode(map[string]string{
		"error":    "Not implemented yet",
		"stack":    stackName,
		"path":     filePath,
		"endpoint": "DeleteFile",
	})
}
