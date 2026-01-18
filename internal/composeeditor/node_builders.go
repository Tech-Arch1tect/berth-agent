package composeeditor

import (
	"fmt"

	"gopkg.in/yaml.v3"
)

func (s *Service) buildPortsNode(ports []PortMapping) *yaml.Node {
	node := createSequenceNode()
	for _, p := range ports {
		var port string
		if p.HostIP != "" {
			port += p.HostIP + ":"
		}
		if p.Published != "" {
			port += p.Published + ":"
		}
		port += p.Target
		if p.Protocol != "" && p.Protocol != "tcp" {
			port += "/" + p.Protocol
		}
		node.Content = append(node.Content, createScalarNode(port))
	}
	return node
}

func (s *Service) buildVolumesNode(volumes []VolumeMount) *yaml.Node {
	node := createSequenceNode()
	for _, v := range volumes {
		vol := v.Source + ":" + v.Target
		if v.ReadOnly {
			vol += ":ro"
		}
		node.Content = append(node.Content, createScalarNode(vol))
	}
	return node
}

func (s *Service) buildCommandNode(values []string) *yaml.Node {
	if len(values) == 0 {

		return createNullNode()
	}
	if len(values) == 1 {
		return createScalarNode(values[0])
	}
	node := createSequenceNode()
	for _, v := range values {
		node.Content = append(node.Content, createScalarNode(v))
	}
	return node
}

func (s *Service) buildDependsOnNode(deps map[string]DependsOnConfig) *yaml.Node {
	node := createMappingNode()
	for name, cfg := range deps {
		valNode := createMappingNode()

		condition := cfg.Condition
		if condition == "" {
			condition = "service_started"
		}
		appendScalarPair(valNode, "condition", condition)

		if cfg.Restart {
			appendBoolPair(valNode, "restart", true)
		}

		if cfg.Required {
			appendBoolPair(valNode, "required", true)
		}

		appendNodePair(node, name, valNode)
	}
	return node
}

func (s *Service) buildHealthcheckNode(hc *HealthcheckConfig) *yaml.Node {
	node := createMappingNode()

	if hc.Disable {
		appendBoolPair(node, "disable", true)
		return node
	}

	if len(hc.Test) > 0 {
		testNode := createSequenceNode()
		for _, t := range hc.Test {
			testNode.Content = append(testNode.Content, createScalarNode(t))
		}
		appendNodePair(node, "test", testNode)
	}

	if hc.Interval != "" {
		appendScalarPair(node, "interval", hc.Interval)
	}

	if hc.Timeout != "" {
		appendScalarPair(node, "timeout", hc.Timeout)
	}

	if hc.Retries != nil {
		appendUint64Pair(node, "retries", *hc.Retries)
	}

	if hc.StartPeriod != "" {
		appendScalarPair(node, "start_period", hc.StartPeriod)
	}

	if hc.StartInterval != "" {
		appendScalarPair(node, "start_interval", hc.StartInterval)
	}

	return node
}

func (s *Service) buildDeployNode(deploy *DeployConfig) *yaml.Node {
	node := createMappingNode()

	if deploy.Mode != nil {
		appendScalarPair(node, "mode", *deploy.Mode)
	}

	if deploy.Replicas != nil {
		appendIntPair(node, "replicas", *deploy.Replicas)
	}

	if deploy.Resources != nil {
		resourcesNode := s.buildResourcesNode(deploy.Resources)
		appendNodePair(node, "resources", resourcesNode)
	}

	if deploy.RestartPolicy != nil {
		restartPolicyNode := s.buildRestartPolicyNode(deploy.RestartPolicy)
		appendNodePair(node, "restart_policy", restartPolicyNode)
	}

	if deploy.Placement != nil {
		placementNode := s.buildPlacementNode(deploy.Placement)
		appendNodePair(node, "placement", placementNode)
	}

	if deploy.UpdateConfig != nil {
		updateConfigNode := s.buildUpdateRollbackConfigNode(deploy.UpdateConfig)
		appendNodePair(node, "update_config", updateConfigNode)
	}

	if deploy.RollbackConfig != nil {
		rollbackConfigNode := s.buildUpdateRollbackConfigNode(deploy.RollbackConfig)
		appendNodePair(node, "rollback_config", rollbackConfigNode)
	}

	return node
}

func (s *Service) buildResourcesNode(resources *ResourcesConfig) *yaml.Node {
	node := createMappingNode()

	if resources.Limits != nil {
		limitsNode := s.buildResourceLimitsNode(resources.Limits)
		appendNodePair(node, "limits", limitsNode)
	}

	if resources.Reservations != nil {
		reservationsNode := s.buildResourceLimitsNode(resources.Reservations)
		appendNodePair(node, "reservations", reservationsNode)
	}

	return node
}

func (s *Service) buildResourceLimitsNode(limits *ResourceLimits) *yaml.Node {
	node := createMappingNode()

	if limits.CPUs != "" {
		appendScalarPair(node, "cpus", limits.CPUs)
	}

	if limits.Memory != "" {
		appendScalarPair(node, "memory", limits.Memory)
	}

	return node
}

