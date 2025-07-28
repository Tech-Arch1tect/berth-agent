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

func ValidateStackAndFindComposeFile(cfg *config.AppConfig, stackName string) (string, string, error) {
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
	stackDir, configFile, err := ValidateStackAndFindComposeFile(cfg, stackName)
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

func ComposePsHandler(cfg *config.AppConfig) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		stackName := utils.ExtractStackName(r, "/api/v1/stacks/")
		ComposePs(w, r, cfg, stackName)
	}
}

type NetworkInfo struct {
	Name      string `json:"name"`
	IPAddress string `json:"ip_address"`
	Gateway   string `json:"gateway"`
}

type ComposeService struct {
	ID       string        `json:"id"`
	Name     string        `json:"name"`
	Command  string        `json:"command"`
	State    string        `json:"state"`
	Ports    string        `json:"ports"`
	Image    string        `json:"image"`
	Networks []NetworkInfo `json:"networks"`
}

type ComposePsResponse struct {
	Stack    string           `json:"stack"`
	Services []ComposeService `json:"services"`
}

func ComposePs(w http.ResponseWriter, r *http.Request, cfg *config.AppConfig, stackName string) {
	response, err := GetStackServices(cfg, stackName)
	if err != nil {
		if strings.Contains(err.Error(), "stack not found") {
			writeError(w, http.StatusNotFound, err.Error(), stackName)
		} else if strings.Contains(err.Error(), "Failed to get service status") {
			writeError(w, http.StatusInternalServerError, err.Error(), stackName)
		} else {
			writeError(w, http.StatusBadRequest, err.Error(), stackName)
		}
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)
}

func GetStackServices(cfg *config.AppConfig, stackName string) (*ComposePsResponse, error) {
	stackDir, _, err := ValidateStackAndFindComposeFile(cfg, stackName)
	if err != nil {
		return nil, err
	}

	output, err := runDockerCompose(stackDir, "ps", "--format", "json", "--no-trunc")
	if err != nil {
		return nil, fmt.Errorf("Failed to get service status: %v", err)
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
			containerID := getString(service, "ID")
			networks := getContainerNetworks(containerID)

			composeService := ComposeService{
				ID:       containerID,
				Name:     getString(service, "Name"),
				Command:  getString(service, "Command"),
				State:    getString(service, "State"),
				Ports:    getString(service, "Ports"),
				Image:    getString(service, "Image"),
				Networks: networks,
			}
			services = append(services, composeService)
		}
	}

	return &ComposePsResponse{
		Stack:    stackName,
		Services: services,
	}, nil
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
	stackDir, _, err := ValidateStackAndFindComposeFile(cfg, stackName)
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

func getString(m map[string]interface{}, key string) string {
	if val, ok := m[key]; ok {
		if str, ok := val.(string); ok {
			return str
		}
	}
	return ""
}

func getContainerNetworks(containerID string) []NetworkInfo {
	if containerID == "" {
		return []NetworkInfo{}
	}

	cmd := exec.Command("docker", "inspect", containerID, "--format", "{{json .NetworkSettings.Networks}}")
	output, err := cmd.Output()
	if err != nil {
		return []NetworkInfo{}
	}

	var networks map[string]interface{}
	if err := json.Unmarshal(output, &networks); err != nil {
		return []NetworkInfo{}
	}

	var networkInfos []NetworkInfo
	for networkName, networkData := range networks {
		if networkMap, ok := networkData.(map[string]interface{}); ok {
			info := NetworkInfo{
				Name:      networkName,
				IPAddress: getStringFromInterface(networkMap, "IPAddress"),
				Gateway:   getStringFromInterface(networkMap, "Gateway"),
			}
			if info.IPAddress != "" {
				networkInfos = append(networkInfos, info)
			}
		}
	}

	return networkInfos
}

func getStringFromInterface(m map[string]interface{}, key string) string {
	if val, ok := m[key]; ok {
		if str, ok := val.(string); ok {
			return str
		}
	}
	return ""
}
