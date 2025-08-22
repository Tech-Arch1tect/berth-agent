package stack

import (
	"berth-agent/config"
	"os"
	"path/filepath"
)

type Stack struct {
	Name        string `json:"name"`
	Path        string `json:"path"`
	ComposeFile string `json:"compose_file"`
}

type Service struct {
	stackLocation string
}

func NewService(cfg *config.Config) *Service {
	return &Service{
		stackLocation: cfg.StackLocation,
	}
}

func (s *Service) ListStacks() ([]Stack, error) {
	var stacks []Stack

	entries, err := os.ReadDir(s.stackLocation)
	if err != nil {
		return nil, err
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		stackPath := filepath.Join(s.stackLocation, entry.Name())

		composeFiles := []string{
			"docker-compose.yml",
			"docker-compose.yaml",
			"compose.yml",
			"compose.yaml",
		}

		for _, filename := range composeFiles {
			composePath := filepath.Join(stackPath, filename)
			if _, err := os.Stat(composePath); err == nil {
				stack := Stack{
					Name:        entry.Name(),
					Path:        stackPath,
					ComposeFile: filename,
				}
				stacks = append(stacks, stack)
				break
			}
		}
	}

	return stacks, nil
}
