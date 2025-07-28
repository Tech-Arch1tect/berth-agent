package export

import (
	"encoding/json"
	"net/http"
	"strings"
)

func ExportHandler(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/export/")
	stackName := strings.Split(path, "/")[0]

	if stackName == "" {
		http.NotFound(w, r)
		return
	}

	switch r.Method {
	case "POST":
		ExportStack(w, r, stackName)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func ExportStack(w http.ResponseWriter, r *http.Request, stackName string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusNotImplemented)
	json.NewEncoder(w).Encode(map[string]string{
		"error":    "Not implemented yet",
		"stack":    stackName,
		"endpoint": "ExportStack",
	})
}
