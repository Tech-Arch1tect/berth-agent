package files

import (
	"encoding/json"
	"net/http"
)

func ListFilesHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	
	stackName := extractStackName(r, "/api/v1/stacks/")
	if stackName == "" {
		http.NotFound(w, r)
		return
	}
	
	ListFiles(w, r, stackName)
}

func ListFiles(w http.ResponseWriter, r *http.Request, stackName string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusNotImplemented)
	json.NewEncoder(w).Encode(map[string]string{
		"error": "Not implemented yet",
		"stack": stackName,
		"endpoint": "ListFiles",
	})
}