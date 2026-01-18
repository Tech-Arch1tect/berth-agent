package sidecar

import (
	"berth-agent/internal/logging"
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

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

func (s *Service) ExecuteOperation(_ context.Context, req OperationRequest) error {
	s.logger.Info("sidecar operation starting",
		zap.String("command", req.Command),
		zap.String("stack_path", req.StackPath),
		zap.Strings("options", req.Options),
		zap.Strings("services", req.Services),
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
	s.logger.Info("sidecar executing docker compose command",
		zap.String("full_command", fullCommand),
		zap.String("working_dir", req.StackPath),
		zap.Strings("args", args),
	)

	execCtx, execCancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer execCancel()

	cmd := exec.CommandContext(execCtx, "docker", args...)
	cmd.Dir = req.StackPath

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	s.logger.Info("sidecar starting command execution")

	err := cmd.Run()
	exitCode := 0
	if cmd.ProcessState != nil {
		exitCode = cmd.ProcessState.ExitCode()
	}

	stdoutStr := strings.TrimSpace(stdout.String())
	stderrStr := strings.TrimSpace(stderr.String())

	if stdoutStr != "" {
		for _, line := range strings.Split(stdoutStr, "\n") {
			if line != "" {
				s.logger.Info("sidecar docker stdout",
					zap.String("line", line),
				)
			}
		}
	}

	if stderrStr != "" {
		for _, line := range strings.Split(stderrStr, "\n") {
			if line != "" {
				s.logger.Warn("sidecar docker stderr",
					zap.String("line", line),
				)
			}
		}
	}

	if err != nil {
		s.logger.Error("sidecar command execution failed",
			zap.String("command", fullCommand),
			zap.Int("exit_code", exitCode),
			zap.String("stdout", stdoutStr),
			zap.String("stderr", stderrStr),
			zap.Error(err),
		)
		return fmt.Errorf("docker compose command failed with exit code %d: %w, stderr: %s", exitCode, err, stderrStr)
	}

	s.logger.Info("sidecar command execution successful",
		zap.String("command", fullCommand),
		zap.Int("exit_code", exitCode),
		zap.String("stdout", stdoutStr),
		zap.String("stderr", stderrStr),
	)
	return nil
}
