package composeeditor

import (
	"berth-agent/config"
	"berth-agent/internal/logging"
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/compose-spec/compose-go/v2/cli"
	"github.com/compose-spec/compose-go/v2/types"
	"go.uber.org/zap"
	"gopkg.in/yaml.v3"
)

type Service struct {
	stackLocation string
	logger        *logging.Logger
}

func NewService(cfg *config.Config, logger *logging.Logger) *Service {
	return &Service{
		stackLocation: cfg.StackLocation,
		logger:        logger,
	}
}

func (s *Service) GetComposeConfig(ctx context.Context, stackName string) (*ComposeConfig, error) {
	stackPath := filepath.Join(s.stackLocation, stackName)

	if _, err := os.Stat(stackPath); os.IsNotExist(err) {
		return nil, fmt.Errorf("stack not found: %s", stackName)
	}

	composeFile, err := s.findComposeFile(stackPath)
	if err != nil {
		return nil, fmt.Errorf("failed to find compose file: %w", err)
	}

	s.logger.Debug("parsing compose file",
		zap.String("stack", stackName),
		zap.String("file", composeFile),
	)

	project, err := s.loadProject(stackPath, composeFile)
	if err != nil {
		return nil, fmt.Errorf("failed to parse compose file: %w", err)
	}

	return &ComposeConfig{
		ComposeFile: filepath.Base(composeFile),
		Services:    project.Services,
		Networks:    project.Networks,
		Volumes:     project.Volumes,
		Secrets:     project.Secrets,
		Configs:     project.Configs,
	}, nil
}

func (s *Service) findComposeFile(stackPath string) (string, error) {
	candidates := []string{
		"docker-compose.yml",
		"docker-compose.yaml",
		"compose.yml",
		"compose.yaml",
	}

	for _, candidate := range candidates {
		path := filepath.Join(stackPath, candidate)
		if _, err := os.Stat(path); err == nil {
			return path, nil
		}
	}

	return "", fmt.Errorf("no compose file found in %s", stackPath)
}

func (s *Service) loadProject(stackPath, composeFile string) (*types.Project, error) {
	options, err := cli.NewProjectOptions(
		[]string{composeFile},
		cli.WithWorkingDirectory(stackPath),
		cli.WithResolvedPaths(true),
		cli.WithDiscardEnvFile,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create project options: %w", err)
	}

	project, err := cli.ProjectFromOptions(context.Background(), options)
	if err != nil {
		return nil, fmt.Errorf("failed to load project: %w", err)
	}

	return project, nil
}

func (s *Service) UpdateCompose(ctx context.Context, stackName string, changes ComposeChanges) error {
	stackPath := filepath.Join(s.stackLocation, stackName)

	if _, err := os.Stat(stackPath); os.IsNotExist(err) {
		return fmt.Errorf("stack not found: %s", stackName)
	}

	composeFile, err := s.findComposeFile(stackPath)
	if err != nil {
		return fmt.Errorf("failed to find compose file: %w", err)
	}

	s.logger.Debug("updating compose file",
		zap.String("stack", stackName),
		zap.String("file", composeFile),
	)

	project, err := s.loadProject(stackPath, composeFile)
	if err != nil {
		return fmt.Errorf("failed to parse compose file: %w", err)
	}

	if err := s.applyChanges(project, changes); err != nil {
		return fmt.Errorf("failed to apply changes: %w", err)
	}

	if err := s.writeComposeFile(composeFile, project); err != nil {
		return fmt.Errorf("failed to write compose file: %w", err)
	}

	return nil
}

