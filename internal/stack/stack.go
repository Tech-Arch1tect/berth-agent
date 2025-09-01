package stack

import (
	"berth-agent/config"
	"berth-agent/internal/docker"
	"berth-agent/internal/validation"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"maps"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/volume"
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
	Name           string             `json:"name"`
	Image          string             `json:"image"`
	State          string             `json:"state"`
	Ports          []Port             `json:"ports,omitempty"`
	Created        string             `json:"created,omitempty"`
	Started        string             `json:"started,omitempty"`
	Finished       string             `json:"finished,omitempty"`
	ExitCode       int                `json:"exit_code,omitempty"`
	RestartPolicy  *RestartPolicy     `json:"restart_policy,omitempty"`
	ResourceLimits *ResourceLimits    `json:"resource_limits,omitempty"`
	Health         *HealthStatus      `json:"health,omitempty"`
	Command        []string           `json:"command,omitempty"`
	WorkingDir     string             `json:"working_dir,omitempty"`
	User           string             `json:"user,omitempty"`
	Labels         map[string]string  `json:"labels,omitempty"`
	Networks       []ContainerNetwork `json:"networks,omitempty"`
	Mounts         []ContainerMount   `json:"mounts,omitempty"`
}

type ContainerNetwork struct {
	Name       string   `json:"name"`
	NetworkID  string   `json:"network_id,omitempty"`
	IPAddress  string   `json:"ip_address,omitempty"`
	Gateway    string   `json:"gateway,omitempty"`
	MacAddress string   `json:"mac_address,omitempty"`
	Aliases    []string `json:"aliases,omitempty"`
}

type ContainerMount struct {
	Type        string `json:"type"`
	Source      string `json:"source"`
	Destination string `json:"destination"`
	Driver      string `json:"driver,omitempty"`
	Mode        string `json:"mode,omitempty"`
	RW          bool   `json:"rw"`
	Propagation string `json:"propagation,omitempty"`
}

type RestartPolicy struct {
	Name              string `json:"name"`
	MaximumRetryCount int    `json:"maximum_retry_count,omitempty"`
}

type ResourceLimits struct {
	CPUShares  int64 `json:"cpu_shares,omitempty"`
	Memory     int64 `json:"memory,omitempty"`
	MemorySwap int64 `json:"memory_swap,omitempty"`
	CPUQuota   int64 `json:"cpu_quota,omitempty"`
	CPUPeriod  int64 `json:"cpu_period,omitempty"`
}

type HealthStatus struct {
	Status        string      `json:"status"`
	FailingStreak int         `json:"failing_streak,omitempty"`
	Log           []HealthLog `json:"log,omitempty"`
}

