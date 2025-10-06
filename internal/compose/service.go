package compose

import (
	"fmt"
)

type Service struct {
	editor *Editor
}

func NewService(stackLocation string) *Service {
	return &Service{
		editor: NewEditor(stackLocation),
	}
}

func (s *Service) UpdateCompose(stackName string, changes ComposeChanges) error {
	if err := ValidateChanges(changes); err != nil {
		return fmt.Errorf("validation failed: %w", err)
	}

	if err := s.editor.ApplyChanges(stackName, changes); err != nil {
		return fmt.Errorf("failed to apply changes: %w", err)
	}

	return nil
}