func (s *Service) applyChanges(project *types.Project, changes ComposeChanges) error {
	for serviceName, serviceChanges := range changes.ServiceChanges {
		svc, exists := project.Services[serviceName]
		if !exists {
			return fmt.Errorf("service not found: %s", serviceName)
		}

		if serviceChanges.Image != nil {
			svc.Image = *serviceChanges.Image
		}

		if serviceChanges.Ports != nil {
			svc.Ports = s.convertPorts(serviceChanges.Ports)
		}

		if serviceChanges.Environment != nil {
			if svc.Environment == nil {
				svc.Environment = make(types.MappingWithEquals)
			}
			for key, val := range serviceChanges.Environment {
				if val == nil {
					delete(svc.Environment, key)
				} else {
					svc.Environment[key] = val
				}
			}
		}

		if serviceChanges.Volumes != nil {
			svc.Volumes = s.convertVolumes(serviceChanges.Volumes)
		}

		if serviceChanges.Command != nil {
			svc.Command = serviceChanges.Command.Values
		}

		if serviceChanges.Entrypoint != nil {
			svc.Entrypoint = serviceChanges.Entrypoint.Values
		}

		if serviceChanges.Restart != nil {
			svc.Restart = *serviceChanges.Restart
		}

		if serviceChanges.Labels != nil {
			if svc.Labels == nil {
				svc.Labels = make(types.Labels)
			}
			for key, val := range serviceChanges.Labels {
				if val == nil {
					delete(svc.Labels, key)
				} else {
					svc.Labels[key] = *val
				}
			}
		}

		if serviceChanges.DependsOn != nil {
			svc.DependsOn = s.convertDependsOn(serviceChanges.DependsOn)
		}

		if serviceChanges.Healthcheck != nil {
			svc.HealthCheck = s.convertHealthcheck(serviceChanges.Healthcheck)
		}

		if serviceChanges.Deploy != nil {
			svc.Deploy = s.convertDeploy(serviceChanges.Deploy)
		}

		if serviceChanges.Build != nil {
			svc.Build = s.convertBuild(serviceChanges.Build)
		}

		project.Services[serviceName] = svc
	}

	return nil
}

func (s *Service) convertPorts(ports []PortMapping) []types.ServicePortConfig {
	result := make([]types.ServicePortConfig, len(ports))
	for i, p := range ports {
		result[i] = types.ServicePortConfig{
			Target:    p.Target,
			Published: p.Published,
			HostIP:    p.HostIP,
			Protocol:  p.Protocol,
		}
	}
	return result
}

func (s *Service) convertVolumes(volumes []VolumeMount) []types.ServiceVolumeConfig {
	result := make([]types.ServiceVolumeConfig, len(volumes))
	for i, v := range volumes {
		result[i] = types.ServiceVolumeConfig{
			Type:     v.Type,
			Source:   v.Source,
			Target:   v.Target,
			ReadOnly: v.ReadOnly,
		}
	}
	return result
}

func (s *Service) convertDependsOn(deps map[string]DependsOnConfig) types.DependsOnConfig {
	result := make(types.DependsOnConfig)
	for name, cfg := range deps {
		condition := types.ServiceConditionStarted
		if cfg.Condition != "" {
			condition = cfg.Condition
		}
		result[name] = types.ServiceDependency{
			Condition: condition,
			Restart:   cfg.Restart,
			Required:  cfg.Required,
		}
	}
	return result
}

func (s *Service) convertHealthcheck(hc *HealthcheckConfig) *types.HealthCheckConfig {
	if hc.Disable {
		return &types.HealthCheckConfig{
			Disable: true,
		}
	}

	result := &types.HealthCheckConfig{
		Test: hc.Test,
	}

	if hc.Interval != "" {
		if d, err := time.ParseDuration(hc.Interval); err == nil {
			dur := types.Duration(d)
			result.Interval = &dur
		}
	}
	if hc.Timeout != "" {
		if d, err := time.ParseDuration(hc.Timeout); err == nil {
			dur := types.Duration(d)
			result.Timeout = &dur
		}
	}
	if hc.StartPeriod != "" {
		if d, err := time.ParseDuration(hc.StartPeriod); err == nil {
			dur := types.Duration(d)
			result.StartPeriod = &dur
		}
	}
	if hc.StartInterval != "" {
		if d, err := time.ParseDuration(hc.StartInterval); err == nil {
			dur := types.Duration(d)
			result.StartInterval = &dur
		}
	}
	if hc.Retries != nil {
		result.Retries = hc.Retries
	}

	return result
}

