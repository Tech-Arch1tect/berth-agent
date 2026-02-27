package stack

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/tech-arch1tect/berth-agent/config"
	"github.com/tech-arch1tect/berth-agent/internal/docker"
	"github.com/tech-arch1tect/berth-agent/internal/logging"
	"github.com/tech-arch1tect/berth-agent/internal/validation"

	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/volume"
	"go.uber.org/zap"
)

type Stack struct {
	Name              string              `json:"name"`
	Path              string              `json:"path"`
	ComposeFile       string              `json:"compose_file"`
	IsHealthy         bool                `json:"is_healthy"`
	TotalContainers   int                 `json:"total_containers"`
	RunningContainers int                 `json:"running_containers"`
	HealthDetails     *StackHealthDetails `json:"health_details,omitempty"`
}

type StackHealthDetails struct {
	Percentage     int      `json:"percentage"`
	HealthyCount   int      `json:"healthy_count"`
	UnhealthyCount int      `json:"unhealthy_count"`
	StoppedCount   int      `json:"stopped_count"`
	Reasons        []string `json:"reasons"`
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
	Ports      []string    `json:"ports,omitempty"`
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
	ExitCode       *int               `json:"exit_code,omitempty"`
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

type StackStatistics struct {
	TotalStacks     int `json:"total_stacks"`
	HealthyStacks   int `json:"healthy_stacks"`
	UnhealthyStacks int `json:"unhealthy_stacks"`
}

type ContainerImageDetails struct {
	ContainerName string              `json:"container_name"`
	ImageID       string              `json:"image_id"`
	ImageName     string              `json:"image_name"`
	ImageInfo     ImageInspectInfo    `json:"image_info"`
	ImageHistory  []ImageHistoryLayer `json:"image_history"`
}

type ImageInspectInfo struct {
	Architecture  string      `json:"architecture"`
	OS            string      `json:"os"`
	Size          int64       `json:"size"`
	VirtualSize   int64       `json:"virtual_size"`
	Author        string      `json:"author"`
	Created       string      `json:"created"`
	DockerVersion string      `json:"docker_version"`
	Config        ImageConfig `json:"config"`
	RootFS        RootFS      `json:"rootfs"`
	Parent        string      `json:"parent,omitempty"`
	RepoTags      []string    `json:"repo_tags,omitempty"`
	RepoDigests   []string    `json:"repo_digests,omitempty"`
}

type ImageConfig struct {
	User         string              `json:"user,omitempty"`
	Env          []string            `json:"env,omitempty"`
	Cmd          []string            `json:"cmd,omitempty"`
	Entrypoint   []string            `json:"entrypoint,omitempty"`
	WorkingDir   string              `json:"working_dir,omitempty"`
	ExposedPorts map[string]struct{} `json:"exposed_ports,omitempty"`
	Labels       map[string]string   `json:"labels,omitempty"`
}

type RootFS struct {
	Type   string   `json:"type"`
	Layers []string `json:"layers,omitempty"`
}

type ImageHistoryLayer struct {
	ID        string   `json:"id"`
	Created   int64    `json:"created"`
	CreatedBy string   `json:"created_by"`
	Size      int64    `json:"size"`
	Comment   string   `json:"comment,omitempty"`
	Tags      []string `json:"tags,omitempty"`
}

type Service struct {
	stackLocation string
	commandExec   *docker.CommandExecutor
	dockerClient  *docker.Client
	serviceCache  *ServiceCountCache
	logger        *logging.Logger
}

func NewService(cfg *config.Config, dockerClient *docker.Client, logger *logging.Logger) *Service {
	cache := NewServiceCountCache(cfg.StackLocation)

	service := &Service{
		stackLocation: cfg.StackLocation,
		commandExec:   docker.NewCommandExecutor(cfg.StackLocation),
		dockerClient:  dockerClient,
		serviceCache:  cache,
		logger:        logger.With(zap.String("component", "stack")),
	}

	if err := cache.Start(); err != nil {
		logger.Warn("Failed to start service count cache", zap.Error(err))
	}

	return service
}

func (s *Service) ListStacks() ([]Stack, error) {
	s.logger.Info("Listing stacks", zap.String("location", s.stackLocation))

	var stacks []Stack

	entries, err := os.ReadDir(s.stackLocation)
	if err != nil {
		s.logger.Error("Failed to read stack location", zap.String("location", s.stackLocation), zap.Error(err))
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
				stackName := entry.Name()
				isHealthy := s.isStackHealthy(stackName)

				totalContainers, runningContainers := s.getStackContainerCounts(stackName)
				healthDetails := s.getStackHealthDetails(stackName)

				s.logger.Debug("Discovered stack",
					zap.String("stack", stackName),
					zap.String("compose_file", filename),
					zap.Bool("is_healthy", isHealthy),
					zap.Int("total_containers", totalContainers),
					zap.Int("running_containers", runningContainers))

				stack := Stack{
					Name:              stackName,
					Path:              stackPath,
					ComposeFile:       filename,
					IsHealthy:         isHealthy,
					TotalContainers:   totalContainers,
					RunningContainers: runningContainers,
					HealthDetails:     healthDetails,
				}
				stacks = append(stacks, stack)
				break
			}
		}
	}

	sort.Slice(stacks, func(i, j int) bool {
		return stacks[i].Name < stacks[j].Name
	})

	s.logger.Info("Stack listing completed", zap.Int("count", len(stacks)))

	return stacks, nil
}

