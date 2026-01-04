package composeeditor

import (
	"berth-agent/config"
	"berth-agent/internal/logging"
	"context"
	"fmt"
	"os"
	"path/filepath"
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

func (s *Service) writeComposeFile(filePath string, project *types.Project) error {
	content := map[string]any{}

	if len(project.Services) > 0 {
		services := map[string]any{}
		for name, svc := range project.Services {
			services[name] = s.serviceToMap(svc)
		}
		content["services"] = services
	}

	if len(project.Networks) > 0 {
		content["networks"] = project.Networks
	}

	if len(project.Volumes) > 0 {
		content["volumes"] = project.Volumes
	}

	if len(project.Secrets) > 0 {
		content["secrets"] = project.Secrets
	}

	if len(project.Configs) > 0 {
		content["configs"] = project.Configs
	}

	data, err := yaml.Marshal(content)
	if err != nil {
		return fmt.Errorf("failed to marshal compose: %w", err)
	}

	if err := os.WriteFile(filePath, data, 0644); err != nil {
		return fmt.Errorf("failed to write file: %w", err)
	}

	return nil
}

func (s *Service) serviceToMap(svc types.ServiceConfig) map[string]any {
	result := map[string]any{}

	if svc.Image != "" {
		result["image"] = svc.Image
	}

	if svc.Build != nil {
		result["build"] = svc.Build
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
		result["ports"] = ports
	}

	if len(svc.Environment) > 0 {
		env := map[string]any{}
		for k, v := range svc.Environment {
			if v != nil {
				env[k] = *v
			} else {
				env[k] = nil
			}
		}
		result["environment"] = env
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
		result["volumes"] = volumes
	}

	if len(svc.Command) > 0 {
		if len(svc.Command) == 1 {
			result["command"] = svc.Command[0]
		} else {
			result["command"] = svc.Command
		}
	}

	if len(svc.Entrypoint) > 0 {
		if len(svc.Entrypoint) == 1 {
			result["entrypoint"] = svc.Entrypoint[0]
		} else {
			result["entrypoint"] = svc.Entrypoint
		}
	}

	if svc.Restart != "" {
		result["restart"] = svc.Restart
	}

	if len(svc.Labels) > 0 {
		result["labels"] = svc.Labels
	}

	if len(svc.DependsOn) > 0 {
		deps := map[string]any{}
		for name, dep := range svc.DependsOn {
			deps[name] = map[string]any{
				"condition": dep.Condition,
			}
		}
		result["depends_on"] = deps
	}

	if svc.HealthCheck != nil {
		hc := map[string]any{}
		if svc.HealthCheck.Disable {
			hc["disable"] = true
		} else {
			if len(svc.HealthCheck.Test) > 0 {
				hc["test"] = svc.HealthCheck.Test
			}
			if svc.HealthCheck.Interval != nil && *svc.HealthCheck.Interval != 0 {
				hc["interval"] = time.Duration(*svc.HealthCheck.Interval).String()
			}
			if svc.HealthCheck.Timeout != nil && *svc.HealthCheck.Timeout != 0 {
				hc["timeout"] = time.Duration(*svc.HealthCheck.Timeout).String()
			}
			if svc.HealthCheck.Retries != nil {
				hc["retries"] = *svc.HealthCheck.Retries
			}
			if svc.HealthCheck.StartPeriod != nil && *svc.HealthCheck.StartPeriod != 0 {
				hc["start_period"] = time.Duration(*svc.HealthCheck.StartPeriod).String()
			}
		}
		if len(hc) > 0 {
			result["healthcheck"] = hc
		}
	}

	if len(svc.Networks) > 0 {
		result["networks"] = svc.Networks
	}

	if svc.Deploy != nil {
		result["deploy"] = svc.Deploy
	}

	return result
}
