package sidecar

import (
	"context"
	"fmt"
	"os/exec"
)

type Service struct{}

func NewService() *Service {
	return &Service{}
}

func (s *Service) ExecuteOperation(ctx context.Context, req OperationRequest) error {
	fmt.Printf("Sidecar executing docker-compose %s berth-agent\n", req.Command)

	args := []string{"compose", req.Command}

	for _, option := range req.Options {
		if option != "-d" && option != "--detach" {
			args = append(args, option)
		}
	}

	args = append(args, "berth-agent")

	if req.Command == "up" {
		args = append(args, "-d")
	}

	cmd := exec.CommandContext(ctx, "docker", args...)
	cmd.Dir = req.StackPath

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker-compose command failed: %w, output: %s", err, output)
	}

	fmt.Printf("Sidecar operation completed: %s\n", output)
	return nil
}
