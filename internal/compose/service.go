package compose

import (
	"fmt"

	"berth-agent/internal/logging"

	"go.uber.org/zap"
)

type Service struct {
	editor *Editor
	logger *logging.Logger
}

func NewService(stackLocation string, logger *logging.Logger) *Service {
	return &Service{
		editor: NewEditor(stackLocation, logger),
		logger: logger.With(zap.String("component", "compose.service")),
	}
}

func (s *Service) UpdateCompose(stackName string, changes ComposeChanges) error {
	s.logger.Debug("validating compose changes",
		zap.String("stack_name", stackName),
		zap.Int("image_updates", len(changes.ServiceImageUpdates)),
		zap.Int("port_updates", len(changes.ServicePortUpdates)),
	)

	if err := ValidateChanges(changes, s.logger); err != nil {
		s.logger.Error("compose validation failed",
			zap.String("stack_name", stackName),
			zap.Error(err),
		)
		return fmt.Errorf("validation failed: %w", err)
	}

	s.logger.Info("compose validation succeeded",
		zap.String("stack_name", stackName),
	)

	if err := s.editor.ApplyChanges(stackName, changes); err != nil {
		s.logger.Error("failed to apply compose changes",
			zap.String("stack_name", stackName),
			zap.Error(err),
		)
		return fmt.Errorf("failed to apply changes: %w", err)
	}

	s.logger.Info("compose file updated successfully",
		zap.String("stack_name", stackName),
	)

	return nil
}