func (s *Service) CreateStack(name string) (*Stack, error) {
	s.logger.Info("Creating new stack", zap.String("name", name))

	if err := validation.ValidateStackName(name); err != nil {
		s.logger.Error("Invalid stack name", zap.String("name", name), zap.Error(err))
		return nil, fmt.Errorf("invalid stack name: %w", err)
	}

	stackPath := filepath.Join(s.stackLocation, name)
	if _, err := os.Stat(stackPath); err == nil {
		s.logger.Warn("Stack already exists", zap.String("name", name), zap.String("path", stackPath))
		return nil, fmt.Errorf("stack '%s' already exists", name)
	}

	if err := os.MkdirAll(stackPath, 0755); err != nil {
		s.logger.Error("Failed to create stack directory", zap.String("path", stackPath), zap.Error(err))
		return nil, fmt.Errorf("failed to create directory: %w", err)
	}

	template := `services:
  hello-world:
    image: hello-world
`
	composePath := filepath.Join(stackPath, "docker-compose.yml")
	if err := os.WriteFile(composePath, []byte(template), 0644); err != nil {
		os.RemoveAll(stackPath)
		s.logger.Error("Failed to write compose file", zap.String("path", composePath), zap.Error(err))
		return nil, fmt.Errorf("failed to write compose file: %w", err)
	}

	s.logger.Info("Stack created successfully", zap.String("name", name), zap.String("path", stackPath))

	return &Stack{
		Name:        name,
		Path:        stackPath,
		ComposeFile: "docker-compose.yml",
		IsHealthy:   false,
	}, nil
}

func (s *Service) GetStackDetails(name string) (*StackDetails, error) {
	s.logger.Info("Retrieving stack details", zap.String("stack", name))

	stackPath, err := validation.SanitizeStackPath(s.stackLocation, name)
	if err != nil {
		s.logger.Error("Invalid stack name", zap.String("stack", name), zap.Error(err))
		return nil, fmt.Errorf("invalid stack name '%s': %w", name, err)
	}

	if _, err := os.Stat(stackPath); os.IsNotExist(err) {
		s.logger.Error("Stack not found", zap.String("stack", name), zap.String("path", stackPath))
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
		s.logger.Error("No compose file found in stack", zap.String("stack", name), zap.String("path", stackPath))
		return nil, fmt.Errorf("no compose file found in stack '%s'", name)
	}

	s.logger.Debug("Found compose file", zap.String("stack", name), zap.String("compose_file", composeFile))

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
			if services[i].Image == "" {
				services[i].Image = "built-from-dockerfile"
			}
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

	s.logger.Info("Stack details retrieved successfully",
		zap.String("stack", name),
		zap.Int("service_count", len(services)))

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

	cmd, err := s.commandExec.ExecuteComposeCommand(stackName, "config", "--format", "json")
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

				if rawPorts, ok := serviceConfigMap["ports"]; ok {
					switch typed := rawPorts.(type) {
					case []any:
						for _, entry := range typed {
							if mapping, ok := parseComposePortEntry(entry); ok {
								service.Ports = append(service.Ports, mapping)
							}
						}
					case []string:
						for _, entry := range typed {
							if trimmed := strings.TrimSpace(entry); trimmed != "" {
								service.Ports = append(service.Ports, trimmed)
							}
						}
					case map[string]any:
						if mapping, ok := composePortMapToString(typed); ok {
							service.Ports = append(service.Ports, mapping)
						}
					}
				}
			}

			services = append(services, service)
		}
	}

	return services, nil
}