func (s *Service) buildRestartPolicyNode(policy *RestartPolicyConfig) *yaml.Node {
	node := createMappingNode()

	if policy.Condition != "" {
		appendScalarPair(node, "condition", policy.Condition)
	}

	if policy.Delay != "" {
		appendScalarPair(node, "delay", policy.Delay)
	}

	if policy.MaxAttempts != nil {
		appendIntPair(node, "max_attempts", *policy.MaxAttempts)
	}

	if policy.Window != "" {
		appendScalarPair(node, "window", policy.Window)
	}

	return node
}

func (s *Service) buildPlacementNode(placement *PlacementConfig) *yaml.Node {
	node := createMappingNode()

	if len(placement.Constraints) > 0 {
		constraintsNode := createSequenceNode()
		for _, c := range placement.Constraints {
			constraintsNode.Content = append(constraintsNode.Content, createScalarNode(c))
		}
		appendNodePair(node, "constraints", constraintsNode)
	}

	if len(placement.Preferences) > 0 {
		prefsNode := createSequenceNode()
		for _, pref := range placement.Preferences {
			prefNode := createMappingNode()
			appendScalarPair(prefNode, "spread", pref.Spread)
			prefsNode.Content = append(prefsNode.Content, prefNode)
		}
		appendNodePair(node, "preferences", prefsNode)
	}

	return node
}

func (s *Service) buildUpdateRollbackConfigNode(config *UpdateRollbackConfig) *yaml.Node {
	node := createMappingNode()

	if config.Parallelism != nil {
		appendIntPair(node, "parallelism", *config.Parallelism)
	}

	if config.Delay != "" {
		appendScalarPair(node, "delay", config.Delay)
	}

	if config.FailureAction != "" {
		appendScalarPair(node, "failure_action", config.FailureAction)
	}

	if config.Monitor != "" {
		appendScalarPair(node, "monitor", config.Monitor)
	}

	if config.MaxFailureRatio != 0 {
		appendScalarPair(node, "max_failure_ratio", fmt.Sprintf("%g", config.MaxFailureRatio))
	}

	if config.Order != "" {
		appendScalarPair(node, "order", config.Order)
	}

	return node
}

func (s *Service) buildBuildNode(build *BuildConfig) *yaml.Node {
	node := createMappingNode()

	if build.Context != "" {
		appendScalarPair(node, "context", build.Context)
	}

	if build.Dockerfile != "" {
		appendScalarPair(node, "dockerfile", build.Dockerfile)
	}

	if build.Target != "" {
		appendScalarPair(node, "target", build.Target)
	}

	if len(build.Args) > 0 {
		argsNode := createMappingNode()
		for k, v := range build.Args {
			appendScalarPair(argsNode, k, v)
		}
		appendNodePair(node, "args", argsNode)
	}

	if len(build.CacheFrom) > 0 {
		cacheFromNode := createSequenceNode()
		for _, c := range build.CacheFrom {
			cacheFromNode.Content = append(cacheFromNode.Content, createScalarNode(c))
		}
		appendNodePair(node, "cache_from", cacheFromNode)
	}

	if len(build.CacheTo) > 0 {
		cacheToNode := createSequenceNode()
		for _, c := range build.CacheTo {
			cacheToNode.Content = append(cacheToNode.Content, createScalarNode(c))
		}
		appendNodePair(node, "cache_to", cacheToNode)
	}

	if len(build.Platforms) > 0 {
		platformsNode := createSequenceNode()
		for _, p := range build.Platforms {
			platformsNode.Content = append(platformsNode.Content, createScalarNode(p))
		}
		appendNodePair(node, "platforms", platformsNode)
	}

	return node
}

func (s *Service) buildServiceNetworksNode(networks map[string]*ServiceNetworkConfig) *yaml.Node {
	node := createMappingNode()

	for name, cfg := range networks {
		if cfg == nil {
			appendNodePair(node, name, createNullNode())
			continue
		}

		valNode := createMappingNode()

		if len(cfg.Aliases) > 0 {
			aliasesNode := createSequenceNode()
			for _, a := range cfg.Aliases {
				aliasesNode.Content = append(aliasesNode.Content, createScalarNode(a))
			}
			appendNodePair(valNode, "aliases", aliasesNode)
		}

		if cfg.Ipv4Address != "" {
			appendScalarPair(valNode, "ipv4_address", cfg.Ipv4Address)
		}

		if cfg.Ipv6Address != "" {
			appendScalarPair(valNode, "ipv6_address", cfg.Ipv6Address)
		}

		if cfg.Priority != 0 {
			appendIntPair(valNode, "priority", cfg.Priority)
		}

		if len(valNode.Content) == 0 {
			appendNodePair(node, name, createNullNode())
		} else {
			appendNodePair(node, name, valNode)
		}
	}

	return node
}
