package compose

import (
	"berth-agent/internal/config"
	"berth-agent/internal/utils"
	"bufio"
	"context"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"strings"
)


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

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
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
		fmt.Fprintf(w, "Error: %s\n", err.Error())
	}
}

func executeStreamingCommand(ctx context.Context, w http.ResponseWriter, stackDir string, args []string) error {
	dockerArgs := append([]string{"compose", "--ansi=always"}, args...)
	cmd := exec.CommandContext(ctx, "docker", dockerArgs...)
	cmd.Dir = stackDir
	cmd.Env = append(os.Environ(), "TERM=xterm-256color", "FORCE_COLOR=1")

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

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case err := <-errorChan:
			if err != nil {
				return err
			}
		case line, ok := <-outputChan:
			if !ok {
				return nil
			}
			
			fmt.Fprintf(w, "%s\n", line)
			if flusher, ok := w.(http.Flusher); ok {
				flusher.Flush()
			}
		}
	}
}
