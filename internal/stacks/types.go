package stacks

import "github.com/compose-spec/compose-go/v2/types"

type Stack struct {
	Name               string                         `json:"name"`
	Path               string                         `json:"path"`
	Services           map[string]types.ServiceConfig `json:"services,omitempty"`
	Networks           map[string]types.NetworkConfig `json:"networks,omitempty"`
	Volumes            map[string]types.VolumeConfig  `json:"volumes,omitempty"`
	ParsedSuccessfully bool                           `json:"parsed_successfully"`
}
