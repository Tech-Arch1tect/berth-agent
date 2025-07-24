package utils

import (
	"encoding/base64"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type FileInfo struct {
	Name     string `json:"name"`
	IsDir    bool   `json:"isDir"`
	Size     int64  `json:"size"`
	MimeType string `json:"mimeType"`
	IsBinary bool   `json:"isBinary"`
	ModTime  string `json:"modTime"`
}

type FileContent struct {
	Stack    string `json:"stack"`
	Path     string `json:"path"`
	Content  string `json:"content"`
	Size     int64  `json:"size"`
	MimeType string `json:"mimeType"`
	IsBinary bool   `json:"isBinary"`
	IsBase64 bool   `json:"isBase64"`
	ModTime  string `json:"modTime"`
}

func DetectFileType(filePath string) (mimeType string, isBinary bool, err error) {
	ext := filepath.Ext(filePath)
	if ext != "" {
		mimeType = mime.TypeByExtension(ext)
	}

	if mimeType == "" {
		file, err := os.Open(filePath)
		if err != nil {
			return "", false, err
		}
		defer file.Close()

		buffer := make([]byte, 512)
		n, err := file.Read(buffer)
		if err != nil && err != io.EOF {
			return "", false, err
		}

		mimeType = http.DetectContentType(buffer[:n])
	}

	isBinary = IsBinaryMimeType(mimeType)

	return mimeType, isBinary, nil
}

func IsBinaryMimeType(mimeType string) bool {
	parts := strings.Split(mimeType, "/")
	if len(parts) < 2 {
		return false
	}

	mainType := parts[0]
	subType := parts[1]

	if mainType == "text" {
		return false
	}

	textApplicationTypes := map[string]bool{
		"json":        true,
		"xml":         true,
		"javascript":  true,
		"x-yaml":      true,
		"x-sh":        true,
		"x-perl":      true,
		"x-python":    true,
		"x-ruby":      true,
		"sql":         true,
		"x-httpd-php": true,
	}

	if mainType == "application" && textApplicationTypes[subType] {
		return false
	}

	dockerTextTypes := map[string]bool{
		"dockerfile":   true,
		"x-dockerfile": true,
		"compose":      true,
		"x-compose":    true,
		"yaml":         true,
		"x-yaml":       true,
		"yml":          true,
	}

	if mainType == "application" && dockerTextTypes[subType] {
		return false
	}

	return true
}

func ReadFileContent(filePath string) (*FileContent, error) {
	info, err := os.Stat(filePath)
	if err != nil {
		return nil, err
	}

	if info.IsDir() {
		return nil, fmt.Errorf("path is a directory, not a file")
	}

	mimeType, isBinary, err := DetectFileType(filePath)
	if err != nil {
		return nil, err
	}

	file, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var content string
	var isBase64 bool

	if isBinary {
		data, err := io.ReadAll(file)
		if err != nil {
			return nil, err
		}
		content = base64.StdEncoding.EncodeToString(data)
		isBase64 = true
	} else {
		data, err := io.ReadAll(file)
		if err != nil {
			return nil, err
		}
		content = string(data)
		isBase64 = false
	}

	return &FileContent{
		Path:     filePath,
		Content:  content,
		Size:     info.Size(),
		MimeType: mimeType,
		IsBinary: isBinary,
		IsBase64: isBase64,
		ModTime:  info.ModTime().Format(time.RFC3339),
	}, nil
}

func WriteFileContent(filePath, content string, isBinary, isBase64 bool) error {
	if err := os.MkdirAll(filepath.Dir(filePath), 0755); err != nil {
		return err
	}

	file, err := os.Create(filePath)
	if err != nil {
		return err
	}
	defer file.Close()

	if isBinary && isBase64 {
		data, err := base64.StdEncoding.DecodeString(content)
		if err != nil {
			return fmt.Errorf("failed to decode base64 content: %w", err)
		}
		_, err = file.Write(data)
		return err
	} else {
		_, err = file.WriteString(content)
		return err
	}
}

func GetEnhancedFileInfo(filePath string) (*FileInfo, error) {
	info, err := os.Stat(filePath)
	if err != nil {
		return nil, err
	}

	var mimeType string
	var isBinary bool

	if !info.IsDir() {
		mimeType, isBinary, err = DetectFileType(filePath)
		if err != nil {
			mimeType = "application/octet-stream"
			isBinary = true
		}
	} else {
		mimeType = "inode/directory"
		isBinary = false
	}

	return &FileInfo{
		Name:     info.Name(),
		IsDir:    info.IsDir(),
		Size:     info.Size(),
		MimeType: mimeType,
		IsBinary: isBinary,
		ModTime:  info.ModTime().Format(time.RFC3339),
	}, nil
}

func IsTextEditable(mimeType string) bool {
	if strings.HasPrefix(mimeType, "text/") {
		return true
	}

	editableTypes := map[string]bool{
		"application/json":         true,
		"application/xml":          true,
		"application/javascript":   true,
		"application/x-yaml":       true,
		"application/x-sh":         true,
		"application/x-perl":       true,
		"application/x-python":     true,
		"application/x-ruby":       true,
		"application/sql":          true,
		"application/x-httpd-php":  true,
		"application/dockerfile":   true,
		"application/x-dockerfile": true,
		"application/compose":      true,
		"application/x-compose":    true,
		"application/yaml":         true,
	}

	return editableTypes[mimeType]
}

func GetFileSizeString(size int64) string {
	const unit = 1024
	if size < unit {
		return fmt.Sprintf("%d B", size)
	}
	div, exp := int64(unit), 0
	for n := size / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(size)/float64(div), "KMGTPE"[exp])
}

func ValidateFileSize(size int64, maxSizeMB int64) error {
	maxSize := maxSizeMB * 1024 * 1024
	if size > maxSize {
		return fmt.Errorf("file size %s exceeds maximum allowed size %s",
			GetFileSizeString(size), GetFileSizeString(maxSize))
	}
	return nil
}
