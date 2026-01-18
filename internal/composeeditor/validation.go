package composeeditor

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/compose-spec/compose-go/v2/cli"
)

var wdMutex sync.Mutex

func (s *Service) validateComposeYaml(stackPath, yamlContent string) error {
	tempFile, err := os.CreateTemp(stackPath, "compose-validate-*.yml")
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}
	tempPath := tempFile.Name()
	defer os.Remove(tempPath)

	if _, err := tempFile.WriteString(yamlContent); err != nil {
		tempFile.Close()
		return fmt.Errorf("failed to write temp file: %w", err)
	}
	tempFile.Close()

	tempFilename := filepath.Base(tempPath)

	wdMutex.Lock()
	defer wdMutex.Unlock()

	originalWd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get working directory: %w", err)
	}

	if err := os.Chdir(stackPath); err != nil {
		return fmt.Errorf("failed to change to stack directory: %w", err)
	}
	defer os.Chdir(originalWd)

	options, err := cli.NewProjectOptions(
		[]string{tempFilename},
		cli.WithWorkingDirectory(stackPath),
		cli.WithResolvedPaths(false),
		cli.WithDiscardEnvFile,
	)
	if err != nil {
		return fmt.Errorf("invalid compose configuration: %w", err)
	}

	_, err = cli.ProjectFromOptions(context.Background(), options)
	if err != nil {
		return fmt.Errorf("invalid compose file: %w", err)
	}

	return nil
}
