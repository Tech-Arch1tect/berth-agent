package composeeditor

import (
	"fmt"

	"gopkg.in/yaml.v3"
)

func (s *Service) applyRenameServicesToYaml(root *yaml.Node, renames map[string]string) error {
	servicesNode := s.findYamlKey(root, "services")
	if servicesNode == nil {
		return fmt.Errorf("no services section found")
	}

	for oldName, newName := range renames {
		found := false
		for i := 0; i < len(servicesNode.Content); i += 2 {
			if servicesNode.Content[i].Value == oldName {
				for j := 0; j < len(servicesNode.Content); j += 2 {
					if servicesNode.Content[j].Value == newName {
						return fmt.Errorf("service already exists: %s", newName)
					}
				}
				servicesNode.Content[i].Value = newName
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("service not found: %s", oldName)
		}

		for i := 0; i < len(servicesNode.Content); i += 2 {
			serviceNode := servicesNode.Content[i+1]
			if serviceNode.Kind != yaml.MappingNode {
				continue
			}

			dependsOnNode := s.findYamlKey(serviceNode, "depends_on")
			if dependsOnNode == nil {
				continue
			}

			switch dependsOnNode.Kind {
			case yaml.MappingNode:
				for j := 0; j < len(dependsOnNode.Content); j += 2 {
					if dependsOnNode.Content[j].Value == oldName {
						dependsOnNode.Content[j].Value = newName
					}
				}
			case yaml.SequenceNode:
				for j := 0; j < len(dependsOnNode.Content); j++ {
					if dependsOnNode.Content[j].Value == oldName {
						dependsOnNode.Content[j].Value = newName
					}
				}
			}
		}
	}

	return nil
}

func (s *Service) applyDeleteServicesToYaml(root *yaml.Node, deletes []string) error {
	servicesNode := s.findYamlKey(root, "services")
	if servicesNode == nil {
		return fmt.Errorf("no services section found")
	}

	for _, serviceName := range deletes {
		s.deleteYamlKey(servicesNode, serviceName)
	}

	return nil
}

func (s *Service) applyAddServicesToYaml(root *yaml.Node, services map[string]NewServiceConfig) error {
	servicesNode := s.findYamlKey(root, "services")
	if servicesNode == nil {
		servicesNode = createMappingNode()
		s.setYamlNode(root, "services", servicesNode)
	}

	for name, cfg := range services {
		if s.findYamlKey(servicesNode, name) != nil {
			return fmt.Errorf("service already exists: %s", name)
		}

		serviceNode := s.buildNewServiceNode(cfg)
		s.setYamlNode(servicesNode, name, serviceNode)
	}

	return nil
}

func (s *Service) buildNewServiceNode(cfg NewServiceConfig) *yaml.Node {
	node := createMappingNode()

	if cfg.Image != "" {
		appendScalarPair(node, "image", cfg.Image)
	}

	if len(cfg.Ports) > 0 {
		portsNode := s.buildPortsNode(cfg.Ports)
		appendNodePair(node, "ports", portsNode)
	}

	if len(cfg.Environment) > 0 {
		envNode := createMappingNode()
		for k, v := range cfg.Environment {
			appendScalarPair(envNode, k, v)
		}
		appendNodePair(node, "environment", envNode)
	}

	if len(cfg.Volumes) > 0 {
		volumesNode := s.buildVolumesNode(cfg.Volumes)
		appendNodePair(node, "volumes", volumesNode)
	}

	if cfg.Restart != "" {
		appendScalarPair(node, "restart", cfg.Restart)
	}

	return node
}
