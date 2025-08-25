package validation

import (
	"errors"
	"path/filepath"
	"regexp"
	"strings"
)

var (
	ErrInvalidStackName  = errors.New("invalid stack name")
	ErrPathTraversal     = errors.New("path traversal detected")
	ErrInvalidCharacters = errors.New("invalid characters in input")
)

var ValidStackNameRegex = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]*$`)

func ValidateStackName(name string) error {
	if name == "" {
		return ErrInvalidStackName
	}

	if len(name) > 64 {
		return ErrInvalidStackName
	}

	if !ValidStackNameRegex.MatchString(name) {
		return ErrInvalidCharacters
	}

	if strings.Contains(name, "..") {
		return ErrPathTraversal
	}

	if strings.HasPrefix(name, "/") {
		return ErrPathTraversal
	}

	reserved := []string{".", "..", "con", "prn", "aux", "nul"}
	for _, r := range reserved {
		if strings.EqualFold(name, r) {
			return ErrInvalidStackName
		}
	}

	return nil
}

func SanitizeStackPath(basePath, stackName string) (string, error) {
	if err := ValidateStackName(stackName); err != nil {
		return "", err
	}

	stackPath := filepath.Join(basePath, stackName)
	cleanPath := filepath.Clean(stackPath)

	absBasePath, err := filepath.Abs(basePath)
	if err != nil {
		return "", err
	}

	absStackPath, err := filepath.Abs(cleanPath)
	if err != nil {
		return "", err
	}

	relPath, err := filepath.Rel(absBasePath, absStackPath)
	if err != nil {
		return "", ErrPathTraversal
	}

	if strings.HasPrefix(relPath, "..") {
		return "", ErrPathTraversal
	}

	return cleanPath, nil
}