type HealthLog struct {
	Start    string `json:"start"`
	End      string `json:"end,omitempty"`
	ExitCode int    `json:"exit_code"`
	Output   string `json:"output"`
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

type VolumeMount struct {
	Type         string            `json:"type"`
	Source       string            `json:"source"`
	Target       string            `json:"target"`
	ReadOnly     bool              `json:"read_only,omitempty"`
	BindOptions  map[string]string `json:"bind_options,omitempty"`
	TmpfsOptions map[string]string `json:"tmpfs_options,omitempty"`
}

type VolumeUsage struct {
	ContainerName string        `json:"container_name"`
	ServiceName   string        `json:"service_name"`
	Mounts        []VolumeMount `json:"mounts"`
}

type Volume struct {
	Name       string            `json:"name"`
	Driver     string            `json:"driver,omitempty"`
	External   bool              `json:"external,omitempty"`
	Labels     map[string]string `json:"labels,omitempty"`
	DriverOpts map[string]string `json:"driver_opts,omitempty"`
	Exists     bool              `json:"exists"`
	Created    string            `json:"created,omitempty"`
	Mountpoint string            `json:"mountpoint,omitempty"`
	Scope      string            `json:"scope,omitempty"`
	UsedBy     []VolumeUsage     `json:"used_by,omitempty"`
}

type EnvironmentVariable struct {
	Key             string `json:"key"`
	Value           string `json:"value"`
	IsSensitive     bool   `json:"is_sensitive"`
	Source          string `json:"source"`
	IsFromContainer bool   `json:"is_from_container"`
}

type ServiceEnvironment struct {
	ServiceName string                `json:"service_name"`
	Variables   []EnvironmentVariable `json:"variables"`
}

type StackSummary struct {
	TotalStacks     int `json:"total_stacks"`
	HealthyStacks   int `json:"healthy_stacks"`
	UnhealthyStacks int `json:"unhealthy_stacks"`
}

type Service struct {
	stackLocation string
	commandExec   *docker.CommandExecutor
	dockerClient  *docker.Client
	serviceCache  *ServiceCountCache
}

func NewService(cfg *config.Config, dockerClient *docker.Client) *Service {
	cache := NewServiceCountCache(cfg.StackLocation)

	service := &Service{
		stackLocation: cfg.StackLocation,
		commandExec:   docker.NewCommandExecutor(cfg.StackLocation),
		dockerClient:  dockerClient,
		serviceCache:  cache,
	}

	if err := cache.Start(); err != nil {
		log.Printf("Warning: failed to start service count cache: %v", err)
	}

	return service
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

	sort.Slice(stacks, func(i, j int) bool {
		return stacks[i].Name < stacks[j].Name
	})

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

	type composeData struct {
		services []ComposeService
		err      error
	}

	type containerData struct {
		containers map[string][]Container
		err        error
	}

	composeChan := make(chan composeData, 1)
	containerChan := make(chan containerData, 1)

	go func() {
		services, err := s.parseComposeServicesAndImages(stackPath, composeFile)
		composeChan <- composeData{services: services, err: err}
	}()

	go func() {
		containers, err := s.getContainerInfoViaAPI(name)
		containerChan <- containerData{containers: containers, err: err}
	}()

	composeResult := <-composeChan
	containerResult := <-containerChan

	if composeResult.err != nil {
		return nil, fmt.Errorf("failed to parse compose data: %w", composeResult.err)
	}

	services := composeResult.services
	containers := containerResult.containers
	if containerResult.err != nil {
		containers = make(map[string][]Container)
	}

	for i := range services {
		if containerList, exists := containers[services[i].Name]; exists {
			services[i].Containers = containerList
		} else {
			services[i].Containers = []Container{{
				Name:  fmt.Sprintf("%s-%s-1", name, services[i].Name),
				Image: services[i].Image,
				State: "not created",
			}}
		}

		sort.Slice(services[i].Containers, func(a, b int) bool {
			return services[i].Containers[a].Name < services[i].Containers[b].Name
		})

		for j := range services[i].Containers {
			sort.Slice(services[i].Containers[j].Ports, func(a, b int) bool {
				if services[i].Containers[j].Ports[a].Private != services[i].Containers[j].Ports[b].Private {
					return services[i].Containers[j].Ports[a].Private < services[i].Containers[j].Ports[b].Private
				}
				return services[i].Containers[j].Ports[a].Type < services[i].Containers[j].Ports[b].Type
			})
		}
	}

	sort.Slice(services, func(i, j int) bool {
		return services[i].Name < services[j].Name
	})

	return &StackDetails{
		Name:        name,
		Path:        stackPath,
		ComposeFile: composeFile,
		Services:    services,
	}, nil
}

func (s *Service) GetContainerInfo(stackName string) (map[string][]Container, error) {
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

		sort.Slice(ports, func(i, j int) bool {
			if ports[i].Private != ports[j].Private {
				return ports[i].Private < ports[j].Private
			}
			return ports[i].Type < ports[j].Type
		})

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

func (s *Service) parseComposeServicesAndImages(stackPath, composeFile string) ([]ComposeService, error) {
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

	var services []ComposeService
	if servicesSection, ok := config["services"].(map[string]any); ok {
		for serviceName, serviceConfig := range servicesSection {
			service := ComposeService{
				Name:       serviceName,
				Containers: []Container{},
			}

			if serviceConfigMap, ok := serviceConfig.(map[string]any); ok {
				if image, ok := serviceConfigMap["image"].(string); ok {
					service.Image = image
				}
			}

			services = append(services, service)
		}
	}

	return services, nil
}

func (s *Service) getContainerInfoViaAPI(stackName string) (map[string][]Container, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	filters := map[string][]string{
		"label": {fmt.Sprintf("com.docker.compose.project=%s", stackName)},
	}

	apiContainers, err := s.dockerClient.ContainerList(ctx, filters)
	if err != nil {
		return nil, fmt.Errorf("failed to list containers via Docker API: %w", err)
	}

	containers := make(map[string][]Container)

	for _, apiContainer := range apiContainers {
		serviceName := apiContainer.Labels["com.docker.compose.service"]
		if serviceName == "" {
			continue
		}

		var ports []Port
		for _, port := range apiContainer.Ports {
			ports = append(ports, Port{
				Private: int(port.PrivatePort),
				Public:  int(port.PublicPort),
				Type:    port.Type,
			})
		}

		sort.Slice(ports, func(i, j int) bool {
			if ports[i].Private != ports[j].Private {
				return ports[i].Private < ports[j].Private
			}
			return ports[i].Type < ports[j].Type
		})

		var containerName string
		if len(apiContainer.Names) > 0 {
			containerName = strings.TrimPrefix(apiContainer.Names[0], "/")
		}

		container := Container{
			Name:  containerName,
			Image: apiContainer.Image,
			State: apiContainer.State,
			Ports: ports,
		}

		inspectCtx, inspectCancel := context.WithTimeout(context.Background(), 10*time.Second)
		containerDetails, err := s.dockerClient.ContainerInspect(inspectCtx, apiContainer.ID)
		inspectCancel()

		if err == nil {
			container.Created = containerDetails.Created
			if containerDetails.State.StartedAt != "" {
				container.Started = containerDetails.State.StartedAt
			}
			if containerDetails.State.FinishedAt != "" && containerDetails.State.FinishedAt != "0001-01-01T00:00:00Z" {
				container.Finished = containerDetails.State.FinishedAt
			}
			container.ExitCode = containerDetails.State.ExitCode

			if containerDetails.HostConfig.RestartPolicy.Name != "" {
				container.RestartPolicy = &RestartPolicy{
					Name:              string(containerDetails.HostConfig.RestartPolicy.Name),
					MaximumRetryCount: containerDetails.HostConfig.RestartPolicy.MaximumRetryCount,
				}
			}

			resourceLimits := &ResourceLimits{}
			hasLimits := false

			if containerDetails.HostConfig.CPUShares > 0 {
				resourceLimits.CPUShares = containerDetails.HostConfig.CPUShares
				hasLimits = true
			}
			if containerDetails.HostConfig.Memory > 0 {
				resourceLimits.Memory = containerDetails.HostConfig.Memory
				hasLimits = true
			}
			if containerDetails.HostConfig.MemorySwap > 0 {
				resourceLimits.MemorySwap = containerDetails.HostConfig.MemorySwap
				hasLimits = true
			}
			if containerDetails.HostConfig.CPUQuota > 0 {
				resourceLimits.CPUQuota = containerDetails.HostConfig.CPUQuota
				hasLimits = true
			}
			if containerDetails.HostConfig.CPUPeriod > 0 {
				resourceLimits.CPUPeriod = containerDetails.HostConfig.CPUPeriod
				hasLimits = true
			}

			if hasLimits {
				container.ResourceLimits = resourceLimits
			}

			if containerDetails.State.Health != nil {
				healthLogs := make([]HealthLog, 0)
				for _, logEntry := range containerDetails.State.Health.Log {
					healthLogs = append(healthLogs, HealthLog{
						Start:    logEntry.Start.Format(time.RFC3339),
						End:      logEntry.End.Format(time.RFC3339),
						ExitCode: logEntry.ExitCode,
						Output:   logEntry.Output,
					})
				}

				container.Health = &HealthStatus{
					Status:        containerDetails.State.Health.Status,
					FailingStreak: containerDetails.State.Health.FailingStreak,
					Log:           healthLogs,
				}
			}

			if len(containerDetails.Config.Cmd) > 0 {
				container.Command = containerDetails.Config.Cmd
			} else if len(containerDetails.Config.Entrypoint) > 0 {
				container.Command = containerDetails.Config.Entrypoint
			}

			if containerDetails.Config.WorkingDir != "" {
				container.WorkingDir = containerDetails.Config.WorkingDir
			}

			if containerDetails.Config.User != "" {
				container.User = containerDetails.Config.User
			}

			container.Labels = containerDetails.Config.Labels

			var networks []ContainerNetwork
			for networkName, networkInfo := range containerDetails.NetworkSettings.Networks {
				network := ContainerNetwork{
					Name:       networkName,
					NetworkID:  networkInfo.NetworkID,
					IPAddress:  networkInfo.IPAddress,
					Gateway:    networkInfo.Gateway,
					MacAddress: networkInfo.MacAddress,
					Aliases:    networkInfo.Aliases,
				}
				networks = append(networks, network)
			}
			container.Networks = networks

			var mounts []ContainerMount
			for _, mount := range containerDetails.Mounts {
				containerMount := ContainerMount{
					Type:        string(mount.Type),
					Source:      mount.Source,
					Destination: mount.Destination,
					Driver:      mount.Driver,
					Mode:        mount.Mode,
					RW:          mount.RW,
					Propagation: string(mount.Propagation),
				}
				mounts = append(mounts, containerMount)
			}
			container.Mounts = mounts
		}

		containers[serviceName] = append(containers[serviceName], container)
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

	sort.Slice(result, func(i, j int) bool {
		return result[i].Name < result[j].Name
	})

	return result
}

func (s *Service) GetStackVolumes(name string) ([]Volume, error) {
	stackPath, err := validation.SanitizeStackPath(s.stackLocation, name)
	if err != nil {
		return nil, fmt.Errorf("invalid stack name '%s': %w", name, err)
	}

	if _, err := os.Stat(stackPath); os.IsNotExist(err) {
		return nil, fmt.Errorf("stack '%s' not found", name)
	}

	composeVolumes, err := s.getComposeVolumes(stackPath)
	if err != nil {
		return nil, fmt.Errorf("failed to get compose volumes: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	dockerVolumeList, err := s.dockerClient.ListVolumes(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list Docker volumes: %w", err)
	}

	var dockerVolumes []*volume.Volume
	for _, vol := range dockerVolumeList.Volumes {
		if s.isStackVolume(vol.Name, name) {
			dockerVolumes = append(dockerVolumes, vol)
		}
	}

	containerInfo, err := s.getStackContainerVolumes(name)
	if err != nil {
		return nil, fmt.Errorf("failed to get container volume info: %w", err)
	}

	return s.mergeVolumeInformation(name, composeVolumes, dockerVolumes, containerInfo), nil
}

func (s *Service) isStackVolume(volumeName, stackName string) bool {
	stackPrefix := stackName + "_"

	return strings.HasPrefix(volumeName, stackPrefix) ||
		strings.Contains(volumeName, stackName)
}

func (s *Service) getComposeVolumes(stackPath string) (map[string]Volume, error) {
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
		return make(map[string]Volume), nil
	}

	return s.parseComposeVolumes(stackPath, composeFile)
}

func (s *Service) parseComposeVolumes(stackPath, composeFile string) (map[string]Volume, error) {
	stackName := filepath.Base(stackPath)

	cmd, err := s.commandExec.ExecuteComposeWithFile(stackName, composeFile, "config", "--format", "json")
	if err != nil {
		return nil, fmt.Errorf("failed to create compose command: %w", err)
	}

	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to execute compose config: %w", err)
	}

	var composeData struct {
		Volumes  map[string]any `json:"volumes"`
		Services map[string]any `json:"services"`
	}

	if err := json.Unmarshal(output, &composeData); err != nil {
		return nil, fmt.Errorf("failed to unmarshal compose config: %w", err)
	}

	volumes := make(map[string]Volume)

	for volumeName, volumeConfig := range composeData.Volumes {
		vol := Volume{
			Name:   volumeName,
			Exists: false,
		}

		if config, ok := volumeConfig.(map[string]any); ok {
			if driver, ok := config["driver"].(string); ok {
				vol.Driver = driver
			}

			if external, ok := config["external"].(bool); ok {
				vol.External = external
			}

			if labels, ok := config["labels"].(map[string]any); ok {
				vol.Labels = make(map[string]string)
				for k, v := range labels {
					if str, ok := v.(string); ok {
						vol.Labels[k] = str
					}
				}
			}

			if driverOpts, ok := config["driver_opts"].(map[string]any); ok {
				vol.DriverOpts = make(map[string]string)
				for k, v := range driverOpts {
					if str, ok := v.(string); ok {
						vol.DriverOpts[k] = str
					}
				}
			}
		}

		volumes[volumeName] = vol
	}

	for _, serviceConfig := range composeData.Services {
		if serviceData, ok := serviceConfig.(map[string]any); ok {
			if volumesList, ok := serviceData["volumes"].([]any); ok {
				for _, volumeEntry := range volumesList {
					var volumeKey string
					var volumeSource string
					var volumeType = "bind"

					if volumeStr, ok := volumeEntry.(string); ok {

						parts := strings.Split(volumeStr, ":")
						if len(parts) >= 2 {
							source := parts[0]

							if strings.HasPrefix(source, "/") || strings.HasPrefix(source, "./") || strings.HasPrefix(source, "../") || strings.HasPrefix(source, "~") {

								volumeKey = "bind:" + source
								volumeSource = source
								volumeType = "bind"
							} else {

								volumeKey = source
								volumeSource = source
								volumeType = "volume"
							}
						}
					} else if volumeData, ok := volumeEntry.(map[string]any); ok {

						if source, ok := volumeData["source"].(string); ok {
							volumeSource = source
							if volType, ok := volumeData["type"].(string); ok {
								volumeType = volType
							}

							if volumeType == "bind" {
								volumeKey = "bind:" + source
							} else {
								volumeKey = source
							}
						}
					}

					if volumeKey != "" && volumeSource != "" {
						if _, exists := volumes[volumeKey]; exists {

							continue
						}

						vol := Volume{
							Name:   volumeKey,
							Exists: false,
						}

						switch volumeType {
						case "bind":
							vol.Driver = "bind"
						case "tmpfs":
							vol.Driver = "tmpfs"
						default:
							vol.Driver = "local"
						}

						volumes[volumeKey] = vol
					}
				}
			}
		}
	}

	return volumes, nil
}

func (s *Service) getStackContainerVolumes(stackName string) (map[string][]VolumeUsage, error) {
	cmd, err := s.commandExec.ExecuteComposeCommand(stackName, "ps", "-a", "--format", "json")
	if err != nil {
		return nil, fmt.Errorf("failed to create compose command: %w", err)
	}

	output, err := cmd.Output()
	if err != nil {
		return nil, err
	}

	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	volumeUsage := make(map[string][]VolumeUsage)

	for _, line := range lines {
		if line == "" {
			continue
		}

		var container struct {
			Name    string `json:"Name"`
			Service string `json:"Service"`
			ID      string `json:"ID"`
		}

		if err := json.Unmarshal([]byte(line), &container); err != nil {
			continue
		}

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		containerInfo, err := s.dockerClient.ContainerInspect(ctx, container.ID)
		cancel()

		if err != nil {
			continue
		}

		for _, mount := range containerInfo.Mounts {
			volumeMount := VolumeMount{
				Type:     string(mount.Type),
				Source:   mount.Source,
				Target:   mount.Destination,
				ReadOnly: !mount.RW,
			}

			var volumeKey string
			if mount.Type == "volume" && mount.Name != "" {

				volumeKey = mount.Name
			} else if mount.Type == "bind" {

				volumeKey = "bind:" + mount.Source
			} else if mount.Type == "tmpfs" {

				volumeKey = "tmpfs:" + mount.Destination
			}

			if volumeKey != "" {
				usage := VolumeUsage{
					ContainerName: container.Name,
					ServiceName:   container.Service,
					Mounts:        []VolumeMount{volumeMount},
				}
				volumeUsage[volumeKey] = append(volumeUsage[volumeKey], usage)
			}
		}
	}

	return volumeUsage, nil
}

func (s *Service) mergeVolumeInformation(stackName string, composeVolumes map[string]Volume, dockerVolumes []*volume.Volume, containerInfo map[string][]VolumeUsage) []Volume {
	volumeMap := make(map[string]Volume)

	maps.Copy(volumeMap, composeVolumes)

	stackPrefix := stackName + "_"

	for _, dockerVol := range dockerVolumes {
		var vol Volume
		var volumeKey string

		if after, ok := strings.CutPrefix(dockerVol.Name, stackPrefix); ok {
			volumeKey = after
		} else if strings.Contains(dockerVol.Name, stackName) {
			volumeKey = dockerVol.Name
		} else {

			if _, exists := containerInfo[dockerVol.Name]; exists {
				volumeKey = dockerVol.Name
			} else {
				continue
			}
		}

		if existing, exists := volumeMap[volumeKey]; exists {
			vol = existing
		} else {
			vol = Volume{
				Name: volumeKey,
			}
		}

		vol.Exists = true
		vol.Driver = dockerVol.Driver
		vol.Mountpoint = dockerVol.Mountpoint
		vol.Scope = dockerVol.Scope
		vol.Created = dockerVol.CreatedAt

		if dockerVol.Labels != nil {
			if vol.Labels == nil {
				vol.Labels = make(map[string]string)
			}
			maps.Copy(vol.Labels, dockerVol.Labels)
		}

		if dockerVol.Options != nil {
			if vol.DriverOpts == nil {
				vol.DriverOpts = make(map[string]string)
			}
			maps.Copy(vol.DriverOpts, dockerVol.Options)
		}

		volumeMap[volumeKey] = vol
	}

	for volumeKey, usage := range containerInfo {
		if vol, exists := volumeMap[volumeKey]; exists {
			vol.UsedBy = usage

			if strings.HasPrefix(volumeKey, "bind:") || strings.HasPrefix(volumeKey, "tmpfs:") {
				vol.Exists = true
			}

			volumeMap[volumeKey] = vol
		} else {

			newVol := Volume{
				Name:   volumeKey,
				Exists: true,
				UsedBy: usage,
			}

			if strings.HasPrefix(volumeKey, "bind:") {
				newVol.Driver = "bind"
			} else if strings.HasPrefix(volumeKey, "tmpfs:") {
				newVol.Driver = "tmpfs"
			} else {
				newVol.Driver = "local"
			}

			volumeMap[volumeKey] = newVol
		}
	}

	var result []Volume
	for _, volume := range volumeMap {
		result = append(result, volume)
	}

	sort.Slice(result, func(i, j int) bool {
		return result[i].Name < result[j].Name
	})

	return result
}

func (s *Service) GetStackEnvironmentVariables(name string) (map[string][]ServiceEnvironment, error) {
	stackPath, err := validation.SanitizeStackPath(s.stackLocation, name)
	if err != nil {
		return nil, fmt.Errorf("invalid stack name '%s': %w", name, err)
	}

	if _, err := os.Stat(stackPath); os.IsNotExist(err) {
		return nil, fmt.Errorf("stack '%s' not found", name)
	}

	composeEnvironment, err := s.getComposeEnvironment(stackPath)
	if err != nil {
		return nil, fmt.Errorf("failed to get compose environment: %w", err)
	}

	runtimeEnvironment, err := s.getRuntimeEnvironment(stackPath)
	if err != nil {
		return nil, fmt.Errorf("failed to get runtime environment: %w", err)
	}

	return s.mergeEnvironmentInformation(composeEnvironment, runtimeEnvironment), nil
}

func (s *Service) getComposeEnvironment(stackPath string) (map[string][]ServiceEnvironment, error) {
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
		return make(map[string][]ServiceEnvironment), nil
	}

	return s.parseComposeEnvironment(stackPath, composeFile)
}

