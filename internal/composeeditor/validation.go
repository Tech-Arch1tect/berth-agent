package composeeditor

import (
	"context"
	"fmt"
	"os"

	"github.com/compose-spec/compose-go/v2/cli"
)

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

	options, err := cli.NewProjectOptions(
		[]string{tempPath},
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
