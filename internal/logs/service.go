package logs

import (
	"bufio"
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

type Service struct {
	stackLocation string
}

func NewService(stackLocation string) *Service {
	return &Service{
		stackLocation: stackLocation,
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

	cmd := exec.CommandContext(ctx, "docker", args...)
	cmd.Dir = fmt.Sprintf("%s/%s", s.stackLocation, req.StackName)

	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to get logs: %w", err)
	}

	return s.parseLogOutput(string(output), req.Timestamps), nil
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
