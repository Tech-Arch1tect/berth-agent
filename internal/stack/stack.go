package stack

import (
	"berth-agent/config"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type Stack struct {
	Name        string `json:"name"`
	Path        string `json:"path"`
	ComposeFile string `json:"compose_file"`
}

type StackDetails struct {
	Name        string           `json:"name"`
	Path        string           `json:"path"`
	ComposeFile string           `json:"compose_file"`
	Services    []ComposeService `json:"services"`
}

type ComposeService struct {
	Name       string      `json:"name"`
	Image      string      `json:"image,omitempty"`
	Containers []Container `json:"containers"`
}

type Container struct {
	Name  string `json:"name"`
	Image string `json:"image"`
	State string `json:"state"`
	Ports []Port `json:"ports,omitempty"`
}

type Port struct {
	Private int    `json:"private"`
	Public  int    `json:"public,omitempty"`
	Type    string `json:"type"`
}

type Service struct {
	stackLocation string
}

func NewService(cfg *config.Config) *Service {
	return &Service{
		stackLocation: cfg.StackLocation,
	}
}

func (s *Service) ListStacks() ([]Stack, error) {
	var stacks []Stack

	entries, err := os.ReadDir(s.stackLocation)
	if err != nil {
		return nil, err
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		stackPath := filepath.Join(s.stackLocation, entry.Name())

		composeFiles := []string{
			"docker-compose.yml",
			"docker-compose.yaml",
			"compose.yml",
			"compose.yaml",
		}

		for _, filename := range composeFiles {
			composePath := filepath.Join(stackPath, filename)
			if _, err := os.Stat(composePath); err == nil {
				stack := Stack{
					Name:        entry.Name(),
					Path:        stackPath,
					ComposeFile: filename,
				}
				stacks = append(stacks, stack)
				break
			}
		}
	}

	return stacks, nil
}

func (s *Service) GetStackDetails(name string) (*StackDetails, error) {
	stackPath := filepath.Join(s.stackLocation, name)

	if _, err := os.Stat(stackPath); os.IsNotExist(err) {
		return nil, fmt.Errorf("stack '%s' not found", name)
	}

	composeFiles := []string{
		"docker-compose.yml",
		"docker-compose.yaml",
		"compose.yml",
		"compose.yaml",
	}

	var composeFile string
	for _, filename := range composeFiles {
		composePath := filepath.Join(stackPath, filename)
		if _, err := os.Stat(composePath); err == nil {
			composeFile = filename
			break
		}
	}

	if composeFile == "" {
		return nil, fmt.Errorf("no compose file found in stack '%s'", name)
	}

	services, err := s.parseComposeServices(stackPath, composeFile)
	if err != nil {
		return nil, fmt.Errorf("failed to parse compose services: %w", err)
	}

	containers, err := s.getContainerInfo(name)
	if err != nil {
		containers = make(map[string][]Container)
	}

	allContainers, err := s.getAllContainerInfo(name)
	if err != nil {
		allContainers = make(map[string][]Container)
	}

	for i := range services {
		if containerList, exists := containers[services[i].Name]; exists {
			services[i].Containers = containerList
		} else if stoppedContainers, exists := allContainers[services[i].Name]; exists {
			services[i].Containers = stoppedContainers
		} else {
			services[i].Containers = []Container{{
				Name:  fmt.Sprintf("%s-%s-1", name, services[i].Name),
				Image: services[i].Image,
				State: "not created",
			}}
		}
	}

	return &StackDetails{
		Name:        name,
		Path:        stackPath,
		ComposeFile: composeFile,
		Services:    services,
	}, nil
}

func (s *Service) parseComposeServices(stackPath, composeFile string) ([]ComposeService, error) {
	composePath := filepath.Join(stackPath, composeFile)

	cmd := exec.Command("docker", "compose", "-f", composePath, "config", "--services")
	cmd.Dir = stackPath

	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to get compose services: %w", err)
	}

	serviceNames := strings.Split(strings.TrimSpace(string(output)), "\n")
	var services []ComposeService

	for _, serviceName := range serviceNames {
		if serviceName == "" {
			continue
		}

		image, err := s.getServiceImage(stackPath, composeFile, serviceName)
		if err != nil {
			image = ""
		}

		services = append(services, ComposeService{
			Name:       serviceName,
			Image:      image,
			Containers: []Container{},
		})
	}

	return services, nil
}

