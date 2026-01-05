package composeeditor

import "github.com/compose-spec/compose-go/v2/types"

type ComposeConfig struct {
	ComposeFile string         `json:"compose_file"`
	Services    types.Services `json:"services"`
	Networks    types.Networks `json:"networks,omitempty"`
	Volumes     types.Volumes  `json:"volumes,omitempty"`
	Secrets     types.Secrets  `json:"secrets,omitempty"`
	Configs     types.Configs  `json:"configs,omitempty"`
}

type ComposeChanges struct {
	ServiceChanges map[string]ServiceChanges `json:"service_changes,omitempty"`
}

type ServiceChanges struct {
	Image       *string                    `json:"image,omitempty"`
	Ports       []PortMapping              `json:"ports,omitempty"`
	Environment map[string]*string         `json:"environment,omitempty"`
	Volumes     []VolumeMount              `json:"volumes,omitempty"`
	Command     *CommandConfig             `json:"command,omitempty"`
	Entrypoint  *CommandConfig             `json:"entrypoint,omitempty"`
	DependsOn   map[string]DependsOnConfig `json:"depends_on,omitempty"`
	Healthcheck *HealthcheckConfig         `json:"healthcheck,omitempty"`
	Restart     *string                    `json:"restart,omitempty"`
	Labels      map[string]*string         `json:"labels,omitempty"`
	Deploy      *DeployConfig              `json:"deploy,omitempty"`
	Build       *BuildConfig               `json:"build,omitempty"`
}

type PortMapping struct {
	Target    uint32 `json:"target"`
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

type UpdateComposeRequest struct {
	Changes ComposeChanges `json:"changes"`
}

type UpdateComposeResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message,omitempty"`
}