func parseComposePortEntry(entry any) (string, bool) {
	switch value := entry.(type) {
	case string:
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			return "", false
		}
		return trimmed, true
	case map[string]any:
		return composePortMapToString(value)
	default:
		return "", false
	}
}

func composePortMapToString(entry map[string]any) (string, bool) {
	target, ok := extractComposePortNumber(entry, "target")
	if !ok {
		target, ok = extractComposePortNumber(entry, "target_port")
	}
	if !ok || target <= 0 {
		return "", false
	}

	published, _ := extractComposePortNumber(entry, "published")
	if published == 0 {
		published, _ = extractComposePortNumber(entry, "published_port")
	}

	hostIP := ""
	if rawHostIP, ok := entry["host_ip"].(string); ok {
		hostIP = strings.TrimSpace(rawHostIP)
	}

	protocol := "tcp"
	if rawProtocol, ok := entry["protocol"].(string); ok {
		if trimmed := strings.TrimSpace(strings.ToLower(rawProtocol)); trimmed != "" {
			protocol = trimmed
		}
	}

	base := ""
	if published > 0 {
		if hostIP != "" {
			base = fmt.Sprintf("%s:%d:%d", hostIP, published, target)
		} else {
			base = fmt.Sprintf("%d:%d", published, target)
		}
	} else {
		base = fmt.Sprintf("%d", target)
	}

	if protocol != "" && protocol != "tcp" {
		base = fmt.Sprintf("%s/%s", base, protocol)
	}

	return base, true
}