func (s *Service) getServiceImage(stackPath, composeFile, serviceName string) (string, error) {
	composePath := filepath.Join(stackPath, composeFile)

	cmd := exec.Command("docker", "compose", "-f", composePath, "config", "--format", "json")
	cmd.Dir = stackPath

	output, err := cmd.Output()
	if err != nil {
		return "", err
	}

	var config map[string]any
	if err := json.Unmarshal(output, &config); err != nil {
		return "", err
	}

	if services, ok := config["services"].(map[string]any); ok {
		if service, ok := services[serviceName].(map[string]any); ok {
			if image, ok := service["image"].(string); ok {
				return image, nil
			}
		}
	}

	return "", nil
}

func (s *Service) getContainerInfo(stackName string) (map[string][]Container, error) {
	cmd := exec.Command("docker", "compose", "ps", "--format", "json")
	cmd.Dir = filepath.Join(s.stackLocation, stackName)

	output, err := cmd.Output()
	if err != nil {
		return nil, err
	}

	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	containers := make(map[string][]Container)

	for _, line := range lines {
		if line == "" {
			continue
		}

		var containerInfo map[string]any
		if err := json.Unmarshal([]byte(line), &containerInfo); err != nil {
			continue
		}

		name, _ := containerInfo["Name"].(string)
		image, _ := containerInfo["Image"].(string)
		state, _ := containerInfo["State"].(string)
		service, _ := containerInfo["Service"].(string)

		var ports []Port
		if publishedPorts, ok := containerInfo["Publishers"]; ok {
			if portsList, ok := publishedPorts.([]any); ok {
				for _, portInfo := range portsList {
					if portMap, ok := portInfo.(map[string]any); ok {
						private, _ := portMap["TargetPort"].(float64)
						public, _ := portMap["PublishedPort"].(float64)
						protocol, _ := portMap["Protocol"].(string)

						if protocol == "" {
							protocol = "tcp"
						}

						ports = append(ports, Port{
							Private: int(private),
							Public:  int(public),
							Type:    protocol,
						})
					}
				}
			}
		}

		container := Container{
			Name:  name,
			Image: image,
			State: state,
			Ports: ports,
		}

		containers[service] = append(containers[service], container)
	}

	return containers, nil
}

func (s *Service) getAllContainerInfo(stackName string) (map[string][]Container, error) {
	cmd := exec.Command("docker", "compose", "ps", "-a", "--format", "json")
	cmd.Dir = filepath.Join(s.stackLocation, stackName)

	output, err := cmd.Output()
	if err != nil {
		return nil, err
	}

	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	containers := make(map[string][]Container)

	for _, line := range lines {
		if line == "" {
			continue
		}

		var containerInfo map[string]any
		if err := json.Unmarshal([]byte(line), &containerInfo); err != nil {
			continue
		}

		name, _ := containerInfo["Name"].(string)
		image, _ := containerInfo["Image"].(string)
		state, _ := containerInfo["State"].(string)
		service, _ := containerInfo["Service"].(string)

		var ports []Port
		if publishedPorts, ok := containerInfo["Publishers"]; ok {
			if portsList, ok := publishedPorts.([]any); ok {
				for _, portInfo := range portsList {
					if portMap, ok := portInfo.(map[string]any); ok {
						private, _ := portMap["TargetPort"].(float64)
						public, _ := portMap["PublishedPort"].(float64)
						protocol, _ := portMap["Protocol"].(string)

						if protocol == "" {
							protocol = "tcp"
						}

						ports = append(ports, Port{
							Private: int(private),
							Public:  int(public),
							Type:    protocol,
						})
					}
				}
			}
		}

		container := Container{
			Name:  name,
			Image: image,
			State: state,
			Ports: ports,
		}

		containers[service] = append(containers[service], container)
	}

	return containers, nil
}
