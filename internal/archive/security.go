package archive

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func ValidateExtractPath(destPath, fileName string) (string, error) {
	path := filepath.Join(destPath, fileName)

	cleanDestPath := filepath.Clean(destPath)
	cleanFilePath := filepath.Clean(path)

	if strings.Contains(fileName, "..") || !strings.HasPrefix(cleanFilePath, cleanDestPath) {
		if cleanFilePath != cleanDestPath && !strings.HasPrefix(cleanFilePath, cleanDestPath+string(os.PathSeparator)) {
			return "", fmt.Errorf("path outside destination directory: %s", fileName)
		}
	}

	return path, nil
}

func EnsureWithinStackPath(path, stackPath string) error {
	cleanPath := filepath.Clean(path)
	cleanStackPath := filepath.Clean(stackPath)

	if !strings.HasPrefix(cleanPath, cleanStackPath) {
		return fmt.Errorf("path must be within stack directory")
	}

	return nil
}
