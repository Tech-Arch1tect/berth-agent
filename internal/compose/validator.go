package compose

import (
	"fmt"
	"regexp"
	"strings"
)

var (
	imageTagRegex = regexp.MustCompile(`^[a-zA-Z0-9_][a-zA-Z0-9._-]{0,127}$`)
)

func ValidateChanges(changes ComposeChanges) error {
	if err := validateServiceImageUpdates(changes.ServiceImageUpdates); err != nil {
		return err
	}

	if err := validateServicePortUpdates(changes.ServicePortUpdates); err != nil {
		return err
	}

	return nil
}

func validateServicePortUpdates(updates []ServicePortUpdate) error {
	for _, update := range updates {
		if update.ServiceName == "" {
			return fmt.Errorf("service name is required for port update")
		}

		if update.Ports == nil {
			return fmt.Errorf("ports must be provided for service '%s'", update.ServiceName)
		}

		for _, entry := range update.Ports {
			value := strings.TrimSpace(entry)
			if value == "" {
				return fmt.Errorf("port entry cannot be empty for service '%s'", update.ServiceName)
			}
			if strings.ContainsAny(value, " 	") {
				return fmt.Errorf("port entry '%s' contains whitespace for service '%s'", entry, update.ServiceName)
			}
		}
	}

	return nil
}

func validateServiceImageUpdates(updates []ServiceImageUpdate) error {
	for _, update := range updates {
		if update.ServiceName == "" {
			return fmt.Errorf("service name is required for image update")
		}

		if update.NewImage == "" && update.NewTag == "" {
			return fmt.Errorf("either new_image or new_tag must be provided for service '%s'", update.ServiceName)
		}

		if update.NewImage != "" && update.NewTag != "" {
			return fmt.Errorf("cannot specify both new_image and new_tag for service '%s'", update.ServiceName)
		}

		if update.NewTag != "" {
			if !imageTagRegex.MatchString(update.NewTag) {
				return fmt.Errorf("invalid image tag '%s' for service '%s'", update.NewTag, update.ServiceName)
			}
		}

		if update.NewImage != "" {
			if err := validateImageName(update.NewImage); err != nil {
				return fmt.Errorf("invalid image '%s' for service '%s': %w", update.NewImage, update.ServiceName, err)
			}
		}
	}

	return nil
}

func validateImageName(image string) error {
	if image == "" {
		return fmt.Errorf("image name cannot be empty")
	}

	if len(image) > 255 {
		return fmt.Errorf("image name too long (max 255 characters)")
	}

	parts := strings.Split(image, "/")

	if len(parts) > 3 {
		return fmt.Errorf("invalid image name format")
	}

	imageAndTag := parts[len(parts)-1]

	if strings.Contains(imageAndTag, ":") {
		imageParts := strings.Split(imageAndTag, ":")
		if len(imageParts) > 2 {
			return fmt.Errorf("invalid image:tag format")
		}
	}

	return nil
}
