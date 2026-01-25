package operations

import (
	"errors"
	"github.com/tech-arch1tect/berth-agent/internal/archive"
	"regexp"
	"slices"
	"strings"
)

var (
	ErrInvalidCommand     = errors.New("invalid command")
	ErrInvalidOption      = errors.New("invalid option")
	ErrInvalidServiceName = errors.New("invalid service name")
	ErrCommandInjection   = errors.New("command injection detected")
)

var validCommands = map[string]bool{
	"up":              true,
	"down":            true,
	"start":           true,
	"stop":            true,
	"restart":         true,
	"pull":            true,
	"create-archive":  true,
	"extract-archive": true,
}

var validOptions = map[string]map[string]bool{
	"up": {
		"-d":                           true,
		"--detach":                     true,
		"--build":                      true,
		"--force-recreate":             true,
		"--no-recreate":                true,
		"--remove-orphans":             true,
		"--pull":                       true,
		"--scale":                      true,
		"-t":                           true,
		"--timeout":                    true,
		"--wait":                       true,
		"--wait-timeout":               true,
		"-V":                           true,
		"--renew-anon-volumes":         true,
		"--no-deps":                    true,
		"--abort-on-container-exit":    true,
		"--abort-on-container-failure": true,
	},
	"down": {
		"--remove-orphans": true,
		"--rmi":            true,
		"-v":               true,
		"--volumes":        true,
		"-t":               true,
		"--timeout":        true,
	},
	"start": {
		"--dry-run": true,
	},
	"stop": {
		"-t":        true,
		"--timeout": true,
		"--dry-run": true,
	},
	"restart": {
		"-t":        true,
		"--timeout": true,
		"--no-deps": true,
		"--dry-run": true,
	},
	"pull": {
		"-q":                     true,
		"--quiet":                true,
		"--ignore-buildable":     true,
		"--ignore-pull-failures": true,
		"--include-deps":         true,
		"--policy":               true,
	},
}

var validOptionValues = map[string]map[string]bool{
	"--pull": {
		"always":  true,
		"missing": true,
		"never":   true,
	},
	"--rmi": {
		"local": true,
		"all":   true,
	},
	"--policy": {
		"missing": true,
		"always":  true,
	},
}

var validServiceNameRegex = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]*$`)

var numericValueRegex = regexp.MustCompile(`^\d+$`)

var scaleValueRegex = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]*=\d+$`)

func ValidateOperationRequest(req OperationRequest) error {
	if !validCommands[req.Command] {
		return ErrInvalidCommand
	}

	// Handle archive commands separately
	if req.Command == "create-archive" {
		return archive.ValidateCreateOptions(req.Options)
	}
	if req.Command == "extract-archive" {
		return archive.ValidateExtractOptions(req.Options)
	}

	// Handle Docker commands
	commandOptions, exists := validOptions[req.Command]
	if !exists {
		return ErrInvalidCommand
	}

	if err := validateOptions(req.Options, commandOptions); err != nil {
		return err
	}

	for _, service := range req.Services {
		if err := validateServiceName(service); err != nil {
			return err
		}
	}

	return nil
}

func validateOptions(options []string, validOpts map[string]bool) error {
	i := 0
	for i < len(options) {
		option := options[i]

		if containsDangerousChars(option) {
			return ErrCommandInjection
		}

		if strings.HasPrefix(option, "--") && strings.Contains(option, "=") {

			parts := strings.SplitN(option, "=", 2)
			if len(parts) != 2 {
				return ErrInvalidOption
			}

			optionName := parts[0]
			optionValue := parts[1]

			if !validOpts[optionName] {
				return ErrInvalidOption
			}

			if err := validateOptionValue(optionName, optionValue); err != nil {
				return err
			}
		} else if validOpts[option] {

			if requiresValue(option) && i+1 < len(options) {
				i++
				value := options[i]
				if err := validateOptionValue(option, value); err != nil {
					return err
				}
			}
		} else {
			return ErrInvalidOption
		}

		i++
	}

	return nil
}

func validateServiceName(name string) error {
	if name == "" {
		return ErrInvalidServiceName
	}

	if len(name) > 64 {
		return ErrInvalidServiceName
	}

	if !validServiceNameRegex.MatchString(name) {
		return ErrInvalidServiceName
	}

	if containsDangerousChars(name) {
		return ErrCommandInjection
	}

	return nil
}

func validateOptionValue(option, value string) error {

	if containsDangerousChars(value) {
		return ErrCommandInjection
	}

	if validValues, exists := validOptionValues[option]; exists {
		if !validValues[value] {
			return ErrInvalidOption
		}
	}

	if option == "-t" || option == "--timeout" || option == "--wait-timeout" {
		if !numericValueRegex.MatchString(value) {
			return ErrInvalidOption
		}
	}

	if option == "--scale" {
		if !scaleValueRegex.MatchString(value) {
			return ErrInvalidOption
		}
	}

	return nil
}

func requiresValue(option string) bool {
	valueOptions := []string{
		"-t", "--timeout", "--wait-timeout",
		"--pull", "--rmi", "--policy", "--scale",
	}

	return slices.Contains(valueOptions, option)
}

func containsDangerousChars(input string) bool {

	dangerousPatterns := []string{
		";", "&", "|", "&&", "||", "$", "`",
		"$(", ")", "{", "}", "<", ">", ">>",
		"\\", "'", "\"", "\n", "\r", "\t",
	}

	for _, pattern := range dangerousPatterns {
		if strings.Contains(input, pattern) {
			return true
		}
	}

	return false
}
