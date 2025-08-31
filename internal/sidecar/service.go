package sidecar

import (
	"context"
	"fmt"
	"log"
	"os/exec"
	"strings"
)

type Service struct{}

func NewService() *Service {
	return &Service{}
}

func (s *Service) ExecuteOperation(ctx context.Context, req OperationRequest) error {
	log.Printf("Sidecar service starting operation - Command: %s, StackPath: %s", req.Command, req.StackPath)

	if req.StackPath == "" {
		log.Printf("Error: StackPath is empty")
		return fmt.Errorf("stack path cannot be empty")
	}

	args := []string{"compose", req.Command}

	log.Printf("Processing options: %v", req.Options)
	for _, option := range req.Options {
		if option != "-d" && option != "--detach" {
			args = append(args, option)
			log.Printf("Added option: %s", option)
		} else {
			log.Printf("Filtered out option: %s (will be handled separately)", option)
		}
	}

	args = append(args, "berth-agent")

	if req.Command == "up" {
		args = append(args, "-d")
		log.Printf("Added -d flag for up command")
	}

	fullCommand := "docker " + strings.Join(args, " ")
	log.Printf("Executing command: %s (working directory: %s)", fullCommand, req.StackPath)

	cmd := exec.Command("docker", args...)
	cmd.Dir = req.StackPath

	output, err := cmd.CombinedOutput()
	if err != nil {
		log.Printf("Command execution failed - Command: %s, Error: %v, Output: %s", fullCommand, err, string(output))
		return fmt.Errorf("docker-compose command failed: %w, output: %s", err, output)
	}

	log.Printf("Command execution successful - Command: %s, Output: %s", fullCommand, string(output))
	return nil
}
