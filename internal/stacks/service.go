package stacks

import (
	"io/fs"
	"os"
	"path/filepath"
)

func ScanStacks(basePath string) ([]Stack, error) {
	var stacks []Stack

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

				stacks = append(stacks, Stack{
					Name: stackName,
					Path: path,
				})
				break
			}
		}

		return nil
	})

	return stacks, err
}