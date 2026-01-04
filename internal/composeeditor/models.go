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
