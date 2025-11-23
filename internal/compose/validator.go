package compose

import (
	"fmt"
	"regexp"
	"strings"

	"berth-agent/internal/logging"

	"go.uber.org/zap"
)

var (
	imageTagRegex = regexp.MustCompile(`^[a-zA-Z0-9_][a-zA-Z0-9._-]{0,127}$`)
)

func ValidateChanges(changes ComposeChanges, logger *logging.Logger) error {
	logger.Debug("validating image updates",
		zap.Int("image_updates_count", len(changes.ServiceImageUpdates)),
	)

	if err := validateServiceImageUpdates(changes.ServiceImageUpdates, logger); err != nil {
		logger.Error("image update validation failed",
			zap.Error(err),
		)
		return err
	}

	logger.Debug("validating port updates",
		zap.Int("port_updates_count", len(changes.ServicePortUpdates)),
	)

	if err := validateServicePortUpdates(changes.ServicePortUpdates, logger); err != nil {
		logger.Error("port update validation failed",
			zap.Error(err),
		)
		return err
	}

	logger.Debug("validating environment updates",
		zap.Int("env_updates_count", len(changes.ServiceEnvUpdates)),
	)

	if err := validateServiceEnvironmentUpdates(changes.ServiceEnvUpdates, logger); err != nil {
		logger.Error("environment update validation failed",
			zap.Error(err),
		)
		return err
	}

	return nil
}

func validateServicePortUpdates(updates []ServicePortUpdate, logger *logging.Logger) error {
	for _, update := range updates {
		logger.Debug("validating port update",
			zap.String("service_name", update.ServiceName),
			zap.Int("ports_count", len(update.Ports)),
		)

		if update.ServiceName == "" {
			logger.Warn("port update missing service name")
			return fmt.Errorf("service name is required for port update")
		}

		if update.Ports == nil {
			logger.Warn("port update missing ports array",
				zap.String("service_name", update.ServiceName),
			)
			return fmt.Errorf("ports must be provided for service '%s'", update.ServiceName)
		}

		for _, entry := range update.Ports {
			value := strings.TrimSpace(entry)
			if value == "" {
				logger.Warn("port entry is empty",
					zap.String("service_name", update.ServiceName),
				)
				return fmt.Errorf("port entry cannot be empty for service '%s'", update.ServiceName)
			}
			if strings.ContainsAny(value, " 	") {
				logger.Warn("port entry contains whitespace",
					zap.String("service_name", update.ServiceName),
					zap.String("port_entry", entry),
				)
				return fmt.Errorf("port entry '%s' contains whitespace for service '%s'", entry, update.ServiceName)
			}
		}
	}

	return nil
}

func validateServiceImageUpdates(updates []ServiceImageUpdate, logger *logging.Logger) error {
	for _, update := range updates {
		logger.Debug("validating image update",
			zap.String("service_name", update.ServiceName),
			zap.String("new_image", update.NewImage),
			zap.String("new_tag", update.NewTag),
		)

		if update.ServiceName == "" {
			logger.Warn("image update missing service name")
			return fmt.Errorf("service name is required for image update")
		}

		if update.NewImage == "" && update.NewTag == "" {
			logger.Warn("image update missing both new_image and new_tag",
				zap.String("service_name", update.ServiceName),
			)
			return fmt.Errorf("either new_image or new_tag must be provided for service '%s'", update.ServiceName)
		}

		if update.NewImage != "" && update.NewTag != "" {
			logger.Warn("image update has both new_image and new_tag",
				zap.String("service_name", update.ServiceName),
				zap.String("new_image", update.NewImage),
				zap.String("new_tag", update.NewTag),
			)
			return fmt.Errorf("cannot specify both new_image and new_tag for service '%s'", update.ServiceName)
		}

		if update.NewTag != "" {
			if !imageTagRegex.MatchString(update.NewTag) {
				logger.Warn("invalid image tag format",
					zap.String("service_name", update.ServiceName),
					zap.String("new_tag", update.NewTag),
				)
				return fmt.Errorf("invalid image tag '%s' for service '%s'", update.NewTag, update.ServiceName)
			}
		}

		if update.NewImage != "" {
			logger.Debug("validating image name",
				zap.String("service_name", update.ServiceName),
				zap.String("image", update.NewImage),
			)
			if err := validateImageName(update.NewImage, logger); err != nil {
				logger.Warn("invalid image name",
					zap.String("service_name", update.ServiceName),
					zap.String("image", update.NewImage),
					zap.Error(err),
				)
				return fmt.Errorf("invalid image '%s' for service '%s': %w", update.NewImage, update.ServiceName, err)
			}
		}
	}

	return nil
}

func validateImageName(image string, logger *logging.Logger) error {
	if image == "" {
		logger.Warn("image name is empty")
		return fmt.Errorf("image name cannot be empty")
	}

	if len(image) > 255 {
		logger.Warn("image name too long",
			zap.String("image", image),
			zap.Int("length", len(image)),
		)
		return fmt.Errorf("image name too long (max 255 characters)")
	}

	parts := strings.Split(image, "/")

	if len(parts) > 3 {
		logger.Warn("invalid image name format - too many parts",
			zap.String("image", image),
			zap.Int("parts_count", len(parts)),
		)
		return fmt.Errorf("invalid image name format")
	}

	imageAndTag := parts[len(parts)-1]

	if strings.Contains(imageAndTag, ":") {
		imageParts := strings.Split(imageAndTag, ":")
		if len(imageParts) > 2 {
			logger.Warn("invalid image:tag format - multiple colons",
				zap.String("image", image),
			)
			return fmt.Errorf("invalid image:tag format")
		}
	}

	return nil
}

func validateServiceEnvironmentUpdates(updates []ServiceEnvironmentUpdate, logger *logging.Logger) error {
	for _, update := range updates {
		logger.Debug("validating environment update",
			zap.String("service_name", update.ServiceName),
			zap.Int("env_vars_count", len(update.Environment)),
		)

		if update.ServiceName == "" {
			logger.Warn("environment update missing service name")
			return fmt.Errorf("service name is required for environment update")
		}

		if update.Environment == nil {
			logger.Warn("environment update missing environment array",
				zap.String("service_name", update.ServiceName),
			)
			return fmt.Errorf("environment must be provided for service '%s'", update.ServiceName)
		}

		seenKeys := make(map[string]bool)

		for _, envVar := range update.Environment {
			if envVar.Key == "" {
				logger.Warn("environment variable missing key",
					zap.String("service_name", update.ServiceName),
				)
				return fmt.Errorf("environment variable key cannot be empty for service '%s'", update.ServiceName)
			}

			if strings.ContainsAny(envVar.Key, " \t\n\r=") {
				logger.Warn("environment variable key contains invalid characters",
					zap.String("service_name", update.ServiceName),
					zap.String("key", envVar.Key),
				)
				return fmt.Errorf("environment variable key '%s' contains invalid characters for service '%s'", envVar.Key, update.ServiceName)
			}

			if seenKeys[envVar.Key] {
				logger.Warn("duplicate environment variable key",
					zap.String("service_name", update.ServiceName),
					zap.String("key", envVar.Key),
				)
				return fmt.Errorf("duplicate environment variable key '%s' for service '%s'", envVar.Key, update.ServiceName)
			}
			seenKeys[envVar.Key] = true
		}
	}

	return nil
}
