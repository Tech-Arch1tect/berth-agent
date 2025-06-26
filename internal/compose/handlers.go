package compose

import (
	"berth-agent/internal/config"
	"berth-agent/internal/utils"
	"bufio"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

var composeFileNames = []string{
	"compose.yml",
	"compose.yaml",
	"docker-compose.yml",
	"docker-compose.yaml",
}

type ComposeError struct {
	Error string `json:"error"`
	Stack string `json:"stack"`
}

func validateStackAndFindComposeFile(cfg *config.AppConfig, stackName string) (string, string, error) {
	if stackName == "" {
		return "", "", fmt.Errorf("stack name is required")
	}

	stackDir := filepath.Join(cfg.ComposeDirPath, stackName)
	if _, err := os.Stat(stackDir); os.IsNotExist(err) {
		return "", "", fmt.Errorf("stack not found")
	}

	for _, fileName := range composeFileNames {
		composeFile := filepath.Join(stackDir, fileName)
		if _, err := os.Stat(composeFile); err == nil {
			return stackDir, fileName, nil
		}
	}

	return "", "", fmt.Errorf("no compose file found (checked: %s)", strings.Join(composeFileNames, ", "))
}

func runDockerCompose(stackDir string, args ...string) ([]byte, error) {
	cmd := exec.Command("docker", append([]string{"compose"}, args...)...)
	cmd.Dir = stackDir
	return cmd.CombinedOutput()
}

func writeError(w http.ResponseWriter, statusCode int, message, stackName string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	json.NewEncoder(w).Encode(ComposeError{
		Error: message,
		Stack: stackName,
	})
}

func ComposeInfoHandler(cfg *config.AppConfig) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		stackName := utils.ExtractStackName(r, "/api/v1/stacks/")
		ComposeInfo(w, r, cfg, stackName)
	}
}

type ComposeInfoResponse struct {
	Stack      string `json:"stack"`
	Version    string `json:"version"`
	ConfigFile string `json:"config_file"`
	WorkingDir string `json:"working_dir"`
}

func ComposeInfo(w http.ResponseWriter, r *http.Request, cfg *config.AppConfig, stackName string) {
	stackDir, configFile, err := validateStackAndFindComposeFile(cfg, stackName)
	if err != nil {
		if strings.Contains(err.Error(), "stack not found") {
			writeError(w, http.StatusNotFound, err.Error(), stackName)
		} else if strings.Contains(err.Error(), "no compose file") {
			writeError(w, http.StatusNotFound, err.Error(), stackName)
		} else {
			writeError(w, http.StatusBadRequest, err.Error(), stackName)
		}
		return
	}

	versionOutput, err := runDockerCompose(stackDir, "version", "--short")
	version := "unknown"
	if err == nil {
		version = strings.TrimSpace(string(versionOutput))
	}

	response := ComposeInfoResponse{
		Stack:      stackName,
		Version:    version,
		ConfigFile: configFile,
		WorkingDir: stackDir,
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)
}

func ComposeExecHandler(cfg *config.AppConfig) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		stackName := utils.ExtractStackName(r, "/api/v1/stacks/")
		ComposeExec(w, r, cfg, stackName)
	}
}

type ComposeExecRequest struct {
	Service string   `json:"service"`
	Command []string `json:"command"`
}

type ComposeExecResponse struct {
	Stack   string `json:"stack"`
	Service string `json:"service"`
	Command string `json:"command"`
	Output  string `json:"output"`
	Error   string `json:"error,omitempty"`
}

func ComposeExec(w http.ResponseWriter, r *http.Request, cfg *config.AppConfig, stackName string) {
	stackDir, _, err := validateStackAndFindComposeFile(cfg, stackName)
	if err != nil {
		if strings.Contains(err.Error(), "stack not found") {
			writeError(w, http.StatusNotFound, err.Error(), stackName)
		} else {
			writeError(w, http.StatusBadRequest, err.Error(), stackName)
		}
		return
	}

	var req ComposeExecRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid JSON request body", stackName)
		return
	}

	if req.Service == "" || len(req.Command) == 0 {
		writeError(w, http.StatusBadRequest, "Service and command are required", stackName)
		return
	}

	args := append([]string{"exec", "-T", req.Service}, req.Command...)
	output, err := runDockerCompose(stackDir, args...)

	response := ComposeExecResponse{
		Stack:   stackName,
		Service: req.Service,
		Command: strings.Join(req.Command, " "),
		Output:  string(output),
	}

	if err != nil {
		response.Error = err.Error()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
	} else {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
	}

	json.NewEncoder(w).Encode(response)
}

func ComposePsHandler(cfg *config.AppConfig) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		stackName := utils.ExtractStackName(r, "/api/v1/stacks/")
		ComposePs(w, r, cfg, stackName)
	}
}

type ComposeService struct {
	Name    string `json:"name"`
	Command string `json:"command"`
	State   string `json:"state"`
	Ports   string `json:"ports"`
}

type ComposePsResponse struct {
	Stack    string           `json:"stack"`
	Services []ComposeService `json:"services"`
}

func ComposePs(w http.ResponseWriter, r *http.Request, cfg *config.AppConfig, stackName string) {
	stackDir, _, err := validateStackAndFindComposeFile(cfg, stackName)
	if err != nil {
		if strings.Contains(err.Error(), "stack not found") {
			writeError(w, http.StatusNotFound, err.Error(), stackName)
		} else {
			writeError(w, http.StatusBadRequest, err.Error(), stackName)
		}
		return
	}

	output, err := runDockerCompose(stackDir, "ps", "--format", "json")
	if err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to get service status: %v", err), stackName)
		return
	}

	var services []ComposeService
	scanner := bufio.NewScanner(strings.NewReader(string(output)))
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}
		var service map[string]interface{}
		if err := json.Unmarshal([]byte(line), &service); err == nil {
			composeService := ComposeService{
				Name:    getString(service, "Name"),
				Command: getString(service, "Command"),
				State:   getString(service, "State"),
				Ports:   getString(service, "Ports"),
			}
			services = append(services, composeService)
		}
	}

	response := ComposePsResponse{
		Stack:    stackName,
		Services: services,
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)
}

