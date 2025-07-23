package utils

import (
	"net/http"
	"strings"
)

func ExtractStackName(r *http.Request, prefix string) string {
	path := strings.TrimPrefix(r.URL.Path, prefix)
	parts := strings.Split(path, "/")
	if len(parts) > 0 {
		return parts[0]
	}
	return ""
}

func ExtractFilePath(r *http.Request) string {
	return r.URL.Query().Get("path")
}

func ExtractServiceName(r *http.Request, prefix string) string {
	path := strings.TrimPrefix(r.URL.Path, prefix)
	parts := strings.Split(path, "/")
	if len(parts) > 0 {
		return parts[0]
	}
	return ""
}
