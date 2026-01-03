package logs

import (
	"berth-agent/internal/logging"
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"go.uber.org/zap"
)

type Service struct {
	stackLocation string
	logger        *logging.Logger
}

func NewService(stackLocation string, logger *logging.Logger) *Service {
	return &Service{
		stackLocation: stackLocation,
		logger:        logger.With(zap.String("component", "logs")),
	}
}

type LogEntry struct {
	Timestamp time.Time `json:"timestamp"`
	Message   string    `json:"message"`
	Source    string    `json:"source"`
	Level     string    `json:"level,omitempty"`
}

type LogRequest struct {
	StackName     string
	ServiceName   string
	ContainerName string
	Tail          int
	Since         string
	Timestamps    bool
}

func (s *Service) GetStackLogs(ctx context.Context, req LogRequest) ([]LogEntry, error) {
	s.logger.Info("Starting stack log retrieval",
		zap.String("stack", req.StackName),
		zap.Int("tail", req.Tail),
		zap.String("since", req.Since),
		zap.Bool("timestamps", req.Timestamps),
	)

	args := []string{"compose", "logs"}
	if req.Timestamps {
		args = append(args, "--timestamps")
	}
	if req.Tail > 0 {
		args = append(args, "--tail", strconv.Itoa(req.Tail))
	}
	if req.Since != "" {
		args = append(args, "--since", req.Since)
	}

	s.logger.Debug("Executing docker compose logs",
		zap.String("stack", req.StackName),
		zap.Strings("docker_args", args),
	)

	cmd := exec.CommandContext(ctx, "docker", args...)
	cmd.Dir = fmt.Sprintf("%s/%s", s.stackLocation, req.StackName)

	output, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			s.logger.Error("Failed to retrieve stack logs",
				zap.String("stack", req.StackName),
				zap.String("stderr", string(exitErr.Stderr)),
				zap.Error(err),
			)
		} else {
			s.logger.Error("Failed to execute docker command",
				zap.String("stack", req.StackName),
				zap.Error(err),
			)
		}
		return nil, fmt.Errorf("failed to get stack logs: %w", err)
	}

	entries := s.parseLogOutput(string(output), req.Timestamps)

	s.logger.Info("Stack log retrieval completed",
		zap.String("stack", req.StackName),
		zap.Int("entries_count", len(entries)),
	)

	return entries, nil
}

func (s *Service) GetContainerLogs(ctx context.Context, req LogRequest) ([]LogEntry, error) {
	s.logger.Info("Starting container log retrieval",
		zap.String("stack", req.StackName),
		zap.String("container", req.ContainerName),
		zap.Int("tail", req.Tail),
		zap.String("since", req.Since),
		zap.Bool("timestamps", req.Timestamps),
	)

	if req.StackName != "" {
		if err := s.validateContainerInStack(ctx, req.StackName, req.ContainerName); err != nil {
			s.logger.Warn("Container stack validation failed",
				zap.String("container", req.ContainerName),
				zap.String("stack", req.StackName),
				zap.Error(err),
			)
			return nil, fmt.Errorf("container %s does not belong to stack %s", req.ContainerName, req.StackName)
		}
	}

	args := []string{"logs"}
	if req.Timestamps {
		args = append(args, "--timestamps")
	}
	if req.Tail > 0 {
		args = append(args, "--tail", strconv.Itoa(req.Tail))
	}
	if req.Since != "" {
		args = append(args, "--since", req.Since)
	}
	args = append(args, req.ContainerName)

	s.logger.Debug("Executing docker logs",
		zap.String("container", req.ContainerName),
		zap.Strings("docker_args", args),
	)

	cmd := exec.CommandContext(ctx, "docker", args...)

	output, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			s.logger.Error("Failed to retrieve container logs",
				zap.String("container", req.ContainerName),
				zap.String("stderr", string(exitErr.Stderr)),
				zap.Error(err),
			)
		} else {
			s.logger.Error("Failed to execute docker command",
				zap.String("container", req.ContainerName),
				zap.Error(err),
			)
		}
		return nil, fmt.Errorf("failed to get container logs: %w", err)
	}

	entries := s.parseContainerLogOutput(string(output), req.ContainerName, req.Timestamps)

	s.logger.Info("Container log retrieval completed",
		zap.String("container", req.ContainerName),
		zap.Int("entries_count", len(entries)),
	)

	return entries, nil
}

func (s *Service) parseLogOutput(output string, includeTimestamps bool) []LogEntry {
	var entries []LogEntry
	scanner := bufio.NewScanner(strings.NewReader(output))

	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}

		entry := LogEntry{}

		if includeTimestamps {
			parts := strings.SplitN(line, " ", 2)
			if len(parts) >= 2 {
				if timestamp, err := time.Parse(time.RFC3339Nano, parts[0]); err == nil {
					entry.Timestamp = timestamp
					line = parts[1]
				}
			}
		}

		if entry.Timestamp.IsZero() {
			entry.Timestamp = time.Now()
		}

		sourceParts := strings.SplitN(line, "|", 2)
		if len(sourceParts) == 2 {
			entry.Source = strings.TrimSpace(sourceParts[0])
			entry.Message = strings.TrimSpace(sourceParts[1])
		} else {
			entry.Message = line
		}

		if strings.Contains(entry.Message, "ERROR") || strings.Contains(entry.Message, "error") {
			entry.Level = "error"
		} else if strings.Contains(entry.Message, "WARN") || strings.Contains(entry.Message, "warn") {
			entry.Level = "warn"
		} else if strings.Contains(entry.Message, "INFO") || strings.Contains(entry.Message, "info") {
			entry.Level = "info"
		}

		entries = append(entries, entry)
	}

	return entries
}

func (s *Service) parseContainerLogOutput(output string, containerName string, includeTimestamps bool) []LogEntry {
	var entries []LogEntry
	scanner := bufio.NewScanner(strings.NewReader(output))

	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}

		entry := LogEntry{
			Source: containerName,
		}

		if includeTimestamps {
			parts := strings.SplitN(line, " ", 2)
			if len(parts) >= 2 {
				if timestamp, err := time.Parse(time.RFC3339Nano, parts[0]); err == nil {
					entry.Timestamp = timestamp
					line = parts[1]
				}
			}
		}

		if entry.Timestamp.IsZero() {
			entry.Timestamp = time.Now()
		}

		entry.Message = line

		if strings.Contains(entry.Message, "ERROR") || strings.Contains(entry.Message, "error") {
			entry.Level = "error"
		} else if strings.Contains(entry.Message, "WARN") || strings.Contains(entry.Message, "warn") {
			entry.Level = "warn"
		} else if strings.Contains(entry.Message, "INFO") || strings.Contains(entry.Message, "info") {
			entry.Level = "info"
		}

		entries = append(entries, entry)
	}

	return entries
}

func (s *Service) validateContainerInStack(ctx context.Context, stackName, containerName string) error {
	stackDir := fmt.Sprintf("%s/%s", s.stackLocation, stackName)

	cmd := exec.CommandContext(ctx, "docker", "compose", "ps", "--format", "json", "-a")
	cmd.Dir = stackDir

	output, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return fmt.Errorf("failed to list stack containers: %s", string(exitErr.Stderr))
		}
		return fmt.Errorf("failed to list stack containers: %w", err)
	}

	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	for _, line := range lines {
		if line == "" {
			continue
		}

		var containerInfo struct {
			Name string `json:"Name"`
		}
		if err := json.Unmarshal([]byte(line), &containerInfo); err != nil {
			continue
		}

		if containerInfo.Name == containerName {
			return nil
		}
	}

	return fmt.Errorf("container not found in stack")
}
