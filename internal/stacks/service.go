package stacks

import (
	"context"
	"io/fs"
	"log"
	"os"
	"path/filepath"

	"github.com/compose-spec/compose-go/v2/cli"
)

func ScanStacks(basePath string) ([]Stack, error) {
	var stacks []Stack
	ctx := context.Background()

	err := filepath.WalkDir(basePath, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}

		if !d.IsDir() {
			return nil
		}

		composeFiles := []string{
			"docker-compose.yml",
			"docker-compose.yaml",
			"compose.yml",
			"compose.yaml",
		}

		for _, composeFile := range composeFiles {
			composePath := filepath.Join(path, composeFile)
			if _, err := os.Stat(composePath); err == nil {
				stackName := filepath.Base(path)
				if stackName == filepath.Base(basePath) {
					stackName = "root"
				}

				stack := Stack{
					Name: stackName,
					Path: path,
				}

				options, err := cli.NewProjectOptions(
					[]string{composePath},
					cli.WithOsEnv,
					cli.WithDotEnv,
					cli.WithName(stackName),
				)
				if err == nil {
					project, err := options.LoadProject(ctx)
					if err == nil {
						stack.Services = project.Services
						stack.Networks = project.Networks
						stack.Volumes = project.Volumes
						stack.ParsedSuccessfully = true
					} else {
						stack.ParsedSuccessfully = false
					}
				} else {
					stack.ParsedSuccessfully = false
					log.Printf("Error parsing stack %s: %v", stackName, err)
				}

				stacks = append(stacks, stack)
				break
			}
		}

		return nil
	})

	return stacks, err
}
