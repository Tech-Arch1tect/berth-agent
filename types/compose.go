package types

type RawComposeConfig struct {
	ComposeFile string         `json:"compose_file"`
	Services    map[string]any `json:"services"`
	Networks    map[string]any `json:"networks,omitempty"`
	Volumes     map[string]any `json:"volumes,omitempty"`
	Secrets     map[string]any `json:"secrets,omitempty"`
	Configs     map[string]any `json:"configs,omitempty"`
}

type ComposeChanges struct {
	ServiceChanges map[string]ServiceChanges   `json:"service_changes,omitempty"`
	NetworkChanges map[string]*NetworkConfig   `json:"network_changes,omitempty"`
	VolumeChanges  map[string]*VolumeConfig    `json:"volume_changes,omitempty"`
	SecretChanges  map[string]*SecretConfig    `json:"secret_changes,omitempty"`
	ConfigChanges  map[string]*ConfigConfig    `json:"config_changes,omitempty"`
	AddServices    map[string]NewServiceConfig `json:"add_services,omitempty"`
	DeleteServices []string                    `json:"delete_services,omitempty"`
	RenameServices map[string]string           `json:"rename_services,omitempty"`
}

type NewServiceConfig struct {
	Image       string            `json:"image"`
	Ports       []PortMapping     `json:"ports,omitempty"`
	Environment map[string]string `json:"environment,omitempty"`
	Volumes     []VolumeMount     `json:"volumes,omitempty"`
	Restart     string            `json:"restart,omitempty"`
}

type ServiceChanges struct {
	Image       *string                          `json:"image,omitempty"`
	Ports       []PortMapping                    `json:"ports,omitempty"`
	Environment map[string]*string               `json:"environment,omitempty"`
	Volumes     []VolumeMount                    `json:"volumes,omitempty"`
	Command     *CommandConfig                   `json:"command,omitempty"`
	Entrypoint  *CommandConfig                   `json:"entrypoint,omitempty"`
	DependsOn   map[string]DependsOnConfig       `json:"depends_on,omitempty"`
	Healthcheck *HealthcheckConfig               `json:"healthcheck,omitempty"`
	Restart     *string                          `json:"restart,omitempty"`
	Labels      map[string]*string               `json:"labels,omitempty"`
	Deploy      *DeployConfig                    `json:"deploy,omitempty"`
	Build       *BuildConfig                     `json:"build,omitempty"`
	Networks    map[string]*ServiceNetworkConfig `json:"networks,omitempty"`
}

type ServiceNetworkConfig struct {
	Aliases     []string `json:"aliases,omitempty"`
	Ipv4Address string   `json:"ipv4_address,omitempty"`
	Ipv6Address string   `json:"ipv6_address,omitempty"`
	Priority    int      `json:"priority,omitempty"`
}

type PortMapping struct {
	Target    string `json:"target"`
	Published string `json:"published,omitempty"`
	HostIP    string `json:"host_ip,omitempty"`
	Protocol  string `json:"protocol,omitempty"`
}

type VolumeMount struct {
	Type     string `json:"type"`
	Source   string `json:"source"`
	Target   string `json:"target"`
	ReadOnly bool   `json:"read_only,omitempty"`
}

type CommandConfig struct {
	Values []string `json:"values"`
}

type DependsOnConfig struct {
	Condition string `json:"condition,omitempty"`
	Restart   bool   `json:"restart,omitempty"`
	Required  bool   `json:"required,omitempty"`
}

type HealthcheckConfig struct {
	Test          []string `json:"test,omitempty"`
	Interval      string   `json:"interval,omitempty"`
	Timeout       string   `json:"timeout,omitempty"`
	Retries       *uint64  `json:"retries,omitempty"`
	StartPeriod   string   `json:"start_period,omitempty"`
	StartInterval string   `json:"start_interval,omitempty"`
	Disable       bool     `json:"disable,omitempty"`
}

