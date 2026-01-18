package composeeditor

import (
	"gopkg.in/yaml.v3"
)

func (s *Service) applyNetworkChangesToYaml(root *yaml.Node, changes map[string]*NetworkConfig) error {
	networksNode := s.findYamlKey(root, "networks")

	if networksNode == nil {
		hasAdditions := false
		for _, cfg := range changes {
			if cfg != nil {
				hasAdditions = true
				break
			}
		}
		if hasAdditions {
			networksNode = createMappingNode()
			s.setYamlNode(root, "networks", networksNode)
		} else {
			return nil
		}
	}

	for name, cfg := range changes {
		if cfg == nil {
			s.deleteYamlKey(networksNode, name)
		} else {
			netNode := s.buildNetworkConfigNode(cfg)
			s.setYamlNode(networksNode, name, netNode)
		}
	}

	return nil
}

func (s *Service) buildNetworkConfigNode(cfg *NetworkConfig) *yaml.Node {
	node := createMappingNode()

	if cfg.Driver != "" {
		appendScalarPair(node, "driver", cfg.Driver)
	}

	if len(cfg.DriverOpts) > 0 {
		optsNode := createMappingNode()
		for k, v := range cfg.DriverOpts {
			appendScalarPair(optsNode, k, v)
		}
		appendNodePair(node, "driver_opts", optsNode)
	}

	if cfg.External {
		appendBoolPair(node, "external", true)
	}

	if cfg.Name != "" {
		appendScalarPair(node, "name", cfg.Name)
	}

	if len(cfg.Labels) > 0 {
		labelsNode := createMappingNode()
		for k, v := range cfg.Labels {
			appendScalarPair(labelsNode, k, v)
		}
		appendNodePair(node, "labels", labelsNode)
	}

	if cfg.Ipam != nil {
		ipamNode := s.buildIpamConfigNode(cfg.Ipam)
		appendNodePair(node, "ipam", ipamNode)
	}

	if len(node.Content) == 0 {
		return createNullNode()
	}

	return node
}

func (s *Service) buildIpamConfigNode(cfg *IpamConfig) *yaml.Node {
	node := createMappingNode()

	if cfg.Driver != "" {
		appendScalarPair(node, "driver", cfg.Driver)
	}

	if len(cfg.Config) > 0 {
		configNode := createSequenceNode()
		for _, pool := range cfg.Config {
			poolNode := createMappingNode()
			if pool.Subnet != "" {
				appendScalarPair(poolNode, "subnet", pool.Subnet)
			}
			if pool.Gateway != "" {
				appendScalarPair(poolNode, "gateway", pool.Gateway)
			}
			if pool.IpRange != "" {
				appendScalarPair(poolNode, "ip_range", pool.IpRange)
			}
			configNode.Content = append(configNode.Content, poolNode)
		}
		appendNodePair(node, "config", configNode)
	}

	return node
}

func (s *Service) applyVolumeChangesToYaml(root *yaml.Node, changes map[string]*VolumeConfig) error {
	volumesNode := s.findYamlKey(root, "volumes")

	if volumesNode == nil {
		hasAdditions := false
		for _, cfg := range changes {
			if cfg != nil {
				hasAdditions = true
				break
			}
		}
		if hasAdditions {
			volumesNode = createMappingNode()
			s.setYamlNode(root, "volumes", volumesNode)
		} else {
			return nil
		}
	}

	for name, cfg := range changes {
		if cfg == nil {
			s.deleteYamlKey(volumesNode, name)
		} else {
			volNode := s.buildVolumeConfigNode(cfg)
			s.setYamlNode(volumesNode, name, volNode)
		}
	}

	return nil
}

func (s *Service) buildVolumeConfigNode(cfg *VolumeConfig) *yaml.Node {
	node := createMappingNode()

	if cfg.Driver != "" {
		appendScalarPair(node, "driver", cfg.Driver)
	}

	if len(cfg.DriverOpts) > 0 {
		optsNode := createMappingNode()
		for k, v := range cfg.DriverOpts {
			appendScalarPair(optsNode, k, v)
		}
		appendNodePair(node, "driver_opts", optsNode)
	}

	if cfg.External {
		appendBoolPair(node, "external", true)
	}

	if cfg.Name != "" {
		appendScalarPair(node, "name", cfg.Name)
	}

	if len(cfg.Labels) > 0 {
		labelsNode := createMappingNode()
		for k, v := range cfg.Labels {
			appendScalarPair(labelsNode, k, v)
		}
		appendNodePair(node, "labels", labelsNode)
	}

	if len(node.Content) == 0 {
		return createNullNode()
	}

	return node
}

func (s *Service) applySecretChangesToYaml(root *yaml.Node, changes map[string]*SecretConfig) error {
	secretsNode := s.findYamlKey(root, "secrets")

	if secretsNode == nil {
		hasAdditions := false
		for _, cfg := range changes {
			if cfg != nil {
				hasAdditions = true
				break
			}
		}
		if hasAdditions {
			secretsNode = createMappingNode()
			s.setYamlNode(root, "secrets", secretsNode)
		} else {
			return nil
		}
	}

	for name, cfg := range changes {
		if cfg == nil {
			s.deleteYamlKey(secretsNode, name)
		} else {
			secNode := s.buildSecretConfigNode(cfg)
			s.setYamlNode(secretsNode, name, secNode)
		}
	}

	return nil
}

func (s *Service) buildSecretConfigNode(cfg *SecretConfig) *yaml.Node {
	node := createMappingNode()

	if cfg.File != "" {
		appendScalarPair(node, "file", cfg.File)
	}

	if cfg.Environment != "" {
		appendScalarPair(node, "environment", cfg.Environment)
	}

	if cfg.External {
		appendBoolPair(node, "external", true)
	}

	if cfg.Name != "" {
		appendScalarPair(node, "name", cfg.Name)
	}

	return node
}

func (s *Service) applyConfigChangesToYaml(root *yaml.Node, changes map[string]*ConfigConfig) error {
	configsNode := s.findYamlKey(root, "configs")

	if configsNode == nil {
		hasAdditions := false
		for _, cfg := range changes {
			if cfg != nil {
				hasAdditions = true
				break
			}
		}
		if hasAdditions {
			configsNode = createMappingNode()
			s.setYamlNode(root, "configs", configsNode)
		} else {
			return nil
		}
	}

	for name, cfg := range changes {
		if cfg == nil {
			s.deleteYamlKey(configsNode, name)
		} else {
			cfgNode := s.buildConfigConfigNode(cfg)
			s.setYamlNode(configsNode, name, cfgNode)
		}
	}

	return nil
}

func (s *Service) buildConfigConfigNode(cfg *ConfigConfig) *yaml.Node {
	node := createMappingNode()

	if cfg.File != "" {
		appendScalarPair(node, "file", cfg.File)
	}

	if cfg.Environment != "" {
		appendScalarPair(node, "environment", cfg.Environment)
	}

	if cfg.External {
		appendBoolPair(node, "external", true)
	}

	if cfg.Name != "" {
		appendScalarPair(node, "name", cfg.Name)
	}

	return node
}
