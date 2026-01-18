package composeeditor

import (
	"berth-agent/config"
	"berth-agent/internal/logging"
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

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

func (s *Service) GetComposeConfig(ctx context.Context, stackName string) (*RawComposeConfig, error) {
	stackPath := filepath.Join(s.stackLocation, stackName)

	if _, err := os.Stat(stackPath); os.IsNotExist(err) {
		return nil, fmt.Errorf("stack not found: %s", stackName)
	}

	composeFile, err := s.findComposeFile(stackPath)
	if err != nil {
		return nil, fmt.Errorf("failed to find compose file: %w", err)
	}

	s.logger.Debug("reading compose file",
		zap.String("stack", stackName),
		zap.String("file", composeFile),
	)

	content, err := os.ReadFile(composeFile)
	if err != nil {
		return nil, fmt.Errorf("failed to read compose file: %w", err)
	}

	var rawConfig map[string]any
	if err := yaml.Unmarshal(content, &rawConfig); err != nil {
		return nil, fmt.Errorf("failed to parse compose file: %w", err)
	}

	result := &RawComposeConfig{
		ComposeFile: filepath.Base(composeFile),
	}

	if services, ok := rawConfig["services"].(map[string]any); ok {
		result.Services = services
	}
	if networks, ok := rawConfig["networks"].(map[string]any); ok {
		result.Networks = networks
	}
	if volumes, ok := rawConfig["volumes"].(map[string]any); ok {
		result.Volumes = volumes
	}
	if secrets, ok := rawConfig["secrets"].(map[string]any); ok {
		result.Secrets = secrets
	}
	if configs, ok := rawConfig["configs"].(map[string]any); ok {
		result.Configs = configs
	}

	return result, nil
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

	content, err := os.ReadFile(composeFile)
	if err != nil {
		return fmt.Errorf("failed to read compose file: %w", err)
	}

	var doc yaml.Node
	if err := yaml.Unmarshal(content, &doc); err != nil {
		return fmt.Errorf("failed to parse compose file: %w", err)
	}

	if err := s.applyChangesToYaml(&doc, changes); err != nil {
		return fmt.Errorf("failed to apply changes: %w", err)
	}

	var buf bytes.Buffer
	encoder := yaml.NewEncoder(&buf)
	encoder.SetIndent(2)
	if err := encoder.Encode(&doc); err != nil {
		return fmt.Errorf("failed to encode yaml: %w", err)
	}
	encoder.Close()

	yamlContent := addBlankLinesBetweenSections(buf.String())

	if err := s.validateComposeYaml(stackPath, yamlContent); err != nil {
		return fmt.Errorf("validation failed: %w", err)
	}

	if err := os.WriteFile(composeFile, []byte(yamlContent), 0644); err != nil {
		return fmt.Errorf("failed to write compose file: %w", err)
	}

	return nil
}

func (s *Service) PreviewCompose(ctx context.Context, stackName string, changes ComposeChanges) (originalYaml, modifiedYaml string, err error) {
	stackPath := filepath.Join(s.stackLocation, stackName)

	if _, err := os.Stat(stackPath); os.IsNotExist(err) {
		return "", "", fmt.Errorf("stack not found: %s", stackName)
	}

	composeFile, err := s.findComposeFile(stackPath)
	if err != nil {
		return "", "", fmt.Errorf("failed to find compose file: %w", err)
	}

	content, err := os.ReadFile(composeFile)
	if err != nil {
		return "", "", fmt.Errorf("failed to read compose file: %w", err)
	}

	originalYaml = string(content)

	var doc yaml.Node
	if err := yaml.Unmarshal(content, &doc); err != nil {
		return "", "", fmt.Errorf("failed to parse compose file: %w", err)
	}

	if err := s.applyChangesToYaml(&doc, changes); err != nil {
		return "", "", fmt.Errorf("failed to apply changes: %w", err)
	}

	var buf bytes.Buffer
	encoder := yaml.NewEncoder(&buf)
	encoder.SetIndent(2)
	if err := encoder.Encode(&doc); err != nil {
		return "", "", fmt.Errorf("failed to encode yaml: %w", err)
	}
	encoder.Close()

	modifiedYaml = addBlankLinesBetweenSections(buf.String())

	return originalYaml, modifiedYaml, nil
}

var serviceDefRegex = regexp.MustCompile(`(?m)^  [a-zA-Z][a-zA-Z0-9_-]*:\s*$`)

func addBlankLinesBetweenSections(yaml string) string {
	lines := strings.Split(yaml, "\n")
	var result []string
	inServices := false

	for i, line := range lines {
		if strings.HasPrefix(line, "services:") {
			inServices = true
		} else if len(line) > 0 && line[0] != ' ' && line[0] != '#' {
			inServices = false
		}

		if inServices && i > 0 && serviceDefRegex.MatchString(line) {
			prev := lines[i-1]
			if len(prev) > 0 && prev != "" {
				result = append(result, "")
			}
		}

		result = append(result, line)
	}

	return strings.Join(result, "\n")
}

func (s *Service) applyChangesToYaml(doc *yaml.Node, changes ComposeChanges) error {
	root := doc
	if doc.Kind == yaml.DocumentNode && len(doc.Content) > 0 {
		root = doc.Content[0]
	}

	if root.Kind != yaml.MappingNode {
		return fmt.Errorf("invalid compose document structure")
	}

	if changes.ServiceChanges != nil {
		if err := s.applyServiceChangesToYaml(root, changes.ServiceChanges); err != nil {
			return err
		}
	}
	if changes.NetworkChanges != nil {
		if err := s.applyNetworkChangesToYaml(root, changes.NetworkChanges); err != nil {
			return err
		}
	}
	if changes.VolumeChanges != nil {
		if err := s.applyVolumeChangesToYaml(root, changes.VolumeChanges); err != nil {
			return err
		}
	}
	if changes.SecretChanges != nil {
		if err := s.applySecretChangesToYaml(root, changes.SecretChanges); err != nil {
			return err
		}
	}
	if changes.ConfigChanges != nil {
		if err := s.applyConfigChangesToYaml(root, changes.ConfigChanges); err != nil {
			return err
		}
	}
	if changes.RenameServices != nil {
		if err := s.applyRenameServicesToYaml(root, changes.RenameServices); err != nil {
			return err
		}
	}
	if changes.DeleteServices != nil {
		if err := s.applyDeleteServicesToYaml(root, changes.DeleteServices); err != nil {
			return err
		}
	}
	if changes.AddServices != nil {
		if err := s.applyAddServicesToYaml(root, changes.AddServices); err != nil {
			return err
		}
	}

	return nil
}

func (s *Service) applyServiceChangesToYaml(root *yaml.Node, serviceChanges map[string]ServiceChanges) error {
	servicesNode := s.findYamlKey(root, "services")
	if servicesNode == nil {
		return fmt.Errorf("no services section found")
	}

	for serviceName, svcChanges := range serviceChanges {
		serviceNode := s.findYamlKey(servicesNode, serviceName)
		if serviceNode == nil {
			return fmt.Errorf("service not found: %s", serviceName)
		}

		if svcChanges.Image != nil {
			s.setYamlValue(serviceNode, "image", *svcChanges.Image)
		}
		if svcChanges.Restart != nil {
			s.setYamlValue(serviceNode, "restart", *svcChanges.Restart)
		}
		if svcChanges.Ports != nil {
			s.setYamlNode(serviceNode, "ports", s.buildPortsNode(svcChanges.Ports))
		}
		if svcChanges.Environment != nil {
			if err := s.applyEnvironmentChanges(serviceNode, svcChanges.Environment); err != nil {
				return fmt.Errorf("failed to apply environment changes: %w", err)
			}
		}
		if svcChanges.Volumes != nil {
			s.setYamlNode(serviceNode, "volumes", s.buildVolumesNode(svcChanges.Volumes))
		}
		if svcChanges.Command != nil {
			s.setYamlNode(serviceNode, "command", s.buildCommandNode(svcChanges.Command.Values))
		}
		if svcChanges.Entrypoint != nil {
			s.setYamlNode(serviceNode, "entrypoint", s.buildCommandNode(svcChanges.Entrypoint.Values))
		}
		if svcChanges.Labels != nil {
			if err := s.applyLabelsChanges(serviceNode, svcChanges.Labels); err != nil {
				return fmt.Errorf("failed to apply labels changes: %w", err)
			}
		}
		if svcChanges.DependsOn != nil {
			s.setYamlNode(serviceNode, "depends_on", s.buildDependsOnNode(svcChanges.DependsOn))
		}
		if svcChanges.Healthcheck != nil {
			s.setYamlNode(serviceNode, "healthcheck", s.buildHealthcheckNode(svcChanges.Healthcheck))
		}
		if svcChanges.Deploy != nil {
			s.setYamlNode(serviceNode, "deploy", s.buildDeployNode(svcChanges.Deploy))
		}
		if svcChanges.Build != nil {
			s.setYamlNode(serviceNode, "build", s.buildBuildNode(svcChanges.Build))
		}
		if svcChanges.Networks != nil {
			s.setYamlNode(serviceNode, "networks", s.buildServiceNetworksNode(svcChanges.Networks))
		}
	}

	return nil
}

func (s *Service) applyEnvironmentChanges(serviceNode *yaml.Node, envChanges map[string]*string) error {
	envNode := s.findYamlKey(serviceNode, "environment")
	if envNode == nil {
		envNode = createMappingNode()
		s.setYamlNode(serviceNode, "environment", envNode)
	}
	if envNode.Kind == yaml.SequenceNode {
		envNode.Content = convertSequenceContent(envNode.Content, "=")
		envNode.Kind = yaml.MappingNode
		envNode.Tag = ""
		envNode.Style = 0
	}
	for key, val := range envChanges {
		if val == nil {
			s.deleteYamlKey(envNode, key)
		} else {
			s.setYamlValue(envNode, key, *val)
		}
	}
	return nil
}

func (s *Service) applyLabelsChanges(serviceNode *yaml.Node, labelChanges map[string]*string) error {
	labelsNode := s.findYamlKey(serviceNode, "labels")
	if labelsNode == nil {
		labelsNode = createMappingNode()
		s.setYamlNode(serviceNode, "labels", labelsNode)
	}
	if labelsNode.Kind == yaml.SequenceNode {
		labelsNode.Content = convertSequenceContent(labelsNode.Content, "=")
		labelsNode.Kind = yaml.MappingNode
		labelsNode.Tag = ""
		labelsNode.Style = 0
	}
	for key, val := range labelChanges {
		if val == nil {
			s.deleteYamlKey(labelsNode, key)
		} else {
			s.setYamlValue(labelsNode, key, *val)
		}
	}
	return nil
}