func (s *Service) convertDeploy(deploy *DeployConfig) *types.DeployConfig {
	result := &types.DeployConfig{}

	if deploy.Mode != nil {
		result.Mode = *deploy.Mode
	}
	if deploy.Replicas != nil {
		result.Replicas = deploy.Replicas
	}

	if deploy.Resources != nil {
		result.Resources = types.Resources{}
		if deploy.Resources.Limits != nil {
			result.Resources.Limits = &types.Resource{
				NanoCPUs:    s.parseCPUs(deploy.Resources.Limits.CPUs),
				MemoryBytes: s.parseMemorySize(deploy.Resources.Limits.Memory),
			}
		}
		if deploy.Resources.Reservations != nil {
			result.Resources.Reservations = &types.Resource{
				NanoCPUs:    s.parseCPUs(deploy.Resources.Reservations.CPUs),
				MemoryBytes: s.parseMemorySize(deploy.Resources.Reservations.Memory),
			}
		}
	}

	if deploy.RestartPolicy != nil {
		result.RestartPolicy = &types.RestartPolicy{
			Condition: deploy.RestartPolicy.Condition,
		}
		if deploy.RestartPolicy.Delay != "" {
			if d, err := time.ParseDuration(deploy.RestartPolicy.Delay); err == nil {
				dur := types.Duration(d)
				result.RestartPolicy.Delay = &dur
			}
		}
		if deploy.RestartPolicy.MaxAttempts != nil {
			attempts := uint64(*deploy.RestartPolicy.MaxAttempts)
			result.RestartPolicy.MaxAttempts = &attempts
		}
		if deploy.RestartPolicy.Window != "" {
			if d, err := time.ParseDuration(deploy.RestartPolicy.Window); err == nil {
				dur := types.Duration(d)
				result.RestartPolicy.Window = &dur
			}
		}
	}

	if deploy.Placement != nil && len(deploy.Placement.Constraints) > 0 {
		result.Placement = types.Placement{
			Constraints: deploy.Placement.Constraints,
		}
	}

	return result
}

func (s *Service) parseCPUs(cpus string) types.NanoCPUs {
	if cpus == "" {
		return 0
	}
	val, err := strconv.ParseFloat(cpus, 32)
	if err != nil {
		return 0
	}
	return types.NanoCPUs(val)
}

func (s *Service) parseMemorySize(size string) types.UnitBytes {
	if size == "" {
		return 0
	}

	size = strings.ToLower(size)
	var multiplier int64 = 1
	var numStr string

	if strings.HasSuffix(size, "g") || strings.HasSuffix(size, "gb") {
		multiplier = 1024 * 1024 * 1024
		numStr = strings.TrimSuffix(strings.TrimSuffix(size, "gb"), "g")
	} else if strings.HasSuffix(size, "m") || strings.HasSuffix(size, "mb") {
		multiplier = 1024 * 1024
		numStr = strings.TrimSuffix(strings.TrimSuffix(size, "mb"), "m")
	} else if strings.HasSuffix(size, "k") || strings.HasSuffix(size, "kb") {
		multiplier = 1024
		numStr = strings.TrimSuffix(strings.TrimSuffix(size, "kb"), "k")
	} else if strings.HasSuffix(size, "b") {
		numStr = strings.TrimSuffix(size, "b")
	} else {
		numStr = size
	}

	num, err := strconv.ParseFloat(numStr, 64)
	if err != nil {
		return 0
	}
	return types.UnitBytes(int64(num) * multiplier)
}

func (s *Service) convertBuild(build *BuildConfig) *types.BuildConfig {
	result := &types.BuildConfig{
		Context:    build.Context,
		Dockerfile: build.Dockerfile,
		Target:     build.Target,
	}

	if len(build.Args) > 0 {
		result.Args = make(types.MappingWithEquals)
		for k, v := range build.Args {
			val := v
			result.Args[k] = &val
		}
	}

	if len(build.CacheFrom) > 0 {
		result.CacheFrom = build.CacheFrom
	}

	return result
}

