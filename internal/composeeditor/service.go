package composeeditor

import (
	"berth-agent/config"
	"berth-agent/internal/logging"
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/compose-spec/compose-go/v2/cli"
	"github.com/compose-spec/compose-go/v2/types"
	"go.uber.org/zap"
)

type Service struct {
	stackLocation string
	logger        *logging.Logger
}

func NewService(cfg *config.Config, logger *logging.Logger) *Service {
	return &Service{
		stackLocation: cfg.StackLocation,
		logger:        logger,
	}
}

func (s *Service) GetComposeConfig(ctx context.Context, stackName string) (*ComposeConfig, error) {
	stackPath := filepath.Join(s.stackLocation, stackName)

	if _, err := os.Stat(stackPath); os.IsNotExist(err) {
		return nil, fmt.Errorf("stack not found: %s", stackName)
	}

	composeFile, err := s.findComposeFile(stackPath)
	if err != nil {
		return nil, fmt.Errorf("failed to find compose file: %w", err)
	}

	s.logger.Debug("parsing compose file",
		zap.String("stack", stackName),
		zap.String("file", composeFile),
	)

	project, err := s.loadProject(stackPath, composeFile)
	if err != nil {
		return nil, fmt.Errorf("failed to parse compose file: %w", err)
	}

	return &ComposeConfig{
		ComposeFile: filepath.Base(composeFile),
		Services:    project.Services,
		Networks:    project.Networks,
		Volumes:     project.Volumes,
		Secrets:     project.Secrets,
		Configs:     project.Configs,
	}, nil
}

func (s *Service) findComposeFile(stackPath string) (string, error) {
	candidates := []string{
		"docker-compose.yml",
		"docker-compose.yaml",
		"compose.yml",
		"compose.yaml",
	}

	for _, candidate := range candidates {
		path := filepath.Join(stackPath, candidate)
		if _, err := os.Stat(path); err == nil {
			return path, nil
		}
	}

	return "", fmt.Errorf("no compose file found in %s", stackPath)
}

func (s *Service) loadProject(stackPath, composeFile string) (*types.Project, error) {
	options, err := cli.NewProjectOptions(
		[]string{composeFile},
		cli.WithWorkingDirectory(stackPath),
		cli.WithResolvedPaths(true),
		cli.WithDiscardEnvFile,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create project options: %w", err)
	}

	project, err := cli.ProjectFromOptions(context.Background(), options)
	if err != nil {
		return nil, fmt.Errorf("failed to load project: %w", err)
	}

	return project, nil
}
