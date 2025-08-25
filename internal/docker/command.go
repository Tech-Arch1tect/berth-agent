package docker

import (
	"berth-agent/internal/validation"
	"fmt"
	"os/exec"
	"path/filepath"
)

type CommandExecutor struct {
	stackLocation string
}

func NewCommandExecutor(stackLocation string) *CommandExecutor {
	return &CommandExecutor{
		stackLocation: stackLocation,
	}
}

func (e *CommandExecutor) ExecuteComposeCommand(stackName string, args ...string) (*exec.Cmd, error) {
	stackPath, err := validation.SanitizeStackPath(e.stackLocation, stackName)
	if err != nil {
		return nil, fmt.Errorf("invalid stack name '%s': %w", stackName, err)
	}

	baseArgs := []string{"compose"}
	safeArgs := append(baseArgs, args...)

	cmd := exec.Command("docker", safeArgs...)
	cmd.Dir = stackPath

	return cmd, nil
}

func (e *CommandExecutor) ExecuteComposeWithFile(stackName, composeFile string, args ...string) (*exec.Cmd, error) {
	stackPath, err := validation.SanitizeStackPath(e.stackLocation, stackName)
	if err != nil {
		return nil, fmt.Errorf("invalid stack name '%s': %w", stackName, err)
	}

	composePath := filepath.Join(stackPath, composeFile)

	baseArgs := []string{"compose", "-f", composePath}
	safeArgs := append(baseArgs, args...)

	cmd := exec.Command("docker", safeArgs...)
	cmd.Dir = stackPath

	return cmd, nil
}