func (s *Service) writeComposeFile(filePath string, project *types.Project) error {
	doc := &yaml.Node{Kind: yaml.MappingNode}

	addSection := func(key string, node *yaml.Node) {
		keyNode := &yaml.Node{Kind: yaml.ScalarNode, Value: key}
		doc.Content = append(doc.Content, keyNode, node)
	}

	if len(project.Services) > 0 {
		servicesNode := &yaml.Node{Kind: yaml.MappingNode}
		serviceNames := make([]string, 0, len(project.Services))
		for name := range project.Services {
			serviceNames = append(serviceNames, name)
		}
		sort.Strings(serviceNames)

		for _, name := range serviceNames {
			svc := project.Services[name]
			keyNode := &yaml.Node{Kind: yaml.ScalarNode, Value: name}
			servicesNode.Content = append(servicesNode.Content, keyNode, s.serviceToNode(svc))
		}
		addSection("services", servicesNode)
	}

	if len(project.Networks) > 0 {
		networks := s.cleanNetworks(project.Networks, project.Name)
		if len(networks) > 0 {
			var netNode yaml.Node
			netNode.Encode(networks)
			addSection("networks", &netNode)
		}
	}

	if len(project.Volumes) > 0 {
		volumes := s.cleanVolumes(project.Volumes)
		if len(volumes) > 0 {
			var volNode yaml.Node
			volNode.Encode(volumes)
			addSection("volumes", &volNode)
		}
	}

	if len(project.Secrets) > 0 {
		var secNode yaml.Node
		secNode.Encode(project.Secrets)
		addSection("secrets", &secNode)
	}

	if len(project.Configs) > 0 {
		var cfgNode yaml.Node
		cfgNode.Encode(project.Configs)
		addSection("configs", &cfgNode)
	}

	var buf bytes.Buffer
	encoder := yaml.NewEncoder(&buf)
	encoder.SetIndent(2)
	if err := encoder.Encode(doc); err != nil {
		return fmt.Errorf("failed to marshal compose: %w", err)
	}
	encoder.Close()

	if err := os.WriteFile(filePath, buf.Bytes(), 0644); err != nil {
		return fmt.Errorf("failed to write file: %w", err)
	}

	return nil
}

func (s *Service) cleanNetworks(networks types.Networks, projectName string) map[string]any {
	result := map[string]any{}
	defaultNetworkName := projectName + "_default"

	for name, network := range networks {
		if name == "default" && network.Name == defaultNetworkName {
			continue
		}
		netConfig := map[string]any{}
		if network.Driver != "" && network.Driver != "bridge" {
			netConfig["driver"] = network.Driver
		}
		if len(network.DriverOpts) > 0 {
			netConfig["driver_opts"] = network.DriverOpts
		}
		if network.External {
			netConfig["external"] = true
		}
		if network.Ipam.Driver != "" || len(network.Ipam.Config) > 0 {
			netConfig["ipam"] = network.Ipam
		}
		if len(network.Labels) > 0 {
			netConfig["labels"] = network.Labels
		}
		if len(netConfig) == 0 {
			result[name] = nil
		} else {
			result[name] = netConfig
		}
	}
	return result
}

func (s *Service) cleanVolumes(volumes types.Volumes) map[string]any {
	result := map[string]any{}

	for name, volume := range volumes {
		volConfig := map[string]any{}
		if volume.Driver != "" && volume.Driver != "local" {
			volConfig["driver"] = volume.Driver
		}
		if len(volume.DriverOpts) > 0 {
			volConfig["driver_opts"] = volume.DriverOpts
		}
		if volume.External {
			volConfig["external"] = true
		}
		if len(volume.Labels) > 0 {
			volConfig["labels"] = volume.Labels
		}
		if len(volConfig) == 0 {
			result[name] = nil
		} else {
			result[name] = volConfig
		}
	}
	return result
}

