package handlers

import (
	"encoding/json"
	"net/http"
	"strings"
)

func FilesRoot(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/api/v1/stacks/files" {
		http.NotFound(w, r)
		return
	}
	
	switch r.Method {
	case "GET":
		ListFiles(w, r)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func FilesWithPath(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/stacks/files/")
	if path == "" {
		http.NotFound(w, r)
		return
	}
	
	switch r.Method {
	case "GET":
		GetFile(w, r)
	case "PUT":
		UpdateFile(w, r)
	case "DELETE":
		DeleteFile(w, r)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func ListFiles(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusNotImplemented)
	json.NewEncoder(w).Encode(map[string]string{"error": "Not implemented yet"})
}

func ExportFiles(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusNotImplemented)
	json.NewEncoder(w).Encode(map[string]string{"error": "Not implemented yet"})
}

func GetFile(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusNotImplemented)
	json.NewEncoder(w).Encode(map[string]string{"error": "Not implemented yet"})
}

func UpdateFile(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusNotImplemented)
	json.NewEncoder(w).Encode(map[string]string{"error": "Not implemented yet"})
}

func DeleteFile(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusNotImplemented)
	json.NewEncoder(w).Encode(map[string]string{"error": "Not implemented yet"})
}