func (s *Service) parseComposeEnvironment(stackPath, composeFile string) (map[string][]ServiceEnvironment, error) {
	stackName := filepath.Base(stackPath)

	cmd, err := s.commandExec.ExecuteComposeWithFile(stackName, composeFile, "config", "--format", "json")
	if err != nil {
		return nil, fmt.Errorf("failed to create compose command: %w", err)
	}

	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to execute compose config: %w", err)
	}

	var composeData struct {
		Services map[string]any `json:"services"`
	}

	if err := json.Unmarshal(output, &composeData); err != nil {
		return nil, fmt.Errorf("failed to unmarshal compose config: %w", err)
	}

	services := make(map[string][]ServiceEnvironment)

	for serviceName, serviceConfig := range composeData.Services {
		if serviceMap, ok := serviceConfig.(map[string]any); ok {
			var envVars []ServiceEnvironment

			if envConfig, exists := serviceMap["environment"]; exists {
				switch env := envConfig.(type) {
				case []any:
					for _, envVar := range env {
						if envStr, ok := envVar.(string); ok {
							key, value := s.parseEnvString(envStr)
							envVars = append(envVars, ServiceEnvironment{
								Variables: []EnvironmentVariable{{
									Key:             key,
									Value:           value,
									IsSensitive:     s.isSensitiveVariable(key),
									Source:          "compose",
									IsFromContainer: false,
								}},
							})
						}
					}
				case map[string]any:
					for key, val := range env {
						value := ""
						if val != nil {
							value = fmt.Sprintf("%v", val)
						}
						envVars = append(envVars, ServiceEnvironment{
							Variables: []EnvironmentVariable{{
								Key:             key,
								Value:           value,
								IsSensitive:     s.isSensitiveVariable(key),
								Source:          "compose",
								IsFromContainer: false,
							}},
						})
					}
				}
			}

			if len(envVars) > 0 {
				services[serviceName] = envVars
			}
		}
	}

	return services, nil
}

