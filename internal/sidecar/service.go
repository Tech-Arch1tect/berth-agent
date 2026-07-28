package sidecar

import (
	"bytes"
	"context"
	"fmt"
	"os"
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

func (s *Service) AgentStackPath() (string, error) {
	base, err := filepath.Abs(s.stackLocation)
	if err != nil {
		return "", fmt.Errorf("invalid stack location %q: %w", s.stackLocation, err)
	}

	stackPath := filepath.Join(base, AgentStackName)
	if _, err := os.Stat(stackPath); err != nil {
		return "", fmt.Errorf("the %s stack directory is not readable at %s: %w", AgentStackName, stackPath, err)
	}
	return stackPath, nil
}

func (s *Service) ExecuteOperation(_ context.Context, req OperationRequest) error {
	s.logger.Info("sidecar operation starting",
		zap.String("command", req.Command),
		zap.Strings("options", req.Options),
	)

	if err := ValidateOperation(req.Command, req.Options); err != nil {
		s.logger.Error("sidecar refused an operation outside what it exists to do",
			zap.String("command", req.Command),
			zap.Strings("options", req.Options),
			zap.Error(err),
		)
		return err
	}

	absStackPath, err := s.AgentStackPath()
	if err != nil {
		s.logger.Error("sidecar cannot reach the agent stack",
			zap.String("stack_location", s.stackLocation),
			zap.Error(err),
		)
		return err
	}

	args := composeArgs(req.Command, req.Options)

	fullCommand := "docker " + strings.Join(args, " ")
	s.logger.Info("sidecar executing docker compose command",
		zap.String("full_command", fullCommand),
		zap.String("working_dir", absStackPath),
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
