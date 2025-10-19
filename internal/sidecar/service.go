package sidecar

import (
	"berth-agent/internal/logging"
	"context"
	"fmt"
	"os/exec"
	"strings"

	"go.uber.org/zap"
)

type Service struct {
	logger *logging.Logger
}

func NewService(logger *logging.Logger) *Service {
	logger.Info("sidecar service initialized")
	return &Service{
		logger: logger,
	}
}

func (s *Service) ExecuteOperation(ctx context.Context, req OperationRequest) error {
	s.logger.Info("sidecar operation starting",
		zap.String("command", req.Command),
		zap.String("stack_path", req.StackPath),
		zap.Strings("options", req.Options),
	)

	if req.StackPath == "" {
		s.logger.Error("stack path is empty")
		return fmt.Errorf("stack path cannot be empty")
	}

	args := []string{"compose", req.Command}

	s.logger.Debug("processing options", zap.Strings("options", req.Options))
	for _, option := range req.Options {
		if option != "-d" && option != "--detach" {
			args = append(args, option)
			s.logger.Debug("added option", zap.String("option", option))
		} else {
			s.logger.Debug("filtered out detach option", zap.String("option", option))
		}
	}

	args = append(args, "berth-agent")

	if req.Command == "up" {
		args = append(args, "-d")
		s.logger.Debug("added detach flag for up command")
	}

	fullCommand := "docker " + strings.Join(args, " ")
	s.logger.Info("executing sidecar command",
		zap.String("command", fullCommand),
		zap.String("working_dir", req.StackPath),
	)

	cmd := exec.Command("docker", args...)
	cmd.Dir = req.StackPath

	output, err := cmd.CombinedOutput()
	if err != nil {
		s.logger.Error("sidecar command execution failed",
			zap.String("command", fullCommand),
			zap.String("output", string(output)),
			zap.Error(err),
		)
		return fmt.Errorf("docker-compose command failed: %w, output: %s", err, output)
	}

	s.logger.Info("sidecar command execution successful",
		zap.String("command", fullCommand),
		zap.String("output", string(output)),
	)
	return nil
}
