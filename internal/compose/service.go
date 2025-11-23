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

func (s *Service) PreviewChanges(stackName string, changes ComposeChanges) (original, preview string, err error) {
	if err := ValidateChanges(changes, s.logger); err != nil {
		s.logger.Error("validation failed",
			zap.String("stack", stackName),
			zap.Error(err))
		return "", "", fmt.Errorf("validation failed: %w", err)
	}

	original, preview, err = s.editor.GeneratePreview(stackName, changes)
	if err != nil {
		s.logger.Error("failed to generate preview",
			zap.String("stack", stackName),
			zap.Error(err))
		return "", "", err
	}

	return original, preview, nil
}

func (s *Service) UpdateCompose(stackName string, changes ComposeChanges) error {
	if err := ValidateChanges(changes, s.logger); err != nil {
		s.logger.Error("validation failed",
			zap.String("stack", stackName),
			zap.Error(err))
		return fmt.Errorf("validation failed: %w", err)
	}

	if err := s.editor.ApplyChanges(stackName, changes); err != nil {
		s.logger.Error("failed to apply changes",
			zap.String("stack", stackName),
			zap.Error(err))
		return fmt.Errorf("failed to apply changes: %w", err)
	}

	return nil
}