func (s *Service) getRuntimeEnvironment(stackPath string) (map[string][]ServiceEnvironment, error) {
	stackName := filepath.Base(stackPath)
	containers, err := s.GetContainerInfo(stackName)
	if err != nil {
		return nil, fmt.Errorf("failed to get container info: %w", err)
	}

	services := make(map[string][]ServiceEnvironment)

	for serviceName, containerList := range containers {
		var envVars []ServiceEnvironment

		for _, container := range containerList {
			ctx := context.Background()
			containerJSON, err := s.dockerClient.ContainerInspect(ctx, container.Name)
			if err != nil {
				continue
			}

			var containerEnvVars []EnvironmentVariable
			for _, envStr := range containerJSON.Config.Env {
				key, value := s.parseEnvString(envStr)
				containerEnvVars = append(containerEnvVars, EnvironmentVariable{
					Key:             key,
					Value:           value,
					IsSensitive:     s.isSensitiveVariable(key),
					Source:          "runtime",
					IsFromContainer: true,
				})
			}

			if len(containerEnvVars) > 0 {
				envVars = append(envVars, ServiceEnvironment{
					Variables: containerEnvVars,
				})
			}
		}

		if len(envVars) > 0 {
			services[serviceName] = envVars
		}
	}

	return services, nil
}

