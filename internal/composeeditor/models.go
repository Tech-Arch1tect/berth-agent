package composeeditor

import "berth-agent/types"

type (
	RawComposeConfig      = types.RawComposeConfig
	ComposeChanges        = types.ComposeChanges
	NewServiceConfig      = types.NewServiceConfig
	ServiceChanges        = types.ServiceChanges
	ServiceNetworkConfig  = types.ServiceNetworkConfig
	PortMapping           = types.PortMapping
	VolumeMount           = types.VolumeMount
	CommandConfig         = types.CommandConfig
	DependsOnConfig       = types.DependsOnConfig
	HealthcheckConfig     = types.HealthcheckConfig
	DeployConfig          = types.DeployConfig
	UpdateRollbackConfig  = types.UpdateRollbackConfig
	ResourcesConfig       = types.ResourcesConfig
	ResourceLimits        = types.ResourceLimits
	RestartPolicyConfig   = types.RestartPolicyConfig
	PlacementConfig       = types.PlacementConfig
	PlacementPreference   = types.PlacementPreference
	BuildConfig           = types.BuildConfig
	NetworkConfig         = types.NetworkConfig
	IpamConfig            = types.IpamConfig
	IpamPool              = types.IpamPool
	VolumeConfig          = types.VolumeConfig
	SecretConfig          = types.SecretConfig
	ConfigConfig          = types.ConfigConfig
	UpdateComposeRequest  = types.UpdateComposeRequest
	UpdateComposeResponse = types.UpdateComposeResponse
)
