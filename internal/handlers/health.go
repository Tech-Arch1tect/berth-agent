package handlers

import (
	"encoding/json"
	"net/http"
	"os/exec"
)

type HealthResponse struct {
	Status         string            `json:"status"`
	Service        string            `json:"service"`
	DockerCompose  DockerComposeInfo `json:"docker_compose"`
}

type DockerComposeInfo struct {
	Available bool   `json:"available"`
	Version   string `json:"version,omitempty"`
	Error     string `json:"error,omitempty"`
}

func Health(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	
	composeInfo := checkDockerCompose()
	
	status := "healthy"
	if !composeInfo.Available {
		status = "degraded"
	}
	
	response := HealthResponse{
		Status:        status,
		Service:       "berth-agent",
		DockerCompose: composeInfo,
	}
	
	if status == "degraded" {
		w.WriteHeader(http.StatusServiceUnavailable)
	}
	
	json.NewEncoder(w).Encode(response)
}

func checkDockerCompose() DockerComposeInfo {
	cmd := exec.Command("docker", "compose", "version", "--short")
	output, err := cmd.Output()
	
	if err != nil {
		return DockerComposeInfo{
			Available: false,
			Error:     "docker compose not available: " + err.Error(),
		}
	}
	
	version := string(output)
	if len(version) > 0 && version[len(version)-1] == '\n' {
		version = version[:len(version)-1]
	}
	
	return DockerComposeInfo{
		Available: true,
		Version:   version,
	}
}