func extractComposePortNumber(entry map[string]any, key string) (int, bool) {
	value, ok := entry[key]
	if !ok {
		return 0, false
	}
	switch v := value.(type) {
	case int:
		return v, true
	case int64:
		return int(v), true
	case float64:
		return int(v), true
	case float32:
		return int(v), true
	case json.Number:
		i, err := v.Int64()
		if err != nil {
			return 0, false
		}
		return int(i), true
	case string:
		trimmed := strings.TrimSpace(v)
		if trimmed == "" {
			return 0, false
		}
		if parsed, err := strconv.Atoi(trimmed); err == nil {
			return parsed, true
		}
	}
	return 0, false
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
			exitCode := containerDetails.State.ExitCode
			container.ExitCode = &exitCode

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
	s.logger.Info("Retrieving stack networks", zap.String("stack", name))

	stackPath, err := validation.SanitizeStackPath(s.stackLocation, name)
	if err != nil {
		s.logger.Error("Invalid stack name for network retrieval", zap.String("stack", name), zap.Error(err))
		return nil, fmt.Errorf("invalid stack name '%s': %w", name, err)
	}

	if _, err := os.Stat(stackPath); os.IsNotExist(err) {
		s.logger.Error("Stack not found for network retrieval", zap.String("stack", name), zap.String("path", stackPath))
		return nil, fmt.Errorf("stack '%s' not found", name)
	}

	composeNetworks, err := s.getComposeNetworks(stackPath)
	if err != nil {
		s.logger.Error("Failed to get compose networks", zap.String("stack", name), zap.Error(err))
		return nil, fmt.Errorf("failed to get compose networks: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	dockerNetworkSummaries, err := s.dockerClient.ListNetworks(ctx)
	if err != nil {
		s.logger.Error("Failed to list Docker networks", zap.String("stack", name), zap.Error(err))
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

	networks := s.mergeNetworkInformation(name, composeNetworks, dockerNetworks)
	s.logger.Info("Stack networks retrieved successfully",
		zap.String("stack", name),
		zap.Int("network_count", len(networks)))

	return networks, nil
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

	cmd, err := s.commandExec.ExecuteComposeCommand(stackName, "config", "--format", "json")
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
	s.logger.Info("Retrieving stack volumes", zap.String("stack", name))

	stackPath, err := validation.SanitizeStackPath(s.stackLocation, name)
	if err != nil {
		s.logger.Error("Invalid stack name for volume retrieval", zap.String("stack", name), zap.Error(err))
		return nil, fmt.Errorf("invalid stack name '%s': %w", name, err)
	}

	if _, err := os.Stat(stackPath); os.IsNotExist(err) {
		s.logger.Error("Stack not found for volume retrieval", zap.String("stack", name), zap.String("path", stackPath))
		return nil, fmt.Errorf("stack '%s' not found", name)
	}

	composeVolumes, err := s.getComposeVolumes(stackPath)
	if err != nil {
		s.logger.Error("Failed to get compose volumes", zap.String("stack", name), zap.Error(err))
		return nil, fmt.Errorf("failed to get compose volumes: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	dockerVolumeList, err := s.dockerClient.ListVolumes(ctx)
	if err != nil {
		s.logger.Error("Failed to list Docker volumes", zap.String("stack", name), zap.Error(err))
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
		s.logger.Error("Failed to get container volume info", zap.String("stack", name), zap.Error(err))
		return nil, fmt.Errorf("failed to get container volume info: %w", err)
	}

	volumes := s.mergeVolumeInformation(name, composeVolumes, dockerVolumes, containerInfo)
	s.logger.Info("Stack volumes retrieved successfully",
		zap.String("stack", name),
		zap.Int("volume_count", len(volumes)))

	return volumes, nil
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

	cmd, err := s.commandExec.ExecuteComposeCommand(stackName, "config", "--format", "json")
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

func (s *Service) GetStackEnvironmentVariables(name string, unmask bool) (map[string][]ServiceEnvironment, error) {
	s.logger.Info("Retrieving stack environment variables",
		zap.String("stack", name),
		zap.Bool("unmask", unmask))

	stackPath, err := validation.SanitizeStackPath(s.stackLocation, name)
	if err != nil {
		s.logger.Error("Invalid stack name for environment retrieval", zap.String("stack", name), zap.Error(err))
		return nil, fmt.Errorf("invalid stack name '%s': %w", name, err)
	}

	if _, err := os.Stat(stackPath); os.IsNotExist(err) {
		s.logger.Error("Stack not found for environment retrieval", zap.String("stack", name), zap.String("path", stackPath))
		return nil, fmt.Errorf("stack '%s' not found", name)
	}

	composeEnvironment, err := s.getComposeEnvironment(stackPath)
	if err != nil {
		s.logger.Error("Failed to get compose environment", zap.String("stack", name), zap.Error(err))
		return nil, fmt.Errorf("failed to get compose environment: %w", err)
	}

	runtimeEnvironment, err := s.getRuntimeEnvironment(stackPath)
	if err != nil {
		s.logger.Error("Failed to get runtime environment", zap.String("stack", name), zap.Error(err))
		return nil, fmt.Errorf("failed to get runtime environment: %w", err)
	}

	envVars := s.mergeEnvironmentInformation(composeEnvironment, runtimeEnvironment, unmask)
	s.logger.Info("Stack environment variables retrieved successfully",
		zap.String("stack", name),
		zap.Int("service_count", len(envVars)),
		zap.Bool("unmask", unmask))

	return envVars, nil
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

	cmd, err := s.commandExec.ExecuteComposeCommand(stackName, "config", "--format", "json")
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

func (s *Service) mergeEnvironmentInformation(composeEnv, runtimeEnv map[string][]ServiceEnvironment, unmask bool) map[string][]ServiceEnvironment {
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
			if !unmask && envVar.IsSensitive && envVar.Value != "" {
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

var sensitiveKeywords = []string{
	"PASSWORD", "PASS", "SECRET", "TOKEN", "KEY", "API_KEY",
	"AUTH", "CREDENTIAL", "PRIVATE", "CERT", "SSL", "TLS",
	"JWT", "OAUTH", "BEARER", "SESSION", "COOKIE",
}

func (s *Service) isSensitiveVariable(key string) bool {
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

func (s *Service) GetStacksSummary(patterns []string) (*StackStatistics, error) {
	entries, err := os.ReadDir(s.stackLocation)
	if err != nil {
		return nil, err
	}

	summary := &StackStatistics{
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
		s.logger.Debug("Cache miss for stack service count", zap.String("stack", stackName))
		return false
	}

	s.logger.Debug("Cache hit for stack service count",
		zap.String("stack", stackName),
		zap.Int("expected_count", expectedCount))

	containers, err := s.getContainerInfoViaAPI(stackName)
	if err != nil {
		s.logger.Debug("Failed to get container info for health check",
			zap.String("stack", stackName),
			zap.Error(err))
		return false
	}

	servicesWithHealthChecks := 0
	healthyServices := 0
	runningServices := 0

	for _, containerList := range containers {
		hasRunningContainer := false
		hasHealthCheck := false
		serviceHealthy := false

		for _, container := range containerList {
			if container.State == "running" {
				hasRunningContainer = true

				if container.Health != nil {
					hasHealthCheck = true

					if container.Health.Status == "unhealthy" {
						serviceHealthy = false
						s.logger.Debug("Container is unhealthy",
							zap.String("container", container.Name),
							zap.String("health_status", container.Health.Status))
						break
					}

					if container.Health.Status == "starting" {
						serviceHealthy = false
						s.logger.Debug("Container health check starting",
							zap.String("container", container.Name))
					}

					if container.Health.Status == "healthy" {
						serviceHealthy = true
					}
				} else {
					serviceHealthy = true
				}
			}
		}

		if hasRunningContainer {
			runningServices++
			if hasHealthCheck {
				servicesWithHealthChecks++
				if serviceHealthy {
					healthyServices++
				}
			}
		}
	}

	if servicesWithHealthChecks == 0 {
		isHealthy := runningServices == expectedCount
		s.logger.Debug("Stack health check (no health checks)",
			zap.String("stack", stackName), zap.Bool("is_healthy", isHealthy),
			zap.Int("running_services", runningServices), zap.Int("expected_services", expectedCount))
		return isHealthy
	}

	isHealthy := healthyServices == servicesWithHealthChecks && runningServices == expectedCount

	s.logger.Debug("Stack health check (with health checks)",
		zap.String("stack", stackName), zap.Bool("is_healthy", isHealthy),
		zap.Int("running_services", runningServices), zap.Int("expected_services", expectedCount),
		zap.Int("services_with_health_checks", servicesWithHealthChecks),
		zap.Int("healthy_services", healthyServices))

	return isHealthy
}

func (s *Service) getStackContainerCounts(stackName string) (total int, running int) {

	expectedCount, exists := s.serviceCache.GetServiceCount(stackName)
	if !exists {
		s.logger.Debug("Cache miss for stack container counts", zap.String("stack", stackName))
		return 0, 0
	}

	s.logger.Debug("Cache hit for stack container counts",
		zap.String("stack", stackName),
		zap.Int("expected_count", expectedCount))

	containers, err := s.getContainerInfoViaAPI(stackName)
	if err != nil {
		s.logger.Debug("Failed to get container info for count",
			zap.String("stack", stackName),
			zap.Error(err))
		return expectedCount, 0
	}

	runningCount := 0
	for _, containerList := range containers {
		for _, container := range containerList {
			if container.State == "running" {
				runningCount++
			}
		}
	}

	s.logger.Debug("Container counts retrieved",
		zap.String("stack", stackName),
		zap.Int("total", expectedCount),
		zap.Int("running", runningCount))

	return expectedCount, runningCount
}

func (s *Service) getStackHealthDetails(stackName string) *StackHealthDetails {
	containers, err := s.getContainerInfoViaAPI(stackName)
	if err != nil {
		s.logger.Debug("Failed to get container info for health details",
			zap.String("stack", stackName), zap.Error(err))
		return nil
	}

	total := 0
	running := 0
	stopped := 0
	healthy := 0
	unhealthy := 0

	for _, containerList := range containers {
		for _, container := range containerList {
			total++

			if container.State == "running" {
				running++

				if container.Health == nil {
					healthy++
				} else {
					switch container.Health.Status {
					case "healthy":
						healthy++
					case "unhealthy":
						unhealthy++
					default:
					}
				}
			} else {
				stopped++
			}
		}
	}

	var percentage int
	if total > 0 {
		percentage = int(math.Round(float64(healthy) / float64(total) * 100))
	}

	reasons := []string{}
	if stopped > 0 {
		reasons = append(reasons, fmt.Sprintf("%d container(s) stopped", stopped))
	}
	if unhealthy > 0 {
		reasons = append(reasons, fmt.Sprintf("%d container(s) failing health check", unhealthy))
	}

	healthDetails := &StackHealthDetails{
		Percentage:     percentage,
		HealthyCount:   healthy,
		UnhealthyCount: unhealthy,
		StoppedCount:   stopped,
		Reasons:        reasons,
	}

	s.logger.Debug("Stack health details calculated",
		zap.String("stack", stackName),
		zap.Int("percentage", percentage),
		zap.Int("healthy", healthy),
		zap.Int("unhealthy", unhealthy),
		zap.Int("stopped", stopped),
		zap.Any("reasons", reasons))

	return healthDetails
}

func (s *Service) GetContainerImageDetails(stackName string) ([]ContainerImageDetails, error) {
	s.logger.Info("Retrieving container image details", zap.String("stack", stackName))

	ctx := context.Background()

	stackPath, err := validation.SanitizeStackPath(s.stackLocation, stackName)
	if err != nil {
		s.logger.Error("Invalid stack name for image details retrieval", zap.String("stack", stackName), zap.Error(err))
		return nil, fmt.Errorf("invalid stack name '%s': %w", stackName, err)
	}

	if _, err := os.Stat(stackPath); os.IsNotExist(err) {
		s.logger.Error("Stack not found for image details retrieval", zap.String("stack", stackName), zap.String("path", stackPath))
		return nil, fmt.Errorf("stack '%s' not found", stackName)
	}

	containers, err := s.getContainerInfoViaAPI(stackName)
	if err != nil {
		s.logger.Error("Failed to get container info for image details", zap.String("stack", stackName), zap.Error(err))
		return nil, fmt.Errorf("failed to get container info: %w", err)
	}

	var imageDetails []ContainerImageDetails

	for _, containerList := range containers {
		for _, container := range containerList {
			if container.Image == "" {
				continue
			}

			imageInfo, err := s.dockerClient.ImageInspect(ctx, container.Image)
			if err != nil {
				s.logger.Error("Failed to inspect image",
					zap.String("stack", stackName),
					zap.String("container", container.Name),
					zap.String("image", container.Image),
					zap.Error(err))
				continue
			}

			history, err := s.dockerClient.ImageHistory(ctx, container.Image)
			if err != nil {
				s.logger.Warn("Failed to get image history",
					zap.String("stack", stackName),
					zap.String("image", container.Image),
					zap.Error(err))
				history = nil
			}

			var historyLayers []ImageHistoryLayer
			for _, layer := range history {
				historyLayers = append(historyLayers, ImageHistoryLayer{
					ID:        layer.ID,
					Created:   layer.Created,
					CreatedBy: layer.CreatedBy,
					Size:      layer.Size,
					Comment:   layer.Comment,
					Tags:      layer.Tags,
				})
			}

			exposedPorts := make(map[string]struct{})
			if imageInfo.Config.ExposedPorts != nil {
				for port := range imageInfo.Config.ExposedPorts {
					exposedPorts[port] = struct{}{}
				}
			}

			imageDetails = append(imageDetails, ContainerImageDetails{
				ContainerName: container.Name,
				ImageID:       imageInfo.ID,
				ImageName:     container.Image,
				ImageInfo: ImageInspectInfo{
					Architecture:  imageInfo.Architecture,
					OS:            imageInfo.Os,
					Size:          imageInfo.Size,
					VirtualSize:   imageInfo.Size,
					Author:        imageInfo.Author,
					Created:       imageInfo.Created,
					DockerVersion: imageInfo.DockerVersion,
					Parent:        imageInfo.Parent,
					RepoTags:      imageInfo.RepoTags,
					RepoDigests:   imageInfo.RepoDigests,
					Config: ImageConfig{
						User:         imageInfo.Config.User,
						Env:          imageInfo.Config.Env,
						Cmd:          imageInfo.Config.Cmd,
						Entrypoint:   imageInfo.Config.Entrypoint,
						WorkingDir:   imageInfo.Config.WorkingDir,
						ExposedPorts: exposedPorts,
						Labels:       imageInfo.Config.Labels,
					},
					RootFS: RootFS{
						Type:   imageInfo.RootFS.Type,
						Layers: imageInfo.RootFS.Layers,
					},
				},
				ImageHistory: historyLayers,
			})
		}
	}

	s.logger.Info("Container image details retrieved successfully",
		zap.String("stack", stackName),
		zap.Int("image_count", len(imageDetails)))

	return imageDetails, nil
}