func (s *Service) mergeEnvironmentInformation(composeEnv, runtimeEnv map[string][]ServiceEnvironment) map[string][]ServiceEnvironment {
	result := make(map[string][]ServiceEnvironment)

	allServices := make(map[string]bool)
	for serviceName := range composeEnv {
		allServices[serviceName] = true
	}
	for serviceName := range runtimeEnv {
		allServices[serviceName] = true
	}

	for serviceName := range allServices {
		var mergedEnvVars []EnvironmentVariable
		envVarMap := make(map[string]EnvironmentVariable)

		if composeVars, exists := composeEnv[serviceName]; exists {
			for _, serviceEnv := range composeVars {
				for _, envVar := range serviceEnv.Variables {
					envVarMap[envVar.Key] = envVar
				}
			}
		}

		if runtimeVars, exists := runtimeEnv[serviceName]; exists {
			for _, serviceEnv := range runtimeVars {
				for _, envVar := range serviceEnv.Variables {
					if existing, exists := envVarMap[envVar.Key]; exists {
						if existing.Source == "compose" && envVar.Source == "runtime" {
							existing.IsFromContainer = true
						}
						envVarMap[envVar.Key] = existing
					} else {
						envVarMap[envVar.Key] = envVar
					}
				}
			}
		}

		for _, envVar := range envVarMap {
			if envVar.IsSensitive && envVar.Value != "" {
				envVar.Value = "***"
			}
			mergedEnvVars = append(mergedEnvVars, envVar)
		}

		sort.Slice(mergedEnvVars, func(i, j int) bool {
			return mergedEnvVars[i].Key < mergedEnvVars[j].Key
		})

		if len(mergedEnvVars) > 0 {
			result[serviceName] = []ServiceEnvironment{{
				Variables: mergedEnvVars,
			}}
		}
	}

	return result
}