type DeployConfig struct {
	Mode           *string               `json:"mode,omitempty"`
	Replicas       *int                  `json:"replicas,omitempty"`
	Resources      *ResourcesConfig      `json:"resources,omitempty"`
	RestartPolicy  *RestartPolicyConfig  `json:"restart_policy,omitempty"`
	Placement      *PlacementConfig      `json:"placement,omitempty"`
	UpdateConfig   *UpdateRollbackConfig `json:"update_config,omitempty"`
	RollbackConfig *UpdateRollbackConfig `json:"rollback_config,omitempty"`
}

type UpdateRollbackConfig struct {
	Parallelism     *int    `json:"parallelism,omitempty"`
	Delay           string  `json:"delay,omitempty"`
	FailureAction   string  `json:"failure_action,omitempty"`
	Monitor         string  `json:"monitor,omitempty"`
	MaxFailureRatio float64 `json:"max_failure_ratio,omitempty"`
	Order           string  `json:"order,omitempty"`
}

type ResourcesConfig struct {
	Limits       *ResourceLimits `json:"limits,omitempty"`
	Reservations *ResourceLimits `json:"reservations,omitempty"`
}

type ResourceLimits struct {
	CPUs   string `json:"cpus,omitempty"`
	Memory string `json:"memory,omitempty"`
}

type RestartPolicyConfig struct {
	Condition   string `json:"condition,omitempty"`
	Delay       string `json:"delay,omitempty"`
	MaxAttempts *int   `json:"max_attempts,omitempty"`
	Window      string `json:"window,omitempty"`
}

type PlacementConfig struct {
	Constraints []string              `json:"constraints,omitempty"`
	Preferences []PlacementPreference `json:"preferences,omitempty"`
}

type PlacementPreference struct {
	Spread string `json:"spread"`
}

type BuildConfig struct {
	Context    string            `json:"context,omitempty"`
	Dockerfile string            `json:"dockerfile,omitempty"`
	Args       map[string]string `json:"args,omitempty"`
	Target     string            `json:"target,omitempty"`
	CacheFrom  []string          `json:"cache_from,omitempty"`
	CacheTo    []string          `json:"cache_to,omitempty"`
	Platforms  []string          `json:"platforms,omitempty"`
}

type NetworkConfig struct {
	Driver     string            `json:"driver,omitempty"`
	DriverOpts map[string]string `json:"driver_opts,omitempty"`
	External   bool              `json:"external,omitempty"`
	Name       string            `json:"name,omitempty"`
	Labels     map[string]string `json:"labels,omitempty"`
	Ipam       *IpamConfig       `json:"ipam,omitempty"`
}

type IpamConfig struct {
	Driver string     `json:"driver,omitempty"`
	Config []IpamPool `json:"config,omitempty"`
}

type IpamPool struct {
	Subnet  string `json:"subnet,omitempty"`
	Gateway string `json:"gateway,omitempty"`
	IpRange string `json:"ip_range,omitempty"`
}

type VolumeConfig struct {
	Driver     string            `json:"driver,omitempty"`
	DriverOpts map[string]string `json:"driver_opts,omitempty"`
	External   bool              `json:"external,omitempty"`
	Name       string            `json:"name,omitempty"`
	Labels     map[string]string `json:"labels,omitempty"`
}

type SecretConfig struct {
	File        string `json:"file,omitempty"`
	Environment string `json:"environment,omitempty"`
	External    bool   `json:"external,omitempty"`
	Name        string `json:"name,omitempty"`
}

type ConfigConfig struct {
	File        string `json:"file,omitempty"`
	Environment string `json:"environment,omitempty"`
	External    bool   `json:"external,omitempty"`
	Name        string `json:"name,omitempty"`
}

type UpdateComposeRequest struct {
	Changes ComposeChanges `json:"changes"`
	Preview bool           `json:"preview,omitempty"`
}

type UpdateComposeResponse struct {
	Success      bool   `json:"success"`
	Message      string `json:"message,omitempty"`
	OriginalYaml string `json:"original_yaml,omitempty"`
	ModifiedYaml string `json:"modified_yaml,omitempty"`
}
