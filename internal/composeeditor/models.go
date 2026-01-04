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

type UpdateComposeRequest struct {
	Changes ComposeChanges `json:"changes"`
}

type UpdateComposeResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message,omitempty"`
}
