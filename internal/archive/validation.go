package archive

import (
	"fmt"
	"slices"
)

var validFormats = []string{"zip", "tar", "tar.gz"}

var validCompressionTypes = []string{"gzip", "none"}

var validCreateOptions = []string{"--format", "--output", "--exclude", "--include", "--compression"}

var validExtractOptions = []string{"--archive", "--destination", "--overwrite", "--create-dirs"}

func ValidateCreateOptions(options []string) error {
	i := 0
	for i < len(options) {
		opt := options[i]

		if !slices.Contains(validCreateOptions, opt) {
			return fmt.Errorf("invalid create-archive option: %s", opt)
		}

		if requiresValue(opt) {
			if i+1 >= len(options) {
				return fmt.Errorf("option %s requires a value", opt)
			}

			value := options[i+1]
			if hasValueOptions(opt) && !isValidValue(opt, value) {
				return fmt.Errorf("invalid value %s for option %s", value, opt)
			}

			i += 2
		} else {
			i += 1
		}
	}
	return nil
}

func ValidateExtractOptions(options []string) error {
	i := 0
	for i < len(options) {
		opt := options[i]

		if !slices.Contains(validExtractOptions, opt) {
			return fmt.Errorf("invalid extract-archive option: %s", opt)
		}

		if requiresValue(opt) {
			if i+1 >= len(options) {
				return fmt.Errorf("option %s requires a value", opt)
			}
			i += 2
		} else {
			i += 1
		}
	}
	return nil
}

func requiresValue(option string) bool {
	valueOptions := []string{"--format", "--output", "--exclude", "--include", "--compression", "--archive", "--destination"}
	return slices.Contains(valueOptions, option)
}

func hasValueOptions(option string) bool {
	return option == "--format" || option == "--compression"
}

func isValidValue(option, value string) bool {
	switch option {
	case "--format":
		return slices.Contains(validFormats, value)
	case "--compression":
		return slices.Contains(validCompressionTypes, value)
	}
	return true
}
