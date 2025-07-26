package stacks

import (
	"github.com/compose-spec/compose-go/v2/types"
	"github.com/docker/docker/api/types/network"
)

type RuntimeNetworkInfo struct {
	Name     string            `json:"name"`
	ID       string            `json:"id"`
	Driver   string            `json:"driver"`
	Scope    string            `json:"scope"`
	Internal bool              `json:"internal"`
	IPAM     network.IPAM      `json:"ipam"`
	Options  map[string]string `json:"options"`
	Labels   map[string]string `json:"labels"`
	Created  string            `json:"created"`
}

type EnhancedNetworkConfig struct {
	ComposeConfig types.NetworkConfig `json:"compose_config"`
	RuntimeInfo   *RuntimeNetworkInfo `json:"runtime_info,omitempty"`
}

type Stack struct {
	Name               string                           `json:"name"`
	Path               string                           `json:"path"`
	Services           map[string]types.ServiceConfig   `json:"services,omitempty"`
	Networks           map[string]EnhancedNetworkConfig `json:"networks,omitempty"`
	Volumes            map[string]types.VolumeConfig    `json:"volumes,omitempty"`
	ParsedSuccessfully bool                             `json:"parsed_successfully"`
}