func (s *Service) parseEnvString(envStr string) (key, value string) {
	parts := strings.SplitN(envStr, "=", 2)
	key = parts[0]
	if len(parts) == 2 {
		value = parts[1]
	}
	return key, value
}

func (s *Service) isSensitiveVariable(key string) bool {
	sensitiveKeywords := []string{
		"PASSWORD", "PASS", "SECRET", "TOKEN", "KEY", "API_KEY",
		"AUTH", "CREDENTIAL", "PRIVATE", "CERT", "SSL", "TLS",
		"JWT", "OAUTH", "BEARER", "SESSION", "COOKIE",
	}

	upperKey := strings.ToUpper(key)
	for _, keyword := range sensitiveKeywords {
		if strings.Contains(upperKey, keyword) {
			return true
		}
	}
	return false
}

func matchesAnyPattern(stackName string, patterns []string) bool {
	if len(patterns) == 0 {
		return true
	}

	for _, pattern := range patterns {
		if matchesPattern(stackName, pattern) {
			return true
		}
	}
	return false
}

func matchesPattern(text, pattern string) bool {
	if pattern == "*" {
		return true
	}

	text = strings.ToLower(text)
	pattern = strings.ToLower(pattern)

	return matchesComplexPattern(text, pattern)
}

func matchesComplexPattern(text, pattern string) bool {
	if !strings.Contains(pattern, "*") {
		return text == pattern
	}

	parts := strings.Split(pattern, "*")

	if len(parts) == 1 {
		return text == pattern
	}

	textPos := 0

	for i, part := range parts {
		if part == "" {
			continue
		}

		if i == 0 {
			if !strings.HasPrefix(text, part) {
				return false
			}
			textPos = len(part)
		} else if i == len(parts)-1 {
			if !strings.HasSuffix(text, part) {
				return false
			}
			if len(text) < textPos+len(part) {
				return false
			}
		} else {
			index := strings.Index(text[textPos:], part)
			if index == -1 {
				return false
			}
			textPos += index + len(part)
		}
	}

	return true
}

