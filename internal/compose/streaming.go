package compose

import (
	"berth-agent/internal/config"
	"berth-agent/internal/utils"
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

type ComposeEvent struct {
	Type      string           `json:"type"`
	Timestamp time.Time        `json:"timestamp"`
	Status    *StatusUpdate    `json:"status,omitempty"`
	Service   *ServiceUpdate   `json:"service,omitempty"`
	Network   *NetworkUpdate   `json:"network,omitempty"`
	Container *ContainerUpdate `json:"container,omitempty"`
	Message   string           `json:"message,omitempty"`
}

type StatusUpdate struct {
	Current int `json:"current"`
	Total   int `json:"total"`
}

type ServiceUpdate struct {
	Name     string `json:"name"`
	Action   string `json:"action"`
	Progress string `json:"progress,omitempty"`
	Size     string `json:"size,omitempty"`
	Duration string `json:"duration,omitempty"`
}

type NetworkUpdate struct {
	Name     string `json:"name"`
	Action   string `json:"action"`
	Duration string `json:"duration,omitempty"`
}

type ContainerUpdate struct {
	Name     string `json:"name"`
	Action   string `json:"action"`
	Duration string `json:"duration,omitempty"`
}

func ComposeUpStreamHandler(cfg *config.AppConfig) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		stackName := utils.ExtractStackName(r, "/api/v1/stacks/")
		streamComposeOperation(w, r, cfg, stackName, "up")
	}
}

func ComposeDownStreamHandler(cfg *config.AppConfig) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		stackName := utils.ExtractStackName(r, "/api/v1/stacks/")
		streamComposeOperation(w, r, cfg, stackName, "down")
	}
}

func streamComposeOperation(w http.ResponseWriter, r *http.Request, cfg *config.AppConfig, stackName, operation string) {
	stackDir, _, err := validateStackAndFindComposeFile(cfg, stackName)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	var args []string
	switch operation {
	case "up":
		args = []string{"up", "-d"}
		if servicesParam := r.URL.Query().Get("services"); servicesParam != "" {
			services := strings.Split(servicesParam, ",")
			for _, service := range services {
				if service = strings.TrimSpace(service); service != "" {
					args = append(args, service)
				}
			}
		}
	case "down":
		args = []string{"down"}
		if r.URL.Query().Get("remove_volumes") == "true" {
			args = append(args, "-v")
		}
		if r.URL.Query().Get("remove_images") == "true" {
			args = append(args, "--rmi", "all")
		}
		if servicesParam := r.URL.Query().Get("services"); servicesParam != "" {
			services := strings.Split(servicesParam, ",")
			for _, service := range services {
				if service = strings.TrimSpace(service); service != "" {
					args = append(args, service)
				}
			}
		}
	default:
		http.Error(w, "unsupported operation", http.StatusBadRequest)
		return
	}

	if err := executeStreamingCommand(ctx, w, stackDir, args); err != nil {
		sendEvent(w, &ComposeEvent{
			Type:      "error",
			Timestamp: time.Now(),
			Message:   err.Error(),
		})
	}
}

func executeStreamingCommand(ctx context.Context, w http.ResponseWriter, stackDir string, args []string) error {
	cmd := exec.CommandContext(ctx, "docker", append([]string{"compose"}, args...)...)
	cmd.Dir = stackDir

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("failed to create stdout pipe: %v", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("failed to create stderr pipe: %v", err)
	}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start command: %v", err)
	}

	outputChan := make(chan string)
	errorChan := make(chan error)

	go func() {
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			outputChan <- scanner.Text()
		}
		if err := scanner.Err(); err != nil {
			errorChan <- err
		}
	}()

	go func() {
		scanner := bufio.NewScanner(stderr)
		for scanner.Scan() {
			outputChan <- scanner.Text()
		}
		if err := scanner.Err(); err != nil {
			errorChan <- err
		}
	}()

	go func() {
		defer close(outputChan)
		defer close(errorChan)
		cmd.Wait()
	}()

	sendEvent(w, &ComposeEvent{
		Type:      "connection",
		Timestamp: time.Now(),
		Message:   "Connected to Docker Compose",
	})

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case err := <-errorChan:
			if err != nil {
				sendEvent(w, &ComposeEvent{
					Type:      "error",
					Timestamp: time.Now(),
					Message:   err.Error(),
				})
				return err
			}
		case line, ok := <-outputChan:
			if !ok {
				sendEvent(w, &ComposeEvent{
					Type:      "complete",
					Timestamp: time.Now(),
					Message:   "Operation completed",
				})
				return nil
			}

			if event := parseDockerComposeLine(line); event != nil {
				sendEvent(w, event)
			}
		}
	}
}

func sendEvent(w http.ResponseWriter, event *ComposeEvent) {
	data, _ := json.Marshal(event)
	fmt.Fprintf(w, "data: %s\n\n", data)
	if flusher, ok := w.(http.Flusher); ok {
		flusher.Flush()
	}
}

func parseDockerComposeLine(line string) *ComposeEvent {
	line = strings.TrimSpace(line)
	if line == "" {
		return nil
	}

	timestamp := time.Now()

	if statusMatch := regexp.MustCompile(`\[.+\] Running (\d+)/(\d+)`).FindStringSubmatch(line); statusMatch != nil {
		current := 0
		total := 0
		fmt.Sscanf(statusMatch[1], "%d", &current)
		fmt.Sscanf(statusMatch[2], "%d", &total)

		return &ComposeEvent{
			Type:      "status",
			Timestamp: timestamp,
			Status: &StatusUpdate{
				Current: current,
				Total:   total,
			},
			Message: line,
		}
	}

	if serviceMatch := regexp.MustCompile(`^\s*(.+?)\s+(Pulling|Pulled|Creating|Starting|Stopping|Removing)(?:\s+(.+?))?$`).FindStringSubmatch(line); serviceMatch != nil {
		serviceName := strings.TrimSpace(serviceMatch[1])
		action := serviceMatch[2]
		extra := ""
		if len(serviceMatch) > 3 {
			extra = serviceMatch[3]
		}

		return &ComposeEvent{
			Type:      "service",
			Timestamp: timestamp,
			Service: &ServiceUpdate{
				Name:     serviceName,
				Action:   action,
				Duration: extra,
			},
			Message: line,
		}
	}

	if containerMatch := regexp.MustCompile(`Container\s+(.+?)\s+(Created|Started|Stopped|Removed|Stopping|Starting|Removing)(?:\s+(.+?))?$`).FindStringSubmatch(line); containerMatch != nil {
		containerName := containerMatch[1]
		action := containerMatch[2]
		duration := ""
		if len(containerMatch) > 3 {
			duration = containerMatch[3]
		}

		return &ComposeEvent{
			Type:      "container",
			Timestamp: timestamp,
			Container: &ContainerUpdate{
				Name:     containerName,
				Action:   action,
				Duration: duration,
			},
			Message: line,
		}
	}

	if networkMatch := regexp.MustCompile(`Network\s+(.+?)\s+(Created|Removed|Creating|Removing)(?:\s+(.+?))?$`).FindStringSubmatch(line); networkMatch != nil {
		networkName := networkMatch[1]
		action := networkMatch[2]
		duration := ""
		if len(networkMatch) > 3 {
			duration = networkMatch[3]
		}

		return &ComposeEvent{
			Type:      "network",
			Timestamp: timestamp,
			Network: &NetworkUpdate{
				Name:     networkName,
				Action:   action,
				Duration: duration,
			},
			Message: line,
		}
	}

	return &ComposeEvent{
		Type:      "log",
		Timestamp: timestamp,
		Message:   line,
	}
}
