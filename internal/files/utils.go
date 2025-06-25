package files

import (
	"net/http"
	"strings"
)

func extractStackName(r *http.Request, prefix string) string {
	path := strings.TrimPrefix(r.URL.Path, prefix)
	parts := strings.Split(path, "/")
	if len(parts) > 0 {
		return parts[0]
	}
	return ""
}

func extractFilePath(r *http.Request) string {
	return r.URL.Query().Get("path")
}