func (s *Service) GetStacksSummary(patterns []string) (*StackSummary, error) {
	entries, err := os.ReadDir(s.stackLocation)
	if err != nil {
		return nil, err
	}

	summary := &StackSummary{
		TotalStacks:     0,
		HealthyStacks:   0,
		UnhealthyStacks: 0,
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		stackName := entry.Name()
		if !matchesAnyPattern(stackName, patterns) {
			continue
		}

		stackPath := filepath.Join(s.stackLocation, stackName)

		composeFiles := []string{
			"docker-compose.yml",
			"docker-compose.yaml",
			"compose.yml",
			"compose.yaml",
		}

		var hasComposeFile bool
		for _, filename := range composeFiles {
			composePath := filepath.Join(stackPath, filename)
			if _, err := os.Stat(composePath); err == nil {
				hasComposeFile = true
				break
			}
		}

		if !hasComposeFile {
			continue
		}

		summary.TotalStacks++

		isHealthy := s.isStackHealthy(stackName)
		if isHealthy {
			summary.HealthyStacks++
		} else {
			summary.UnhealthyStacks++
		}
	}

	return summary, nil
}

func (s *Service) isStackHealthy(stackName string) bool {

	expectedCount, exists := s.serviceCache.GetServiceCount(stackName)
	if !exists || expectedCount == 0 {
		return false
	}

	containers, err := s.getContainerInfoViaAPI(stackName)
	if err != nil {
		return false
	}

	runningServices := 0
	for _, containerList := range containers {
		hasRunningContainer := false
		for _, container := range containerList {
			if container.State == "running" {
				hasRunningContainer = true
				break
			}
		}
		if hasRunningContainer {
			runningServices++
		}
	}

	return runningServices == expectedCount
}
