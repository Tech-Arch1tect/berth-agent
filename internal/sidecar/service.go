package sidecar

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/tech-arch1tect/berth-agent/config"
	"github.com/tech-arch1tect/berth-agent/internal/logging"

	"go.uber.org/zap"
)

type Service struct {
	logger        *logging.Logger
	stackLocation string
}

func NewService(cfg *config.Config, logger *logging.Logger) *Service {
	logger.Info("sidecar service initialized",
		zap.String("stack_location", cfg.StackLocation),
	)
	return &Service{
		logger:        logger,
		stackLocation: cfg.StackLocation,
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

	absStackPath, err := filepath.Abs(req.StackPath)
	if err != nil {
		s.logger.Error("invalid stack path",
			zap.String("stack_path", req.StackPath),
			zap.Error(err),
		)
		return fmt.Errorf("invalid stack path: %w", err)
	}

	absBase, err := filepath.Abs(s.stackLocation)
	if err != nil {
		s.logger.Error("invalid stack location config",
			zap.String("stack_location", s.stackLocation),
			zap.Error(err),
		)
		return fmt.Errorf("invalid stack location: %w", err)
	}

	rel, err := filepath.Rel(absBase, absStackPath)
	if err != nil || strings.HasPrefix(rel, "..") || strings.Contains(rel, string(filepath.Separator)) {
		s.logger.Error("stack path outside allowed directory",
			zap.String("stack_path", req.StackPath),
			zap.String("stack_location", s.stackLocation),
			zap.String("resolved_rel", rel),
		)
		return fmt.Errorf("stack path is outside the allowed directory")
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
	cmd.Dir = absStackPath

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	s.logger.Info("sidecar starting command execution")

	err = cmd.Run()
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
