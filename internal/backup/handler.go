package backup

import (
	"fmt"
	"sort"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/tech-arch1tect/berth-agent/internal/common"
	"github.com/tech-arch1tect/berth-agent/internal/validation"
)

type Handler struct {
	service *Service
}

func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

type ListBackupsResponse struct {
	Configured bool  `json:"configured"`
	Runs       []Run `json:"runs"`
}

func (h *Handler) ListStackBackups(c echo.Context) error {
	stackName := c.Param("stackName")
	if err := validation.ValidateStackName(stackName); err != nil {
		return common.SendBadRequest(c, "Invalid stack name: "+err.Error())
	}

	runs, err := h.service.ListRuns(stackName)
	if err != nil {
		return common.SendInternalError(c, err.Error())
	}

	return common.SendSuccess(c, ListBackupsResponse{
		Configured: h.service.Configured(),
		Runs:       runs,
	})
}

func (h *Handler) GetStackBackup(c echo.Context) error {
	stackName := c.Param("stackName")
	if err := validation.ValidateStackName(stackName); err != nil {
		return common.SendBadRequest(c, "Invalid stack name: "+err.Error())
	}
	backupID := c.Param("backupId")
	if backupID == "" {
		return common.SendBadRequest(c, "Backup id is required")
	}

	run, err := h.service.GetRun(stackName, backupID)
	if err != nil {
		return common.SendInternalError(c, err.Error())
	}
	if run == nil {
		return common.SendNotFound(c, "backup not found")
	}
	return common.SendSuccess(c, run)
}

func (s *Service) ListRuns(stackName string) ([]Run, error) {
	if err := validation.ValidateStackName(stackName); err != nil {
		return nil, err
	}
	loaded, err := s.persistence.LoadStackRuns(stackName)
	if err != nil {
		return nil, err
	}

	runs := make([]Run, 0, len(loaded))
	for _, run := range loaded {
		runs = append(runs, *run)
	}
	sort.Slice(runs, func(i, j int) bool {
		return runs[i].StartedAt.After(runs[j].StartedAt)
	})
	return runs, nil
}

func (s *Service) GetRun(stackName, runID string) (*Run, error) {
	if err := validation.ValidateStackName(stackName); err != nil {
		return nil, err
	}
	if _, err := uuid.Parse(runID); err != nil {
		return nil, fmt.Errorf("invalid backup id %q", runID)
	}
	return s.persistence.LoadRun(stackName, runID)
}
