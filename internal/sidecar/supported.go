package sidecar

import (
	"errors"
	"fmt"
)

const (
	AgentStackName   = "berth-agent"
	AgentServiceName = "berth-agent"
)

var (
	ErrUnsupportedCommand = errors.New("the sidecar can only bring the agent up or restart it")
	ErrUnsupportedOption  = errors.New("the sidecar does not accept this option when updating the agent")
)

var detachOptions = map[string]bool{
	"-d":       true,
	"--detach": true,
}

var supportedOptions = map[string]map[string]bool{
	"up": {
		"--build":              true,
		"--force-recreate":     true,
		"--no-recreate":        true,
		"--remove-orphans":     true,
		"--renew-anon-volumes": true,
		"-V":                   true,
		"--wait":               true,
		"--no-deps":            true,
	},
	"restart": {
		"--no-deps": true,
	},
}

func ValidateOperation(command string, options []string) error {
	allowed, supported := supportedOptions[command]
	if !supported {
		return fmt.Errorf("%w, so it cannot run %q", ErrUnsupportedCommand, command)
	}
	for _, option := range options {
		if detachOptions[option] || allowed[option] {
			continue
		}
		return fmt.Errorf("%w: %q", ErrUnsupportedOption, option)
	}
	return nil
}

func composeArgs(command string, options []string) []string {
	args := []string{"compose", command}
	for _, option := range options {
		if !detachOptions[option] {
			args = append(args, option)
		}
	}
	args = append(args, AgentServiceName)
	if command == "up" {
		args = append(args, "-d")
	}
	return args
}