func ComposeLogsHandler(cfg *config.AppConfig) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		stackName := utils.ExtractStackName(r, "/api/v1/stacks/")
		ComposeLogs(w, r, cfg, stackName)
	}
}

type ComposeLogsResponse struct {
	Stack   string `json:"stack"`
	Service string `json:"service,omitempty"`
	Lines   int    `json:"lines"`
	Logs    string `json:"logs"`
}

func ComposeLogs(w http.ResponseWriter, r *http.Request, cfg *config.AppConfig, stackName string) {
	stackDir, _, err := validateStackAndFindComposeFile(cfg, stackName)
	if err != nil {
		if strings.Contains(err.Error(), "stack not found") {
			writeError(w, http.StatusNotFound, err.Error(), stackName)
		} else {
			writeError(w, http.StatusBadRequest, err.Error(), stackName)
		}
		return
	}

	service := r.URL.Query().Get("service")
	tail := r.URL.Query().Get("tail")
	if tail == "" {
		tail = "100"
	}

	var args []string
	if service != "" {
		args = []string{"logs", "--tail", tail, service}
	} else {
		args = []string{"logs", "--tail", tail}
	}

	output, err := runDockerCompose(stackDir, args...)
	if err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to get logs: %v", err), stackName)
		return
	}

	lines := strings.Count(string(output), "\n")
	response := ComposeLogsResponse{
		Stack:   stackName,
		Service: service,
		Lines:   lines,
		Logs:    string(output),
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)
}

func ComposeUpHandler(cfg *config.AppConfig) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		stackName := utils.ExtractStackName(r, "/api/v1/stacks/")
		ComposeUp(w, r, cfg, stackName)
	}
}

type ComposeUpRequest struct {
	Services []string `json:"services,omitempty"`
}

type ComposeUpResponse struct {
	Stack    string `json:"stack"`
	Message  string `json:"message"`
	Services string `json:"services,omitempty"`
	Output   string `json:"output"`
}

func ComposeUp(w http.ResponseWriter, r *http.Request, cfg *config.AppConfig, stackName string) {
	stackDir, _, err := validateStackAndFindComposeFile(cfg, stackName)
	if err != nil {
		if strings.Contains(err.Error(), "stack not found") {
			writeError(w, http.StatusNotFound, err.Error(), stackName)
		} else {
			writeError(w, http.StatusBadRequest, err.Error(), stackName)
		}
		return
	}

	var req ComposeUpRequest
	if r.Body != nil {
		json.NewDecoder(r.Body).Decode(&req)
	}

	args := []string{"up", "-d"}
	if len(req.Services) > 0 {
		args = append(args, req.Services...)
	}

	output, err := runDockerCompose(stackDir, args...)

	response := ComposeUpResponse{
		Stack:  stackName,
		Output: string(output),
	}

	if len(req.Services) > 0 {
		response.Services = strings.Join(req.Services, ", ")
		response.Message = fmt.Sprintf("Started services: %s", response.Services)
	} else {
		response.Message = "Started all services"
	}

	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"error":  fmt.Sprintf("Failed to start services: %v", err),
			"stack":  stackName,
			"output": string(output),
		})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)
}

func ComposeDownHandler(cfg *config.AppConfig) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		stackName := utils.ExtractStackName(r, "/api/v1/stacks/")
		ComposeDown(w, r, cfg, stackName)
	}
}

type ComposeDownRequest struct {
	Services      []string `json:"services,omitempty"`
	RemoveVolumes bool     `json:"remove_volumes"`
	RemoveImages  bool     `json:"remove_images"`
}

type ComposeDownResponse struct {
	Stack    string `json:"stack"`
	Message  string `json:"message"`
	Services string `json:"services,omitempty"`
	Output   string `json:"output"`
}

func ComposeDown(w http.ResponseWriter, r *http.Request, cfg *config.AppConfig, stackName string) {
	stackDir, _, err := validateStackAndFindComposeFile(cfg, stackName)
	if err != nil {
		if strings.Contains(err.Error(), "stack not found") {
			writeError(w, http.StatusNotFound, err.Error(), stackName)
		} else {
			writeError(w, http.StatusBadRequest, err.Error(), stackName)
		}
		return
	}

	var req ComposeDownRequest
	if r.Body != nil {
		json.NewDecoder(r.Body).Decode(&req)
	}

	args := []string{"down"}
	if req.RemoveVolumes {
		args = append(args, "-v")
	}
	if req.RemoveImages {
		args = append(args, "--rmi", "all")
	}
	if len(req.Services) > 0 {
		args = append(args, req.Services...)
	}

	output, err := runDockerCompose(stackDir, args...)

	response := ComposeDownResponse{
		Stack:  stackName,
		Output: string(output),
	}

	if len(req.Services) > 0 {
		response.Services = strings.Join(req.Services, ", ")
		response.Message = fmt.Sprintf("Stopped and removed services: %s", response.Services)
	} else {
		response.Message = "Stopped and removed all services"
	}

	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"error":  fmt.Sprintf("Failed to stop services: %v", err),
			"stack":  stackName,
			"output": string(output),
		})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)
}

func getString(m map[string]interface{}, key string) string {
	if val, ok := m[key]; ok {
		if str, ok := val.(string); ok {
			return str
		}
	}
	return ""
}
