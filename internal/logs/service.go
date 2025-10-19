package logs

import (
	"berth-agent/internal/logging"
	"bufio"
	"context"
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

func (s *Service) GetLogs(ctx context.Context, req LogRequest) ([]LogEntry, error) {
	s.logger.Info("Starting log retrieval",
		zap.String("stack", req.StackName),
		zap.String("service", req.ServiceName),
		zap.String("container", req.ContainerName),
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

	if req.ServiceName != "" {
		args = append(args, req.ServiceName)
	} else if req.ContainerName != "" {
		serviceName := s.extractServiceNameFromContainer(req.ContainerName, req.StackName)
		if serviceName != "" {
			args = append(args, serviceName)
		} else {
			args = append(args, req.ContainerName)
		}
	}

	s.logger.Debug("Connecting to Docker container for logs",
		zap.String("stack", req.StackName),
		zap.Strings("docker_args", args),
	)

	cmd := exec.CommandContext(ctx, "docker", args...)
	cmd.Dir = fmt.Sprintf("%s/%s", s.stackLocation, req.StackName)

	output, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			s.logger.Error("Container not found or failed to retrieve logs",
				zap.String("stack", req.StackName),
				zap.String("service", req.ServiceName),
				zap.String("container", req.ContainerName),
				zap.String("stderr", string(exitErr.Stderr)),
				zap.Error(err),
			)
		} else {
			s.logger.Error("Failed to execute docker command",
				zap.String("stack", req.StackName),
				zap.Error(err),
			)
		}
		return nil, fmt.Errorf("failed to get logs: %w", err)
	}

	entries := s.parseLogOutput(string(output), req.Timestamps)

	s.logger.Info("Log retrieval completed",
		zap.String("stack", req.StackName),
		zap.String("service", req.ServiceName),
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

func (s *Service) extractServiceNameFromContainer(containerName, stackName string) string {
	if strings.HasPrefix(containerName, stackName+"-") && strings.Contains(containerName, "-") {
		withoutStack := strings.TrimPrefix(containerName, stackName+"-")

		lastDashIndex := strings.LastIndex(withoutStack, "-")
		if lastDashIndex > 0 {
			return withoutStack[:lastDashIndex]
		}
	}
	return ""
}
