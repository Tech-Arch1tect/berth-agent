package stack

import (
	"berth-agent/config"
	"berth-agent/internal/docker"
	"berth-agent/internal/validation"
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/docker/docker/api/types/network"
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

type NetworkIPAMConfig struct {
	Subnet  string `json:"subnet,omitempty"`
	Gateway string `json:"gateway,omitempty"`
}

type NetworkIPAM struct {
	Driver string              `json:"driver,omitempty"`
	Config []NetworkIPAMConfig `json:"config,omitempty"`
}

type NetworkEndpoint struct {
	Name        string `json:"name"`
	EndpointID  string `json:"endpoint_id,omitempty"`
	MacAddress  string `json:"mac_address,omitempty"`
	IPv4Address string `json:"ipv4_address,omitempty"`
	IPv6Address string `json:"ipv6_address,omitempty"`
}

type Network struct {
	Name       string                     `json:"name"`
	Driver     string                     `json:"driver,omitempty"`
	External   bool                       `json:"external,omitempty"`
	Labels     map[string]string          `json:"labels,omitempty"`
	Options    map[string]string          `json:"options,omitempty"`
	IPAM       *NetworkIPAM               `json:"ipam,omitempty"`
	Containers map[string]NetworkEndpoint `json:"containers,omitempty"`
	Exists     bool                       `json:"exists"`
	Created    string                     `json:"created,omitempty"`
}

type Service struct {
	stackLocation string
	commandExec   *docker.CommandExecutor
	dockerClient  *docker.Client
}

func NewService(cfg *config.Config, dockerClient *docker.Client) *Service {
	return &Service{
		stackLocation: cfg.StackLocation,
		commandExec:   docker.NewCommandExecutor(cfg.StackLocation),
		dockerClient:  dockerClient,
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
	stackPath, err := validation.SanitizeStackPath(s.stackLocation, name)
	if err != nil {
		return nil, fmt.Errorf("invalid stack name '%s': %w", name, err)
	}

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
	stackName := filepath.Base(stackPath)

	cmd, err := s.commandExec.ExecuteComposeWithFile(stackName, composeFile, "config", "--services")
	if err != nil {
		return nil, fmt.Errorf("failed to create compose command: %w", err)
	}

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
	stackName := filepath.Base(stackPath)

	cmd, err := s.commandExec.ExecuteComposeWithFile(stackName, composeFile, "config", "--format", "json")
	if err != nil {
		return "", fmt.Errorf("failed to create compose command: %w", err)
	}

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
	cmd, err := s.commandExec.ExecuteComposeCommand(stackName, "ps", "--format", "json")
	if err != nil {
		return nil, fmt.Errorf("failed to create compose command: %w", err)
	}

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
	cmd, err := s.commandExec.ExecuteComposeCommand(stackName, "ps", "-a", "--format", "json")
	if err != nil {
		return nil, fmt.Errorf("failed to create compose command: %w", err)
	}

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

func (s *Service) GetStackNetworks(name string) ([]Network, error) {
	stackPath, err := validation.SanitizeStackPath(s.stackLocation, name)
	if err != nil {
		return nil, fmt.Errorf("invalid stack name '%s': %w", name, err)
	}

	if _, err := os.Stat(stackPath); os.IsNotExist(err) {
		return nil, fmt.Errorf("stack '%s' not found", name)
	}

	composeNetworks, err := s.getComposeNetworks(stackPath)
	if err != nil {
		return nil, fmt.Errorf("failed to get compose networks: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	dockerNetworkSummaries, err := s.dockerClient.ListNetworks(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list Docker networks: %w", err)
	}

	var dockerNetworks []network.Inspect
	for _, summary := range dockerNetworkSummaries {
		if s.isStackNetwork(summary.Name, name) {
			inspectedNet, err := s.dockerClient.InspectNetwork(ctx, summary.ID)
			if err != nil {
				continue
			}
			dockerNetworks = append(dockerNetworks, inspectedNet)
		}
	}

	return s.mergeNetworkInformation(name, composeNetworks, dockerNetworks), nil
}

func (s *Service) isStackNetwork(networkName, stackName string) bool {
	stackPrefix := stackName + "_"
	defaultNetworkName := stackName + "_default"

	return networkName == defaultNetworkName ||
		strings.HasPrefix(networkName, stackPrefix) ||
		strings.Contains(networkName, stackName)
}

func (s *Service) getComposeNetworks(stackPath string) (map[string]Network, error) {
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
		return make(map[string]Network), nil
	}

	return s.parseComposeNetworks(stackPath, composeFile)
}

func (s *Service) parseComposeNetworks(stackPath, composeFile string) (map[string]Network, error) {
	stackName := filepath.Base(stackPath)

	cmd, err := s.commandExec.ExecuteComposeWithFile(stackName, composeFile, "config", "--format", "json")
	if err != nil {
		return nil, fmt.Errorf("failed to create compose command: %w", err)
	}

	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to get compose config: %w", err)
	}

	var config map[string]any
	if err := json.Unmarshal(output, &config); err != nil {
		return nil, fmt.Errorf("failed to parse compose config: %w", err)
	}

	networks := make(map[string]Network)

	if networksSection, ok := config["networks"].(map[string]any); ok {
		for networkName, networkConfig := range networksSection {
			network := Network{
				Name:   networkName,
				Exists: false,
			}

			if networkConfigMap, ok := networkConfig.(map[string]any); ok {
				if driver, ok := networkConfigMap["driver"].(string); ok {
					network.Driver = driver
				}

				if external, ok := networkConfigMap["external"].(bool); ok {
					network.External = external
				}

				if labels, ok := networkConfigMap["labels"].(map[string]any); ok {
					network.Labels = make(map[string]string)
					for k, v := range labels {
						if strV, ok := v.(string); ok {
							network.Labels[k] = strV
						}
					}
				}

				if driverOpts, ok := networkConfigMap["driver_opts"].(map[string]any); ok {
					network.Options = make(map[string]string)
					for k, v := range driverOpts {
						if strV, ok := v.(string); ok {
							network.Options[k] = strV
						}
					}
				}
			}

			networks[networkName] = network
		}
	}

	return networks, nil
}

func (s *Service) mergeNetworkInformation(stackName string, composeNetworks map[string]Network, dockerNetworks []network.Inspect) []Network {
	networkMap := make(map[string]Network)

	maps.Copy(networkMap, composeNetworks)

	stackPrefix := stackName + "_"
	defaultNetworkName := stackName + "_default"

	for _, dockerNet := range dockerNetworks {
		var net Network
		var networkKey string

		if dockerNet.Name == defaultNetworkName {
			networkKey = "default"
		} else if after, ok := strings.CutPrefix(dockerNet.Name, stackPrefix); ok {
			networkKey = after
		} else if strings.Contains(dockerNet.Name, stackName) {
			networkKey = dockerNet.Name
		} else {
			continue
		}

		if existing, exists := networkMap[networkKey]; exists {
			net = existing
		} else {
			net = Network{
				Name: networkKey,
			}
		}

		net.Exists = true
		net.Driver = dockerNet.Driver
		net.Created = dockerNet.Created.Format(time.RFC3339)

		net.IPAM = &NetworkIPAM{
			Driver: dockerNet.IPAM.Driver,
		}
		for _, ipamConfig := range dockerNet.IPAM.Config {
			net.IPAM.Config = append(net.IPAM.Config, NetworkIPAMConfig{
				Subnet:  ipamConfig.Subnet,
				Gateway: ipamConfig.Gateway,
			})
		}

		if len(dockerNet.Containers) > 0 {
			net.Containers = make(map[string]NetworkEndpoint)
			for containerID, container := range dockerNet.Containers {
				net.Containers[containerID] = NetworkEndpoint{
					Name:        container.Name,
					EndpointID:  container.EndpointID,
					MacAddress:  container.MacAddress,
					IPv4Address: container.IPv4Address,
					IPv6Address: container.IPv6Address,
				}
			}
		}

		if dockerNet.Labels != nil {
			if net.Labels == nil {
				net.Labels = make(map[string]string)
			}
			maps.Copy(net.Labels, dockerNet.Labels)
		}

		if dockerNet.Options != nil {
			if net.Options == nil {
				net.Options = make(map[string]string)
			}
			maps.Copy(net.Options, dockerNet.Options)
		}

		networkMap[networkKey] = net
	}

	var result []Network
	for _, network := range networkMap {
		result = append(result, network)
	}

	return result
}