func (s *Service) shellJoin(args []string) string {
	if len(args) == 0 {
		return ""
	}
	if len(args) == 1 {
		return args[0]
	}
	var result []string
	for _, arg := range args {
		if strings.ContainsAny(arg, " \t\n\"'\\") {
			result = append(result, fmt.Sprintf("%q", arg))
		} else {
			result = append(result, arg)
		}
	}
	return strings.Join(result, " ")
}

func (s *Service) toFlowStyleArray(items []string) *yaml.Node {
	node := &yaml.Node{
		Kind:  yaml.SequenceNode,
		Style: yaml.FlowStyle,
	}
	for _, item := range items {
		node.Content = append(node.Content, &yaml.Node{
			Kind:  yaml.ScalarNode,
			Value: item,
		})
	}
	return node
}

func (s *Service) serviceToNode(svc types.ServiceConfig) *yaml.Node {
	node := &yaml.Node{Kind: yaml.MappingNode}

	addKeyValue := func(key string, value any) {
		keyNode := &yaml.Node{Kind: yaml.ScalarNode, Value: key}
		var valNode yaml.Node
		if err := valNode.Encode(value); err == nil {
			node.Content = append(node.Content, keyNode, &valNode)
		}
	}

	addKeyNode := func(key string, valNode *yaml.Node) {
		keyNode := &yaml.Node{Kind: yaml.ScalarNode, Value: key}
		node.Content = append(node.Content, keyNode, valNode)
	}

	if svc.Image != "" {
		addKeyValue("image", svc.Image)
	}

	if svc.Build != nil {
		addKeyValue("build", svc.Build)
	}

	if len(svc.Entrypoint) > 0 {
		addKeyValue("entrypoint", s.shellJoin(svc.Entrypoint))
	}

	if len(svc.Command) > 0 {
		addKeyValue("command", s.shellJoin(svc.Command))
	}

	if len(svc.Ports) > 0 {
		ports := make([]string, 0, len(svc.Ports))
		for _, p := range svc.Ports {
			port := ""
			if p.HostIP != "" {
				port += p.HostIP + ":"
			}
			if p.Published != "" {
				port += p.Published + ":"
			}
			port += fmt.Sprintf("%d", p.Target)
			if p.Protocol != "" && p.Protocol != "tcp" {
				port += "/" + p.Protocol
			}
			ports = append(ports, port)
		}
		addKeyNode("ports", s.toFlowStyleArray(ports))
	}

	if len(svc.Volumes) > 0 {
		volumes := make([]string, 0, len(svc.Volumes))
		for _, v := range svc.Volumes {
			vol := v.Source + ":" + v.Target
			if v.ReadOnly {
				vol += ":ro"
			}
			volumes = append(volumes, vol)
		}
		addKeyNode("volumes", s.toFlowStyleArray(volumes))
	}

	if len(svc.Environment) > 0 {
		envNode := s.orderedMapNode(svc.Environment)
		addKeyNode("environment", envNode)
	}

	if len(svc.DependsOn) > 0 {
		depsNode := &yaml.Node{Kind: yaml.MappingNode}
		depNames := make([]string, 0, len(svc.DependsOn))
		for name := range svc.DependsOn {
			depNames = append(depNames, name)
		}
		sort.Strings(depNames)
		for _, name := range depNames {
			dep := svc.DependsOn[name]
			keyNode := &yaml.Node{Kind: yaml.ScalarNode, Value: name}
			valNode := &yaml.Node{Kind: yaml.MappingNode}
			condKey := &yaml.Node{Kind: yaml.ScalarNode, Value: "condition"}
			condVal := &yaml.Node{Kind: yaml.ScalarNode, Value: dep.Condition}
			valNode.Content = append(valNode.Content, condKey, condVal)
			depsNode.Content = append(depsNode.Content, keyNode, valNode)
		}
		addKeyNode("depends_on", depsNode)
	}

	if len(svc.Networks) > 0 {
		hasNonDefault := false
		for name, config := range svc.Networks {
			if name != "default" || config != nil {
				hasNonDefault = true
				break
			}
		}
		if hasNonDefault {
			netNode := &yaml.Node{Kind: yaml.MappingNode}
			netNames := make([]string, 0, len(svc.Networks))
			for name := range svc.Networks {
				netNames = append(netNames, name)
			}
			sort.Strings(netNames)
			for _, name := range netNames {
				config := svc.Networks[name]
				if name == "default" && config == nil {
					continue
				}
				keyNode := &yaml.Node{Kind: yaml.ScalarNode, Value: name}
				var valNode yaml.Node
				valNode.Encode(config)
				netNode.Content = append(netNode.Content, keyNode, &valNode)
			}
			if len(netNode.Content) > 0 {
				addKeyNode("networks", netNode)
			}
		}
	}

	if svc.Restart != "" {
		addKeyValue("restart", svc.Restart)
	}

	if svc.HealthCheck != nil {
		hcNode := &yaml.Node{Kind: yaml.MappingNode}
		addHcKey := func(key string, value any) {
			keyNode := &yaml.Node{Kind: yaml.ScalarNode, Value: key}
			var valNode yaml.Node
			valNode.Encode(value)
			hcNode.Content = append(hcNode.Content, keyNode, &valNode)
		}
		addHcNode := func(key string, valNode *yaml.Node) {
			keyNode := &yaml.Node{Kind: yaml.ScalarNode, Value: key}
			hcNode.Content = append(hcNode.Content, keyNode, valNode)
		}

		if svc.HealthCheck.Disable {
			addHcKey("disable", true)
		} else {
			if len(svc.HealthCheck.Test) > 0 {
				addHcNode("test", s.toFlowStyleArray(svc.HealthCheck.Test))
			}
			if svc.HealthCheck.Interval != nil && *svc.HealthCheck.Interval != 0 {
				addHcKey("interval", time.Duration(*svc.HealthCheck.Interval).String())
			}
			if svc.HealthCheck.Timeout != nil && *svc.HealthCheck.Timeout != 0 {
				addHcKey("timeout", time.Duration(*svc.HealthCheck.Timeout).String())
			}
			if svc.HealthCheck.Retries != nil {
				addHcKey("retries", *svc.HealthCheck.Retries)
			}
			if svc.HealthCheck.StartPeriod != nil && *svc.HealthCheck.StartPeriod != 0 {
				addHcKey("start_period", time.Duration(*svc.HealthCheck.StartPeriod).String())
			}
		}
		if len(hcNode.Content) > 0 {
			addKeyNode("healthcheck", hcNode)
		}
	}

	if len(svc.Labels) > 0 {
		labelsNode := s.orderedStringMapNode(svc.Labels)
		addKeyNode("labels", labelsNode)
	}

	if svc.Deploy != nil {
		addKeyValue("deploy", svc.Deploy)
	}

	return node
}

func (s *Service) orderedMapNode(m map[string]*string) *yaml.Node {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	node := &yaml.Node{Kind: yaml.MappingNode}
	for _, k := range keys {
		keyNode := &yaml.Node{Kind: yaml.ScalarNode, Value: k}
		var valNode *yaml.Node
		if m[k] != nil {
			valNode = &yaml.Node{Kind: yaml.ScalarNode, Value: *m[k]}
		} else {
			valNode = &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!null"}
		}
		node.Content = append(node.Content, keyNode, valNode)
	}
	return node
}

func (s *Service) orderedStringMapNode(m map[string]string) *yaml.Node {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	node := &yaml.Node{Kind: yaml.MappingNode}
	for _, k := range keys {
		keyNode := &yaml.Node{Kind: yaml.ScalarNode, Value: k}
		valNode := &yaml.Node{Kind: yaml.ScalarNode, Value: m[k]}
		node.Content = append(node.Content, keyNode, valNode)
	}
	return node
}
